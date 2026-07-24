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
 *   POST /api/ads/publish       { model } → 랜딩 렌더 → 광고 서버에 소재 생성 → 콘솔 URL
 *   POST /api/build               { model, tuya?, id? } → { ok, ms, pages, artifactId, log }  (실제 ray build)
 *   GET  /api/build/:artifactId   → dist.tar.gz (빌드 산출물)
 *   GET  /api/panels              → [{ id, name, updatedAt }]        (파일 스토어 — S3 에서 Postgres)
 *   GET  /api/panels/:id          → { model }
 *   PUT  /api/panels/:id          { name, model } → 저장
 *   DELETE /api/panels/:id
 */
import http from 'node:http';
import { readFileSync, writeFileSync, existsSync, mkdirSync, mkdtempSync, readdirSync, statSync, rmSync, unlinkSync, cpSync } from 'node:fs';
import { spawn } from 'node:child_process';
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
import { renderLanding, reviewLanding } from '../src/emitweb.mjs';
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

/**
 * 광고 서버 프록시 (개발 단계).
 *
 * 광고 콘솔은 별도 서비스(Go)지만, Cloudflare 터널 ingress 가 대시보드 관리라
 * 새 호스트명을 코드로 만들 수 없다. 그래서 **이미 열려 있는 스튜디오 호스트**에
 * 광고 서버의 경로를 그대로 얹어 외부에서 닿게 한다.
 *
 * 경로가 겹치지 않아 재작성이 필요 없다 — 스튜디오는 /api/* 와 정적 web/ 만 쓰고,
 * 광고 서버는 /console·/go·/l/·/e·/report·/auth/ 를 쓴다.
 *
 * ⚠ 이건 **개발 단계 편의**다. 운영에서는 광고 서버가 자기 호스트명(ads.hej.life)을
 * 가져야 한다 — 광고 트래픽과 저작도구 트래픽을 한 프로세스가 받으면 안 된다.
 */
const ADS_PATHS = ['/console', '/go', '/l/', '/e', '/report', '/auth/'];
const isAdsPath = p => ADS_PATHS.some(x => p === x || p.startsWith(x.endsWith('/') ? x : x + '/') || p === x.replace(/\/$/, ''));

async function proxyToAds(req, res, path) {
  const base = process.env.ADS_BASE_URL;
  if (!base) { res.writeHead(503, { 'content-type': 'text/plain; charset=utf-8' });
    return res.end('광고 서버가 연결되지 않았어요 (ADS_BASE_URL 미설정)'); }
  const url = base + req.url;
  const headers = {};
  for (const [k, v] of Object.entries(req.headers)) {
    if (['host', 'connection', 'content-length'].includes(k)) continue;
    headers[k] = v;
  }
  let body;
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    body = await new Promise(r => { const c = []; req.on('data', d => c.push(d)); req.on('end', () => r(Buffer.concat(c))); });
  }
  try {
    // redirect: 'manual' — 302 를 그대로 흘려보낸다(광고 서버의 /go 가 302 로 랜딩을 가리킨다).
    const r = await fetch(url, { method: req.method, headers, body, redirect: 'manual' });
    const out = {};
    r.headers.forEach((v, k) => { if (k !== 'content-encoding' && k !== 'content-length') out[k] = v; });
    const buf = Buffer.from(await r.arrayBuffer());
    res.writeHead(r.status, out);
    res.end(buf);
  } catch (e) {
    res.writeHead(502, { 'content-type': 'text/plain; charset=utf-8' });
    res.end('광고 서버 연결 실패: ' + e.message);
  }
}

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

/* ── ray build (프리베이크 node_modules 재사용) ───────────────────────
 * 생성 저장소의 의존성 집합은 패널과 무관하게 고정이다 — generate 가
 * package.reference.json 을 이름만 바꿔 그대로 쓰기 때문(src/generate.mjs).
 * 그래서 이미지에 '저장소 모양의 작업장'(node_modules 포함)을 한 번 구워 두고,
 * 요청마다 소스만 갈아끼워 재사용한다.
 * 실측(alpine/musl): 설치 33s(이미지 1회) · 빌드 1.8s(패널당). 교차 검증 완료
 * (haatz 로 설치한 node_modules 로 plug-mini 가 빌드됨).
 *
 * node_modules 를 저장소 밖에 두고 심볼릭 링크하면 안 된다 — ray 가 _npm 출력 경로를
 * 저장소 밖으로 계산해 산출물에 '__/__/__/opt/...' 가 박힌다(실측해서 되돌린 설계다).
 *
 * 툴체인이 없으면(로컬 개발) 503 으로 정직하게 거절한다 — sharp·ffmpeg 와 같은 태도.
 * 서버가 셸을 쓰는 유일한 자리다. 실행 대상은 우리가 생성한 저장소뿐이고,
 * 사용자 입력은 데이터로만 들어간다(임의 명령이 아니다). */
