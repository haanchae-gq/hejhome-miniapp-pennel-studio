#!/usr/bin/env node
/**
 * precheck.mjs — URL 프레임 프리체크(P2). 외부 URL 을 미니앱에 임베드할 수 있는가.
 *
 *   node src/precheck.mjs <url> [url2 ...]      # 실측 판정
 *   node src/precheck.mjs --test                # 헤더 픽스처 셀프테스트(네트워크 없음)
 *   node src/precheck.mjs --panel <panel.json>  # webContent 링크 일괄 판정 + 파일에 사유 기록
 *
 * 판정은 두 헤더로 한다 — `X-Frame-Options`(XFO) 와 CSP `frame-ancestors`.
 * 브라우저 규칙대로 **CSP frame-ancestors 가 있으면 그게 XFO 를 이긴다**.
 *
 *   A  임베드 허용      제약 없음. Ray web-view 로 임베드 가능(단 커스텀 렌더 호스트 능력 확인).
 *   B  제한적           허용 오리진에서만 프레임 가능(SAMEORIGIN·'self'·허용목록).
 *                       자체 호스팅/허용 오리진이면 embedded-selfhosted. 3rd파티 그대로면 external-browser.
 *   C  임베드 불가      DENY 또는 frame-ancestors 'none'. external-browser 뿐.
 *   ?  판정 불가        네트워크 실패 등. 안전하게 external-browser 로 본다.
 *
 * "정책"은 사이트 헤더에서 객관적으로 나오지만, "실현"은 우리가 그 콘텐츠를 호스팅할 수
 * 있는지에도 달렸다. 그래서 verdict(정책) 와 recommend(실현 모드)를 따로 낸다.
 */

/** CSP 문자열(들)에서 frame-ancestors 지시자를 뽑는다. 없으면 null. */
export function parseFrameAncestors(csp) {
  if (!csp) return null;
  // 여러 CSP 헤더가 콤마로 합쳐질 수 있다. 각 정책을 세미콜론으로 나눠 훑는다.
  const policies = String(csp).split(/,(?=[^;]*(?:;|$))/); // 대략적 분리 — 정책 경계
  for (const pol of [String(csp), ...policies]) {
    const m = pol.match(/frame-ancestors([^;]*)/i);
    if (m) {
      const sources = m[1].trim().split(/\s+/).filter(Boolean);
      return {
        raw: `frame-ancestors ${sources.join(' ')}`.trim(),
        sources,
        none: sources.length === 1 && sources[0].toLowerCase() === "'none'",
        wildcard: sources.includes('*'),
      };
    }
  }
  return null;
}

function verdict(code, recommend, reasons, summary) {
  return { verdict: code, recommend, summary, reasons };
}

/**
 * 헤더 → 판정. 순수 함수(네트워크 없음) — 테스트의 기준.
 * @param {{xFrameOptions?:string, contentSecurityPolicy?:string}} h
 */
export function classifyFrame(h) {
  const reasons = [];
  const fa = parseFrameAncestors(h.contentSecurityPolicy);

  if (fa) {
    reasons.push(`CSP ${fa.raw}`);
    if (fa.none) return verdict('C', 'external-browser', reasons, "frame-ancestors 'none' — 어떤 곳도 프레임 불가");
    if (fa.wildcard) return verdict('A', 'embedded', reasons, 'frame-ancestors * — 임베드 허용');
    return verdict('B', 'embedded-selfhosted', reasons,
      `허용 오리진(${fa.sources.join(' ')})에서만 프레임 가능 — 자체호스팅 경로. 3rd파티 그대로면 external-browser`);
  }

  const xfo = (h.xFrameOptions || '').trim().toUpperCase();
  if (!xfo) return verdict('A', 'embedded', reasons, 'X-Frame-Options·CSP frame-ancestors 없음 — 임베드 허용(단 Ray 호스트 능력 확인)');
  reasons.push(`X-Frame-Options: ${xfo}`);
  if (xfo === 'DENY') return verdict('C', 'external-browser', reasons, 'XFO DENY — 프레임 불가');
  if (xfo === 'SAMEORIGIN') return verdict('B', 'embedded-selfhosted', reasons,
    'XFO SAMEORIGIN — 동일 오리진만. 자체호스팅 경로. 3rd파티 그대로면 external-browser');
  if (xfo.startsWith('ALLOW-FROM')) return verdict('B', 'embedded-selfhosted', reasons,
    'XFO ALLOW-FROM(구식) — 특정 오리진만 허용');
  return verdict('C', 'external-browser', reasons, `XFO 인식 불가(${xfo}) — 안전하게 external-browser`);
}

/** URL 을 실제로 받아 헤더를 뽑고 분류한다. 실패하면 verdict '?'. */
export async function precheckUrl(url, { timeoutMs = 12000, fetchImpl = fetch } = {}) {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetchImpl(url, {
      method: 'GET',
      redirect: 'follow',
      signal: ctrl.signal,
      headers: { 'User-Agent': 'miniapp-panel-studio/precheck' },
    });
    const h = {
      xFrameOptions: res.headers.get('x-frame-options'),
      contentSecurityPolicy: res.headers.get('content-security-policy'),
    };
    const out = classifyFrame(h);
    out.url = url;
    out.finalUrl = res.url || url;
    out.status = res.status;
    if (out.finalUrl !== url) out.reasons.push(`리다이렉트 → ${out.finalUrl}`);
    return out;
  } catch (e) {
    return verdict('?', 'external-browser', [`요청 실패: ${e.name === 'AbortError' ? '시간 초과' : e.message}`],
      '판정 불가 — 안전하게 external-browser 로 본다');
  } finally {
    clearTimeout(t);
  }
}

