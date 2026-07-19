#!/usr/bin/env node
/**
 * server/index.mjs — 패널 스튜디오 백엔드 (S1).
 *
 * 한 프로세스가 정적 스튜디오(web/)와 /api/* 를 같은 오리진에 서빙한다(CORS 없음).
 * 브라우저가 못 하는 것(sharp·ffmpeg·lift·generate·CORS 프리체크)을 엔드포인트로 노출한다.
 * 기존 CLI 모듈을 그대로 재사용한다 — 로직은 이미 있고 '노출'만 한다.
 *
 *   node server/index.mjs            (PORT=8797 기본)
 *
 * 엔드포인트:
 *   GET  /api/health              → { ok }
 *   GET  /api/me                  → { user }              (Authelia Remote-User, 없으면 anonymous — S4)
 *   POST /api/assets/convert      { dataUri } → { webp, animated }   (이미지·GIF·영상 → WebP)
 *   POST /api/precheck            { url } | { model } → 판정
 *   POST /api/generate            { model, tuya?, id? } → .tar.gz (Ray 저장소 + HANDOFF)
 *   GET  /api/panels              → [{ id, name, updatedAt }]        (파일 스토어 — S3 에서 Postgres)
 *   GET  /api/panels/:id          → { model }
 *   PUT  /api/panels/:id          { name, model } → 저장
 *   DELETE /api/panels/:id
 */
import http from 'node:http';
import { readFileSync, writeFileSync, existsSync, mkdirSync, mkdtempSync, readdirSync, statSync, rmSync, unlinkSync } from 'node:fs';
import { resolve, dirname, join, extname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir, homedir } from 'node:os';
import { gzipSync } from 'node:zlib';
import { randomUUID } from 'node:crypto';

import { lift } from '../src/lift.mjs';
import { generate } from '../src/generate.mjs';
import { applyTuyaToPanel } from '../src/tuya.mjs';
import { precheckUrl, precheckStudio } from '../src/precheck.mjs';
import { dataUriToWebp, videoToWebp } from '../src/assets.mjs';
import { inferGaps } from '../src/handoff.mjs';
import { store } from './db.mjs';
import { oidcEnabled, allowedDomain, sessionUser, login, callback, logout } from './auth.mjs';

const __dir = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dir, '..');
const WEB = resolve(ROOT, 'web');
const PORT = Number(process.env.PORT) || 8797;
// Authelia(S4). Remote-User 는 Caddy forward_auth 가 붙인다. 백엔드가 Caddy 뒤에서만
// 닿을 때(호스트 포트 미노출)만 신뢰해야 한다 — 아무나 닿으면 헤더 스푸핑 가능하기 때문.
// 그래서 TRUST_FORWARD_AUTH=true 일 때만 헤더를 신뢰하고, 아니면 전부 anonymous.
// 유저 판별 우선순위: ① 구글 OIDC 세션 ② Authelia Remote-User(TRUST) ③ anonymous.
const TRUST = process.env.TRUST_FORWARD_AUTH === 'true';
const ownerOf = req => {
  if (oidcEnabled()) { const u = sessionUser(req); return u ? u.email : 'anonymous'; }
  return (TRUST && req.headers['remote-user']) || 'anonymous';
};
const emailOf = req => {
  if (oidcEnabled()) { const u = sessionUser(req); return u ? u.email : null; }
  return (TRUST && req.headers['remote-email']) || null;
};

/* ── 사내 KB 프록시 (kb.goqual.com) ──────────────────────────────────────
 * 토큰은 ~/.kb-token 에서 서버만 읽는다 — 절대 클라이언트/로그/응답에 노출 안 함.
 * 브라우저는 토큰 없이 /api/kb/search 로 질의하고, 서버가 대신 호출해 hits 만 돌려준다.
 * (KB 는 이 호스트 egress IP 화이트리스트 + 60회/분 제한. 그래서 프록시도 자체 제한을 둔다.) */
let _kbToken;
function kbToken() {
  if (_kbToken !== undefined) return _kbToken;
  try { _kbToken = readFileSync(resolve(homedir(), '.kb-token'), 'utf8').trim() || null; }
  catch { _kbToken = null; }
  return _kbToken;
}
let _kbCalls = [];
function kbRateOk() {
  const now = Date.now();
  _kbCalls = _kbCalls.filter(t => now - t < 60000);
  if (_kbCalls.length >= 40) return false; // KB 60/분 여유 두고 40/분
  _kbCalls.push(now);
  return true;
}
async function kbSearch(query, k) {
  const token = kbToken();
  if (!token) { const e = new Error('KB 토큰 없음(~/.kb-token). 이 호스트에서만 검색됩니다.'); e.code = 503; throw e; }
  if (!kbRateOk()) { const e = new Error('KB 요청 한도(분당) 초과 — 잠시 후 다시.'); e.code = 429; throw e; }
  let r;
  try {
    r = await fetch('https://kb.goqual.com/api/kb/search', {
      method: 'POST',
      headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
      body: JSON.stringify({ query, k }),
      signal: AbortSignal.timeout(12000),
    });
  } catch (e) { const err = new Error('KB 검색 실패: ' + (e.name === 'TimeoutError' ? '시간초과' : (e.message || e))); err.code = 502; throw err; }
  if (!r.ok) {
    const msg = r.status === 403 ? 'KB 접근 거부(이 호스트 IP만 허용)' : r.status === 429 ? 'KB 한도 초과' : r.status === 401 ? 'KB 인증 실패' : `KB 오류(${r.status})`;
    const e = new Error(msg); e.code = 502; throw e;
  }
  const data = await r.json();
  // 클라이언트엔 필요한 필드만. text 는 스니펫으로 축약(대용량/원문 노출 최소화).
  return (data.hits || []).map(h => ({ title: h.title, slug: h.slug, text: (h.text || '').replace(/\s+/g, ' ').trim().slice(0, 400) }));
}