const BUILD_DIR = process.env.PANEL_BUILD_DIR || '/opt/panel-build';
const BUILD_TIMEOUT_MS = Number(process.env.BUILD_TIMEOUT_MS) || 120000;
// 작업장이 하나뿐이라 직렬화한다. 빌드가 ~2초라 큐가 길어지지 않는다.
const BUILD_CONCURRENCY = Number(process.env.BUILD_CONCURRENCY) || 1;
const ARTIFACT_TTL_MS = 10 * 60 * 1000;
const ARTIFACT_MAX = 32;
let _building = 0;

const buildable = () => existsSync(join(BUILD_DIR, 'node_modules', '.bin', 'ray'));
const tailLog = (s, n = 4000) => { s = String(s || ''); return s.length > n ? '…' + s.slice(-n) : s; };

/** 작업장에서 node_modules 만 남기고 지운다(이전 빌드 잔재 제거). */
function resetBuildDir() {
  for (const name of readdirSync(BUILD_DIR)) {
    if (name === 'node_modules') continue;
    rmSync(join(BUILD_DIR, name), { recursive: true, force: true });
  }
}

/* 빌드 산출물은 메모리에 잠깐 둔다(TTL 10분·최대 32건). owner 범위 밖은 못 받는다. */
const _artifacts = new Map();
function sweepArtifacts() {
  const now = Date.now();
  for (const [k, v] of _artifacts) if (now - v.at > ARTIFACT_TTL_MS) _artifacts.delete(k);
  while (_artifacts.size > ARTIFACT_MAX) _artifacts.delete(_artifacts.keys().next().value);
}
function putArtifact(owner, name, buf) {
  const id = randomUUID();
  _artifacts.set(id, { buf, owner, name, at: Date.now() });
  sweepArtifacts();
  return id;
}

function runRayBuild(dir) {
  return new Promise(ok => {
    const p = spawn(join(dir, 'node_modules', '.bin', 'ray'), ['build', '-t', 'tuya'], {
      cwd: dir, stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, NODE_ENV: 'production', CI: '1' },
    });
    let out = '', timedOut = false;
    const t = setTimeout(() => { timedOut = true; p.kill('SIGKILL'); }, BUILD_TIMEOUT_MS);
    const cap = c => { if (out.length < 64000) out += c.toString(); };
    p.stdout.on('data', cap); p.stderr.on('data', cap);
    p.on('error', e => { clearTimeout(t); ok({ code: -1, log: String(e.message || e), timedOut: false }); });
    p.on('close', code => { clearTimeout(t); ok({ code, log: out, timedOut }); });
  });
}

async function buildPanel({ model, tuya, id }, owner) {
  if (!buildable()) { const e = new Error('이 서버에는 빌드 툴체인이 없습니다(PANEL_BUILD_DIR 미설치). 저장소 받기로 개발자에게 넘기세요.'); e.code = 503; throw e; }
  if (_building >= BUILD_CONCURRENCY) { const e = new Error('빌드가 혼잡합니다. 잠시 후 다시 시도해 주세요.'); e.code = 429; throw e; }
  _building++;
  const work = mkdtempSync(join(tmpdir(), 'ps-build-'));
  try {
    const { panel } = lift(model);
    if (tuya) applyTuyaToPanel(panel, tuya);
    if (id) panel.meta.id = id;
    if (!panel.meta.id) panel.meta.id = (model.meta?.deviceKey || 'panel').replace(/[^\w-]/g, '') || 'panel';

    const pf = join(work, 'panel.json'); writeFileSync(pf, JSON.stringify(panel, null, 2));
    const outDir = join(work, panel.meta.id);
    const result = generate(pf, outDir);
    const blockers = (result.blockers || inferGaps(panel).filter(g => g.severity === 'blocker')).map(g => g.path || g);
    // blocker 가 있으면 저장소 자체가 안 나온다 — 빌드할 대상이 없다.
    if (result.blocked) return { ok: false, stage: 'generate', blocked: true, blockers, id: panel.meta.id,
      log: `blocker ${blockers.length}건이 남아 저장소가 생성되지 않았습니다. HANDOFF 를 먼저 닫아 주세요.` };

    // 구워둔 작업장에 소스만 갈아끼운다(node_modules 는 보존).
    resetBuildDir();
    cpSync(outDir, BUILD_DIR, { recursive: true });
    const t0 = Date.now();
    const r = await runRayBuild(BUILD_DIR);
    const ms = Date.now() - t0;
    const distDir = join(BUILD_DIR, 'dist', 'tuya');

    if (r.code !== 0 || !existsSync(distDir)) {
      return { ok: false, stage: 'build', id: panel.meta.id, ms, timedOut: r.timedOut,
        log: tailLog(r.timedOut ? `빌드가 ${BUILD_TIMEOUT_MS / 1000}초를 넘겨 중단됐습니다.\n` + r.log : r.log) };
    }
    const pagesDir = join(distDir, 'pages');
    const pages = existsSync(pagesDir) ? readdirSync(pagesDir) : [];
    const targz = tarGz(distDir, panel.meta.id + '-dist');
    return { ok: true, id: panel.meta.id, ms, pages, bytes: targz.length,
      artifactId: putArtifact(owner, panel.meta.id, targz), log: tailLog(r.log, 1500) };
  } finally {
    _building--;
    rmSync(work, { recursive: true, force: true });
    try { resetBuildDir(); } catch {}   // 남의 패널 소스가 작업장에 남지 않게
  }
}