const BADGE = { A: '🟢 A 임베드 허용', B: '🟡 B 제한적(자체호스팅)', C: '🔴 C 임베드 불가', '?': '⚪ ? 판정 불가' };

/** panel.json 의 webContent 링크를 일괄 판정한다. { key, url, ...verdict }[] */
export async function precheckPanel(panel, opts) {
  const out = [];
  for (const wc of panel.webContent || []) {
    const url = wc.url || panel.links?.[wc.key]?.url;
    if (!url) { out.push({ key: wc.key, verdict: '?', summary: 'URL 없음', reasons: [] }); continue; }
    const r = await precheckUrl(url, opts);
    out.push({ key: wc.key, url, ...r });
  }
  return out;
}

// ── 셀프테스트 픽스처 (네트워크 없이 분류기 검증) ──
const FIXTURES = [
  { name: '무제한', h: {}, want: 'A' },
  { name: 'frame-ancestors *', h: { contentSecurityPolicy: "frame-ancestors *" }, want: 'A' },
  { name: "frame-ancestors 'none'", h: { contentSecurityPolicy: "default-src 'self'; frame-ancestors 'none'" }, want: 'C' },
  { name: "frame-ancestors 'self' 허용목록", h: { contentSecurityPolicy: "frame-ancestors 'self' haatzmall.com *.haatzmall.com" }, want: 'B' },
  { name: 'XFO DENY', h: { xFrameOptions: 'DENY' }, want: 'C' },
  { name: 'XFO SAMEORIGIN', h: { xFrameOptions: 'SAMEORIGIN' }, want: 'B' },
  { name: 'CSP 가 XFO 를 이긴다(SAMEORIGIN+none→C)', h: { xFrameOptions: 'SAMEORIGIN', contentSecurityPolicy: "frame-ancestors 'none'" }, want: 'C' },
  { name: 'CSP 가 XFO 를 이긴다(DENY+*→A)', h: { xFrameOptions: 'DENY', contentSecurityPolicy: 'frame-ancestors *' }, want: 'A' },
  { name: 'haatzmall 실측', h: { xFrameOptions: 'SAMEORIGIN', contentSecurityPolicy: "frame-ancestors 'self' haatzmall.com *.haatzmall.com" }, want: 'B' },
];

function runSelftest() {
  let fail = 0;
  for (const f of FIXTURES) {
    const got = classifyFrame(f.h).verdict;
    const ok = got === f.want;
    if (!ok) fail++;
    console.log(`  ${ok ? '✔' : '✘'} ${f.name.padEnd(38)} 기대 ${f.want} · 판정 ${got}`);
  }
  console.log(`\n  ${fail ? '✘' : '✔'} 픽스처 ${FIXTURES.length}건 · 실패 ${fail}`);
  return fail === 0;
}

// CLI
if (process.argv[1] && (await import('node:url')).fileURLToPath(import.meta.url) === (await import('node:path')).resolve(process.argv[1])) {
  const args = process.argv.slice(2);
  if (args[0] === '--test') {
    console.log('── precheck 분류기 셀프테스트 ──');
    process.exit(runSelftest() ? 0 : 1);
  } else if (args[0] === '--panel') {
    const { readFileSync, writeFileSync } = await import('node:fs');
    const { resolve } = await import('node:path');
    const path = resolve(args[1]);
    const panel = JSON.parse(readFileSync(path, 'utf8'));
    const results = await precheckPanel(panel);
    console.log(`── webContent 프레임 프리체크 — ${panel.meta?.name || path} ──\n`);
    for (const r of results) {
      console.log(`  ${BADGE[r.verdict]}  [${r.key}] ${r.url || ''}`);
      console.log(`     ${r.summary}`);
      r.reasons.forEach(x => console.log(`       · ${x}`));
    }
    // 사유를 panel 에 기록(P1.5 의 webContent.reason gap 을 닫는다)
    if (args.includes('--write')) {
      for (const wc of panel.webContent || []) {
        const r = results.find(x => x.key === wc.key);
        if (r) { wc.verdict = r.verdict; wc.reason = `[프레임 프리체크 ${r.verdict}] ${r.summary} (${r.reasons.join(' · ')})`; }
      }
      writeFileSync(path, JSON.stringify(panel, null, 2) + '\n');
      console.log(`\n  ✔ webContent 에 verdict·reason(증거) 기록 → ${path}`);
      console.log(`     (mode 는 그대로 둔다 — 헤더 정책과 별개로 Ray 호스트 능력·자체호스팅 여부를 보고 사람이 정한다)`);
    } else {
      console.log(`\n  (--write 를 붙이면 위 판정을 panel.json 의 webContent.verdict·reason 에 기록한다)`);
    }
    process.exit(0); // fetch(undici) keep-alive 소켓이 이벤트 루프를 잡는다 — 명시 종료
  } else if (args.length) {
    for (const url of args) {
      const r = await precheckUrl(url);
      console.log(`\n${BADGE[r.verdict]}  ${url}`);
      if (r.status) console.log(`  HTTP ${r.status}${r.finalUrl && r.finalUrl !== url ? ` → ${r.finalUrl}` : ''}`);
      console.log(`  ${r.summary}`);
      r.reasons.forEach(x => console.log(`    · ${x}`));
      console.log(`  → 권장: ${r.recommend}`);
    }
    process.exit(0); // 위와 같음
  } else {
    console.error('usage: node src/precheck.mjs <url ...> | --test | --panel <panel.json> [--write]');
    process.exit(1);
  }
}