/* ── HTTP 헬퍼 ───────────────────────────────────────────────────────── */
const send = (res, code, obj) => { const b = Buffer.from(JSON.stringify(obj)); res.writeHead(code, { 'content-type': 'application/json; charset=utf-8', 'content-length': b.length }); res.end(b); };
const sendRaw = (res, code, buf, headers) => { res.writeHead(code, { 'content-length': buf.length, ...headers }); res.end(buf); };
function readJson(req, limitMB = 64) {
  return new Promise((ok, no) => {
    const chunks = []; let n = 0;
    req.on('data', c => { n += c.length; if (n > limitMB * 1048576) { req.destroy(); no(new Error('본문이 너무 큽니다')); } chunks.push(c); });
    req.on('end', () => { try { ok(chunks.length ? JSON.parse(Buffer.concat(chunks)) : {}); } catch (e) { no(new Error('JSON 파싱 실패')); } });
    req.on('error', no);
  });
}

const MIME = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.webp': 'image/webp', '.png': 'image/png', '.svg': 'image/svg+xml', '.ico': 'image/x-icon' };

/* ── .tar.gz (의존성 0) ──────────────────────────────────────────────── */
function walk(dir, base = dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name), st = statSync(p);
    if (st.isDirectory()) walk(p, base, out);
    else out.push({ name: relative(base, p).replace(/\\/g, '/'), buf: readFileSync(p) });
  }
  return out;
}
function tarHeader(name, size) {
  const h = Buffer.alloc(512);
  h.write(name.slice(0, 100), 0);
  h.write('0000644\0', 100); h.write('0000000\0', 108); h.write('0000000\0', 116);
  h.write(size.toString(8).padStart(11, '0') + '\0', 124);
  h.write((0).toString(8).padStart(11, '0') + '\0', 136);
  h.write('        ', 148); // chksum placeholder(spaces)
  h.write('0', 156); h.write('ustar\0', 257); h.write('00', 263);
  let sum = 0; for (const b of h) sum += b;
  h.write(sum.toString(8).padStart(6, '0') + '\0 ', 148);
  return h;
}
function tarGz(rootDir, prefix = '') {
  const parts = [];
  for (const f of walk(rootDir)) {
    const name = prefix ? `${prefix}/${f.name}` : f.name;
    parts.push(tarHeader(name, f.buf.length), f.buf);
    const pad = (512 - (f.buf.length % 512)) % 512; if (pad) parts.push(Buffer.alloc(pad));
  }
  parts.push(Buffer.alloc(1024)); // 종료 블록
  return gzipSync(Buffer.concat(parts), { level: 6 });
}

/* ── 정적 파일 (web/) ────────────────────────────────────────────────── */
function serveStatic(req, res) {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  if (p === '/' || p === '') p = '/index.html';
  const abs = resolve(WEB, '.' + p);
  if (!abs.startsWith(WEB) || !existsSync(abs) || statSync(abs).isDirectory()) return send(res, 404, { error: 'not found' });
  const buf = readFileSync(abs);
  sendRaw(res, 200, buf, { 'content-type': MIME[extname(abs)] || 'application/octet-stream', 'cache-control': 'no-cache' });
}

/* ── 에셋 변환 (이미지·GIF·영상 → WebP) ──────────────────────────────── */
async function convertAsset(dataUri) {
  const m = /^data:([^;,]+)?(;base64)?,/.exec(dataUri || '');
  if (!m) throw new Error('data URI 형식이 아닙니다');
  const mime = m[1] || '';
  const tmp = mkdtempSync(join(tmpdir(), 'ps-asset-'));
  try {
    const out = join(tmp, 'out.webp');
    let animated = false;
    if (mime.startsWith('video/')) {
      const inp = join(tmp, 'in' + (mime.split('/')[1] ? '.' + mime.split('/')[1] : '.mp4'));
      writeFileSync(inp, Buffer.from(dataUri.slice(dataUri.indexOf(',') + 1), 'base64'));
      const r = await videoToWebp(inp, out); animated = r.animated;
    } else {
      const r = await dataUriToWebp(dataUri, out); animated = !!r.animated;
    }
    const webp = 'data:image/webp;base64,' + readFileSync(out).toString('base64');
    return { webp, animated };
  } finally { rmSync(tmp, { recursive: true, force: true }); }
}