/* ── 라우팅 ─────────────────────────────────────────────────────────── */
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x');
  const path = url.pathname;
  try {
    if (isAdsPath(path)) return proxyToAds(req, res, path);
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

    // 실제 ray build — 저작 시점에 '이 저장소가 컴파일되는가'를 초록불로 확인한다.

    // ── 광고로 발행 (3단계) ────────────────────────────────────────────────
    // 스튜디오는 **소재 공장**이다. 랜딩 HTML 을 굽어 광고 서버에 올리고, 캠페인 설정은
    // 광고 콘솔로 넘긴다 — 저작도구가 캠페인까지 떠안으면 성격이 바뀐다.
    // 렌더가 여기(Node)에 있는 이유: 위젯 카탈로그·emitAdStyles 가 이 리포에 있어서
    // Go 로 포팅하면 렌더 규격이 두 벌이 된다.
    if (path === '/api/ads/publish' && req.method === 'POST') {
      const body = await readJson(req);
      const model = body?.model;
      if (!model) return send(res, 400, { error: 'model 이 필요하다' });

      const review = reviewLanding(model);
      if (!review.ok) return send(res, 422, { error: '발행 검수 실패', ...review });

      const adsBase = process.env.ADS_BASE_URL || '';
      const html = renderLanding(model);
      if (!adsBase) {
        // 광고 서버가 없으면 렌더 결과만 돌려준다(로컬에서 확인은 되게).
        return send(res, 200, { ok: true, connected: false, warnings: review.warnings,
          html, note: 'ADS_BASE_URL 미설정 — 광고 서버에 올리지 않고 렌더 결과만 돌려준다' });
      }
      try {
        const r = await fetch(adsBase + '/api/creatives', {
          method: 'POST',
          headers: { 'content-type': 'application/json', 'x-ads-secret': process.env.ADS_ADMIN_SECRET || '' },
          body: JSON.stringify({
            format: model.meta?.deviceKey || 'ad',
            title: model.meta?.name || '광고',
            landingHtml: html,
          }),
        });
        if (!r.ok) return send(res, 502, { error: '광고 서버 응답 오류 ' + r.status });
        const out = await r.json();
        return send(res, 200, { ok: true, connected: true, warnings: review.warnings, ...out });
      } catch (e) {
        return send(res, 502, { error: '광고 서버 연결 실패: ' + e.message });
      }
    }
    if (path === '/api/build' && req.method === 'POST') {
      const body = await readJson(req);
      if (!body.model) return send(res, 400, { error: 'model 필요' });
      try { return send(res, 200, await buildPanel(body, ownerOf(req))); }
      catch (e) { return send(res, e.code || 500, { error: e.message }); }
    }
    const bm = /^\/api\/build\/([\w-]+)$/.exec(path);
    if (bm && req.method === 'GET') {
      sweepArtifacts();
      const a = _artifacts.get(bm[1]);
      if (!a || a.owner !== ownerOf(req)) return send(res, 404, { error: '산출물이 없습니다(만료되었거나 권한 없음).' });
      return sendRaw(res, 200, a.buf, {
        'content-type': 'application/gzip',
        'content-disposition': `attachment; filename="${a.name}-dist.tar.gz"`,
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
