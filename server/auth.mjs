/**
 * auth.mjs — 구글 OIDC 로그인 (@goqual.com 회사 계정만).
 *
 * 백엔드가 직접 OAuth2 Authorization Code Flow(confidential client)를 돈다. Authelia
 * 스택에 의존하지 않아 자족적이다. **도메인 제한은 서버측에서** id_token 의 이메일 도메인·
 * hd 클레임으로 검증한다(구글 `hd` 파라미터는 계정 선택 힌트일 뿐, 보안 경계가 아님).
 *
 * env:
 *   GOOGLE_CLIENT_ID · GOOGLE_CLIENT_SECRET   — 구글 클라우드 OAuth 2.0 웹 클라이언트
 *   ALLOWED_DOMAIN (기본 goqual.com)           — 허용 이메일 도메인
 *   SESSION_SECRET                             — 세션 쿠키 HMAC 키(설정 권장; 없으면 재시작마다 세션 무효)
 *   OIDC_REDIRECT_URI / OIDC_ORIGIN            — (선택) 리다이렉트 URI 고정. 기본은 요청에서 유도.
 *
 * GOOGLE_CLIENT_ID 가 없으면 OIDC 비활성 → 익명/로컬 개발로 동작(기존과 동일).
 *
 * 참고: Authorization Code Flow 는 id_token 을 Google 토큰 엔드포인트에서 TLS 직결로 받으므로
 * OIDC 규격상 서명 검증을 생략할 수 있다(iss·aud·exp·nonce 는 검증한다). 필요하면 JWKS 검증 추가.
 */
import { createHmac, randomBytes, timingSafeEqual } from 'node:crypto';

const CLIENT_ID = process.env.GOOGLE_CLIENT_ID;
const CLIENT_SECRET = process.env.GOOGLE_CLIENT_SECRET;
const DOMAIN = (process.env.ALLOWED_DOMAIN || 'goqual.com').toLowerCase();
const SECRET = process.env.SESSION_SECRET || randomBytes(32).toString('hex');
const TTL_MS = 12 * 3600 * 1000; // 세션 12시간

export const oidcEnabled = () => !!(CLIENT_ID && CLIENT_SECRET);
export const allowedDomain = () => DOMAIN;

const b64uJson = o => Buffer.from(JSON.stringify(o)).toString('base64url');
const hmac = s => createHmac('sha256', SECRET).update(s).digest('base64url');
const safeEq = (a, b) => { try { const x = Buffer.from(a), y = Buffer.from(b); return x.length === y.length && timingSafeEqual(x, y); } catch { return false; } };

function cookies(req) {
  return Object.fromEntries((req.headers.cookie || '').split(';').map(c => { const i = c.indexOf('='); return i < 0 ? null : [c.slice(0, i).trim(), decodeURIComponent(c.slice(i + 1).trim())]; }).filter(Boolean));
}
function setCookie(res, name, val, maxAgeSec) {
  const parts = [`${name}=${encodeURIComponent(val)}`, 'Path=/', 'HttpOnly', 'SameSite=Lax', 'Secure'];
  if (maxAgeSec != null) parts.push(`Max-Age=${maxAgeSec}`);
  const prev = res.getHeader('Set-Cookie');
  res.setHeader('Set-Cookie', [...(prev ? (Array.isArray(prev) ? prev : [prev]) : []), parts.join('; ')]);
}
function originOf(req) {
  if (process.env.OIDC_ORIGIN) return process.env.OIDC_ORIGIN;
  const proto = (req.headers['x-forwarded-proto'] || 'https').split(',')[0].trim();
  const host = req.headers['x-forwarded-host'] || req.headers.host;
  return `${proto}://${host}`;
}
const redirectUri = req => process.env.OIDC_REDIRECT_URI || `${originOf(req)}/api/auth/callback`;

/** 세션 쿠키 → { email, name } | null */
export function sessionUser(req) {
  const tok = cookies(req).ps_session;
  if (!tok) return null;
  const dot = tok.lastIndexOf('.');
  if (dot < 0) return null;
  const payload = tok.slice(0, dot), mac = tok.slice(dot + 1);
  if (!safeEq(mac, hmac(payload))) return null;
  try { const p = JSON.parse(Buffer.from(payload, 'base64url')); return p.exp > Date.now() ? { email: p.email, name: p.name } : null; } catch { return null; }
}

/** GET /api/auth/login → 구글 동의 화면으로 */
export function login(req, res) {
  const state = randomBytes(16).toString('hex');
  const nonce = randomBytes(16).toString('hex');
  setCookie(res, 'ps_oauth', `${state}.${nonce}`, 600);
  const q = new URLSearchParams({
    client_id: CLIENT_ID, redirect_uri: redirectUri(req), response_type: 'code',
    scope: 'openid email profile', state, nonce, hd: DOMAIN, prompt: 'select_account', access_type: 'online',
  });
  res.writeHead(302, { Location: 'https://accounts.google.com/o/oauth2/v2/auth?' + q.toString() });
  res.end();
}

/** GET /api/auth/callback?code&state → 검증 후 세션 발급 */
export async function callback(req, res, url) {
  const [state0, nonce0] = (cookies(req).ps_oauth || '').split('.');
  setCookie(res, 'ps_oauth', '', 0); // 일회용 소진
  const code = url.searchParams.get('code'), state = url.searchParams.get('state');
  const fail = (code, msg) => { res.writeHead(code, { 'content-type': 'text/html; charset=utf-8' }); res.end(`<meta charset=utf-8><body style="font-family:system-ui,sans-serif;display:grid;place-items:center;height:100vh;margin:0"><div style="text-align:center"><h2>로그인할 수 없습니다</h2><p>${msg}</p><a href="/api/auth/login" style="color:#007559">다시 시도</a></div></body>`); };
  if (!code || !state || !state0 || state !== state0) return fail(400, '요청이 유효하지 않아요(state 불일치).');
  let claims;
  try {
    const tok = await (await fetch('https://oauth2.googleapis.com/token', {
      method: 'POST', headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ code, client_id: CLIENT_ID, client_secret: CLIENT_SECRET, redirect_uri: redirectUri(req), grant_type: 'authorization_code' }),
    })).json();
    if (!tok.id_token) return fail(400, '구글 토큰 교환에 실패했어요.');
    claims = JSON.parse(Buffer.from(tok.id_token.split('.')[1], 'base64url'));
  } catch { return fail(502, '구글 인증 서버에 연결하지 못했어요.'); }

  const okIss = ['accounts.google.com', 'https://accounts.google.com'].includes(claims.iss);
  const okAud = claims.aud === CLIENT_ID;
  const okExp = claims.exp * 1000 > Date.now();
  const okNonce = claims.nonce === nonce0;
  const email = String(claims.email || '').toLowerCase();
  const okEmail = claims.email_verified === true && email.endsWith('@' + DOMAIN);
  const okHd = !claims.hd || String(claims.hd).toLowerCase() === DOMAIN;
  if (!(okIss && okAud && okExp && okNonce)) return fail(400, '토큰 검증에 실패했어요.');
  if (!(okEmail && okHd)) return fail(403, `@${DOMAIN} 회사 계정만 접근할 수 있어요.`);

  const payload = b64uJson({ email, name: claims.name || email, exp: Date.now() + TTL_MS });
  setCookie(res, 'ps_session', `${payload}.${hmac(payload)}`, TTL_MS / 1000);
  res.writeHead(302, { Location: '/' });
  res.end();
}

/** GET /api/auth/logout */
export function logout(req, res) {
  setCookie(res, 'ps_session', '', 0);
  res.writeHead(302, { Location: '/' });
  res.end();
}