/* ── generate → .tar.gz ──────────────────────────────────────────────── */
function generateArchive({ model, tuya, id }) {
  const { panel } = lift(model);
  if (tuya) applyTuyaToPanel(panel, tuya);
  if (id) panel.meta.id = id;
  if (!panel.meta.id) panel.meta.id = (model.meta?.deviceKey || 'panel').replace(/[^\w-]/g, '') || 'panel';
  const work = mkdtempSync(join(tmpdir(), 'ps-gen-'));
  try {
    const pf = join(work, 'panel.json'); writeFileSync(pf, JSON.stringify(panel, null, 2));
    const outDir = join(work, panel.meta.id);
    const result = generate(pf, outDir);
    const blockers = (result.blockers || inferGaps(panel).filter(g => g.severity === 'blocker')).map(g => g.path || g);
    const targz = tarGz(outDir, panel.meta.id);
    return { targz, blocked: !!result.blocked, blockers, id: panel.meta.id };
  } finally { rmSync(work, { recursive: true, force: true }); }
}

/* ── 라우팅 ─────────────────────────────────────────────────────────── */
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x');
  const path = url.pathname;
  try {
    if (!path.startsWith('/api/')) return serveStatic(req, res);

    // 인증 라우트(로그인 불필요)
    if (path === '/api/auth/login') return login(req, res);
    if (path === '/api/auth/callback') return callback(req, res, url);
    if (path === '/api/auth/logout') return logout(req, res);
    if (path === '/api/health') return send(res, 200, { ok: true, service: 'panel-studio', ts: new Date().toISOString() });
    if (path === '/api/me') {
      const authed = oidcEnabled() ? !!sessionUser(req) : (TRUST && !!req.headers['remote-user']);
      return send(res, 200, { user: ownerOf(req), email: emailOf(req), authed, oidc: oidcEnabled(), domain: allowedDomain(), loginUrl: '/api/auth/login', logoutUrl: '/api/auth/logout' });
    }

    // OIDC 가 켜졌으면 나머지 API 는 로그인 필수 (assets·precheck·generate·panels)
    if (oidcEnabled() && !sessionUser(req)) return send(res, 401, { error: 'login required', loginUrl: '/api/auth/login' });

    if (path === '/api/assets/convert' && req.method === 'POST') {
      const { dataUri } = await readJson(req); return send(res, 200, await convertAsset(dataUri));
    }
    if (path === '/api/precheck' && req.method === 'POST') {
      const body = await readJson(req);
      if (body.url) return send(res, 200, await precheckUrl(body.url));
      if (body.model) return send(res, 200, { results: await precheckStudio(body.model) });
      return send(res, 400, { error: 'url 또는 model 필요' });
    }
    if (path === '/api/generate' && req.method === 'POST') {
      const body = await readJson(req);
      if (!body.model) return send(res, 400, { error: 'model 필요' });
      const { targz, blocked, blockers, id } = generateArchive(body);
      return sendRaw(res, 200, targz, {
        'content-type': 'application/gzip',
        'content-disposition': `attachment; filename="${id}.tar.gz"`,
        'x-blocked': String(blocked), 'x-blockers': String(blockers.length),
      });
    }

    // 사내 KB 제품 검색 프록시 (토큰은 서버에만). 마법사 제품 프리필용.
    if (path === '/api/kb/search' && req.method === 'POST') {
      const body = await readJson(req, 1);
      const query = String(body.query || '').trim();
      if (!query) return send(res, 400, { error: 'query 필요' });
      const k = Math.max(1, Math.min(20, Number(body.k) || 8));
      try { const hits = await kbSearch(query, k); return send(res, 200, { count: hits.length, hits }); }
      catch (e) { return send(res, e.code || 502, { error: e.message }); }
    }

    // 패널 CRUD (owner 범위 — db.mjs, Postgres 또는 파일)
    const pm = /^\/api\/panels(?:\/([\w-]+))?$/.exec(path);
    if (pm) {
      const id = pm[1], owner = ownerOf(req), db = await store();
      if (req.method === 'GET' && !id) return send(res, 200, await db.list(owner));
      if (req.method === 'GET' && id) { const p = await db.get(id, owner); return p ? send(res, 200, p) : send(res, 404, { error: 'not found' }); }
      if (req.method === 'PUT' && id) { const b = await readJson(req); return send(res, 200, await db.save(id, owner, b.name, b.model)); }
      if (req.method === 'POST' && !id) { const b = await readJson(req); return send(res, 201, await db.create(owner, b.name, b.model)); }
      if (req.method === 'DELETE' && id) return send(res, 200, await db.del(id, owner));
    }

    return send(res, 404, { error: 'unknown endpoint' });
  } catch (e) {
    return send(res, 500, { error: String(e.message || e) });
  }
});

server.listen(PORT, async () => {
  console.log(`패널 스튜디오 백엔드 → http://127.0.0.1:${PORT}  (정적 web/ + /api/*)`);
  await store();   // 스토어 초기화(스키마/디렉터리) + 종류 로그
});
