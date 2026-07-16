/**
 * handoff.mjs — 개발자 인수인계 문서(HANDOFF.md) 생성.
 *
 * 저작도구는 패널의 3~4할(DP·위젯 바인딩·화면·색·한국어 초안)만 만든다. 나머지는
 * 개발자가 채운다. 그 "나머지"를 흰 화면으로 넘기지 않고, **무엇을·왜·누가** 형태의
 * 체크리스트로 저장소에 함께 실어 보낸다. git 으로 전달되면 이 문서가 곧 인수인계다.
 *
 * 입력은 두 갈래 모두 받는다:
 *  · lift 의 gaps (studio → panel.json 경로)     — 사유가 풍부한 정밀 gap
 *  · panel.json 자체 (generate → 저장소 경로)     — panel 구조에서 표준 미완성 추론
 * gaps 가 없으면 panel 에서 추론한다. 어느 쪽이든 커스텀 화면 슬롯은 panel 에서 뽑는다.
 */

const OWNER_LABEL = {
  'tuya-schema': 'Tuya 제품 스키마 (IoT 콘솔 · DP 정의)',
  translator: '번역',
  design: '디자인 · 에셋',
  dev: '개발자',
};
const SEV_BADGE = { blocker: '🔴 blocker', todo: '🟡 todo', note: '⚪ note' };
const SEV_ORDER = { blocker: 0, todo: 1, note: 2 };

/** gaps 가 없을 때 panel 구조에서 표준 미완성 지점을 추론한다. generate 의 preflight 도 쓴다. */
export function inferGaps(panel) {
  const gaps = [];
  const g = (path, severity, owner, phase, reason) => gaps.push({ path, severity, owner, phase, reason });

  for (const d of panel.dps || []) {
    if (d.id == null)
      g(`dps.${d.code}.id`, 'blocker', 'tuya-schema', 'P4', 'Tuya DP 번호 미할당 — 실기기와 통신 불가.');
    if (d.semantic == null)
      g(`dps.${d.code}.semantic`, 'todo', 'dev', 'P1.5', '어느 화면에도 바인딩되지 않아 표현 위젯 미정.');
    if (d.type === 'string' && d.maxlen == null)
      g(`dps.${d.code}.maxlen`, 'todo', 'tuya-schema', 'P4', '문자열 DP 최대 길이 미정.');
  }
  if (!panel.meta?.id) g('meta.id', 'blocker', 'dev', 'P1.5', 'panel id 미정 — 빌드 산출물 이름이라 없으면 생성 불가.');
  if (!panel.meta?.productTuya)
    g('meta.productTuya', 'blocker', 'tuya-schema', 'P4', 'Tuya 제품 등록 정보 미연결.');
  if (panel.theme?.mode?.startsWith('bespoke'))
    g('theme', 'note', 'dev', '-', `테마 예외(${panel.theme.mode}) — 사유를 지켜라: ${panel.theme.reason || ''}`);
  if (!panel.i18n?.en && panel.i18n?.ko)
    g('i18n.en', 'todo', 'translator', 'P1.5', '영문 로케일 없음 — 번역 필요.');
  if (!(panel.icons || []).length)
    g('icons', 'todo', 'design', 'P1.5', '아이콘 에셋 목록 비어 있음.');
  for (const w of panel.webContent || [])
    if (w.mode === 'external-browser' && !w.reason)
      g(`webContent.${w.key}.reason`, 'todo', 'dev', 'P2', '외부 브라우저로 여는 근거 미기재 — 프레임 프리체크로 확인.');
  return gaps;
}

/** 사람이 읽을 gap 경로 라벨. */
function pathLabel(path) {
  const seg = path.split('.');
  if (seg[0] === 'dps') return `DP \`${seg[1]}\` 의 \`${seg[2]}\``;
  if (seg[0] === 'i18n') return seg[1] === 'ko' ? '한국어 문구 (추가 키)' : `\`${seg[1]}\` 로케일`;
  if (seg[0] === 'theme' && seg[1] === 'color') return `테마 색 \`${seg[2]}\``;
  if (seg[0] === 'webContent') return `링크 \`${seg[1]}\` 의 외부 열기 사유`;
  if (seg[0] === 'meta') return `\`meta.${seg.slice(1).join('.')}\``;
  return `\`${path}\``;
}

export function buildHandoff({ panel, gaps, source = 'generate', name }) {
  const title = name || panel.meta?.name || panel.meta?.id || '(이름 미정)';
  const raw = gaps && gaps.length ? gaps : inferGaps(panel);

  // path 중복 제거 (먼저 나온 것 우선)
  const seen = new Map();
  for (const g of raw) if (!seen.has(g.path)) seen.set(g.path, g);
  const use = [...seen.values()];

  const blockers = use.filter(g => g.severity === 'blocker');
  const custom = (panel.routes || []).filter(r => r.custom);

  const L = [];
  L.push(`# ${title} — 개발자 인수인계`);
  L.push('');
  L.push('> 이 문서는 **자동 생성**된다 (miniapp-panel-studio). 저작도구가 패널의 데이터 레이어와');
  L.push('> 표준 위젯 바인딩까지 만들고, 나머지를 아래 체크리스트로 넘긴다. 항목을 닫는 방법은');
  L.push('> panel.json 을 고치고 다시 생성하는 것이다 (`dist/` 를 손대지 않는 design-guide 규칙과 같다).');
  L.push('');
  L.push(`- 생성 경로: \`${source}\``);
  L.push(`- 남은 항목: **${use.length}건** (🔴 blocker ${blockers.length} · 🟡 todo ${use.filter(g => g.severity === 'todo').length} · ⚪ note ${use.filter(g => g.severity === 'note').length})`);
  L.push(`- 커스텀 화면 슬롯: **${custom.length}개** (개발자가 위젯 배치)`);
  L.push('');

  if (blockers.length) {
    L.push('## 🔴 먼저 — 이게 없으면 빌드/동작 안 함');
    L.push('');
    for (const g of blockers) L.push(`- [ ] **${pathLabel(g.path)}** — ${g.reason}`);
    L.push('');
  }

  // 커스텀 화면 슬롯
  if (custom.length) {
    L.push('## 🚧 커스텀 화면 슬롯 — 위젯 배치');
    L.push('');
    L.push('생성된 골격(`src/pages/<name>/index.tsx`)에 표준 위젯을 배치한다. 데이터 레이어');
    L.push('(`useDpState`/`setDp`)는 이미 생성돼 있다. 각 화면이 노출하는 DP:');
    L.push('');
    const dpsBySemantic = (panel.dps || []).filter(d => d.semantic && d.semantic !== 'unrendered');
    for (const r of custom) {
      L.push(`- [ ] **${r.name}** (\`${r.route}\`) — \`src/pages/${r.name}/index.tsx\``);
    }
    L.push('');
    if (dpsBySemantic.length) {
      L.push('  바인딩 대상 DP (semantic → 권장 위젯):');
      for (const d of dpsBySemantic)
        L.push(`  - \`${d.code}\` (${d.type}) → **${d.semantic}**`);
      L.push('');
    }
  }

  // owner 별 TODO
  const rest = use.filter(g => g.severity !== 'blocker');
  const byOwner = {};
  for (const g of rest) (byOwner[g.owner] ??= []).push(g);
  const owners = Object.keys(byOwner).sort((a, b) => (a === 'dev' ? 1 : 0) - (b === 'dev' ? 1 : 0));

  if (rest.length) {
    L.push('## 남은 인수인계 항목');
    L.push('');
    for (const owner of owners) {
      const gs = byOwner[owner].sort((a, b) => SEV_ORDER[a.severity] - SEV_ORDER[b.severity]);
      L.push(`### ${OWNER_LABEL[owner] || owner}`);
      L.push('');
      for (const g of gs)
        L.push(`- [ ] ${SEV_BADGE[g.severity]} **${pathLabel(g.path)}** — ${g.reason}${g.phase && g.phase !== '-' ? `  _(${g.phase})_` : ''}`);
      L.push('');
    }
  }

  L.push('---');
  L.push('');
  L.push('생성: miniapp-panel-studio · 항목을 닫으려면 `panel.json` 수정 후 `npm run generate`.');
  L.push('용어·색·문구는 헤이홈 디자인 시스템(design-guide) 규격을 따른다.');
  L.push('');
  return L.join('\n');
}

const esc = s => String(s ?? '').replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
const SEV_HTML = {
  blocker: ['🔴', '#E03131'], todo: ['🟡', '#F0A020'], note: ['⚪', '#8B95A1'],
};

/**
 * 개발자 인수인계 뷰(HANDOFF.html) — 자립형 hej 스타일 HTML.
 * "무엇이 되어 있나 / 남은 것 / 어떻게 실행 / 규격 리포트" 한 장.
 * @param {{panel:object, gaps?:array, report?:object, stats?:object, source?:string, name?:string}} o
 */
export function buildHandoffHtml({ panel, gaps, report, stats = {}, source = 'generate', name }) {
  const title = name || panel.meta?.name || panel.meta?.id || '(이름 미정)';
  const raw = gaps && gaps.length ? gaps : inferGaps(panel);
  const seen = new Map();
  for (const g of raw) if (!seen.has(g.path)) seen.set(g.path, g);
  const use = [...seen.values()];
  const blockers = use.filter(g => g.severity === 'blocker');
  const rest = use.filter(g => g.severity !== 'blocker');
  const custom = (panel.routes || []).filter(r => r.custom);
  const boundDps = (panel.dps || []).filter(d => d.semantic && d.semantic !== 'unrendered');

  const byOwner = {};
  for (const g of rest) (byOwner[g.owner] ??= []).push(g);
  const owners = Object.keys(byOwner).sort((a, b) => (a === 'dev' ? 1 : 0) - (b === 'dev' ? 1 : 0));

  // "되어 있는 것" 요약
  const done = [
    [`DP ${(panel.dps || []).length}개`, `표현 위젯 바인딩 ${boundDps.length}개`],
    [`화면 ${(panel.routes || []).length}개`, custom.length ? `커스텀 슬롯 ${custom.length}개` : '표준'],
    [`테마 ${esc(panel.theme?.mode || '-')}`, panel.theme?.mode?.startsWith('bespoke') ? '실물 미러(사유 有)' : 'hej 시맨틱'],
    [`언어 ${Object.keys(panel.i18n || {}).join('·') || '-'}`, `링크 ${Object.keys(panel.links || {}).length}개`],
  ];
  if (stats.files) done.unshift([`파일 ${stats.files}개 생성`, 'Ray 저장소']);

  const gapCard = g => `<li class="gap"><span class="sev" style="color:${SEV_HTML[g.severity][1]}">${SEV_HTML[g.severity][0]} ${g.severity}</span>
    <b>${esc(pathLabelPlain(g.path))}</b>${g.phase && g.phase !== '-' ? `<span class="phase">${esc(g.phase)}</span>` : ''}
    <span class="reason">${esc(g.reason)}</span></li>`;

  const terms = report?.termHits || [];

  return `<!doctype html><html lang="ko"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} — 개발자 인수인계</title>
<style>
  :root { --green:#00A872; --surface:#007559; --ink:#10100E; --sub:#8B95A1; --bg:#F7F8FA; --card:#fff; --div:#E5E8EB; --danger:#E03131; }
  * { box-sizing:border-box; } body { margin:0; font-family:'SUIT Variable','SUIT',-apple-system,'Malgun Gothic',sans-serif; color:var(--ink); background:var(--bg); line-height:1.6; }
  .wrap { max-width:860px; margin:0 auto; padding:32px 20px 80px; }
  header { border-left:4px solid var(--green); padding:4px 0 4px 16px; margin-bottom:8px; }
  h1 { font-size:24px; margin:0 0 4px; font-weight:700; } .muted { color:var(--sub); font-size:13px; }
  .counts { display:flex; gap:8px; flex-wrap:wrap; margin:16px 0 4px; }
  .pill { background:var(--card); border:1px solid var(--div); border-radius:999px; padding:5px 12px; font-size:13px; }
  .pill b { font-weight:700; }
  h2 { font-size:16px; margin:28px 0 12px; font-weight:700; }
  .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(180px,1fr)); gap:10px; }
  .stat { background:var(--card); border:1px solid var(--div); border-radius:12px; padding:12px 14px; }
  .stat .k { font-weight:700; font-size:14px; } .stat .v { color:var(--sub); font-size:12px; }
  .banner { background:var(--surface); color:#fff; border-radius:12px; padding:14px 16px; margin:8px 0; }
  ul.gaps { list-style:none; padding:0; margin:0; display:grid; gap:8px; }
  li.gap { background:var(--card); border:1px solid var(--div); border-radius:10px; padding:10px 12px; font-size:13px; }
  li.gap .sev { font-weight:700; font-size:11px; margin-right:8px; }
  li.gap b { margin-right:8px; } li.gap .phase { color:var(--sub); font-size:11px; border:1px solid var(--div); border-radius:6px; padding:0 6px; margin-right:8px; }
  li.gap .reason { display:block; color:var(--sub); margin-top:2px; }
  .owner { font-size:13px; font-weight:700; color:var(--surface); margin:16px 0 8px; }
  code, pre { font-family:'SFMono-Regular',Consolas,monospace; }
  pre { background:var(--ink); color:#E8EAED; border-radius:12px; padding:14px 16px; overflow-x:auto; font-size:13px; }
  .slot { background:var(--card); border:1px solid var(--div); border-radius:10px; padding:10px 12px; font-size:13px; margin-bottom:6px; }
  .dp { display:inline-block; background:#F0F2F5; border-radius:6px; padding:1px 7px; margin:2px 4px 2px 0; font-size:12px; }
  footer { margin-top:40px; color:var(--sub); font-size:12px; border-top:1px solid var(--div); padding-top:16px; }
  a { color:var(--surface); }
</style></head><body><div class="wrap">
<header><h1>${esc(title)}</h1><div class="muted">개발자 인수인계 · 자동 생성(miniapp-panel-studio) · 경로 <code>${esc(source)}</code></div></header>
<div class="counts">
  <span class="pill">남은 항목 <b>${use.length}</b></span>
  <span class="pill" style="color:var(--danger)">🔴 blocker <b>${blockers.length}</b></span>
  <span class="pill">🟡 todo <b>${rest.filter(g => g.severity === 'todo').length}</b></span>
  <span class="pill">⚪ note <b>${rest.filter(g => g.severity === 'note').length}</b></span>
  <span class="pill">🚧 커스텀 슬롯 <b>${custom.length}</b></span>
</div>

<h2>무엇이 되어 있나</h2>
<div class="grid">${done.map(([k, v]) => `<div class="stat"><div class="k">${esc(k)}</div><div class="v">${esc(v)}</div></div>`).join('')}</div>

${blockers.length ? `<h2>🔴 먼저 — 이게 없으면 빌드·동작 안 함</h2>
<div class="banner">아래 ${blockers.length}건은 빌드 blocker 다. 대부분 Tuya DP 스키마에서 온다(P4 로 자동 로드).</div>
<ul class="gaps">${blockers.map(gapCard).join('')}</ul>` : ''}

${custom.length ? `<h2>🚧 커스텀 화면 슬롯 — 위젯 배치</h2>
<p class="muted"><code>src/pages/&lt;name&gt;/index.tsx</code> 에 위젯을 배치한다. 데이터 레이어(<code>useDpState</code>/<code>setDp</code>)는 생성돼 있다.</p>
${custom.map(r => `<div class="slot"><b>${esc(r.name)}</b> <span class="muted">${esc(r.route)}</span></div>`).join('')}
<p class="muted" style="margin-top:10px">바인딩 대상 DP (semantic → 권장 위젯):</p>
<div>${boundDps.map(d => `<span class="dp">${esc(d.code)} → <b>${esc(d.semantic)}</b></span>`).join('')}</div>` : ''}

${rest.length ? `<h2>남은 인수인계 항목</h2>
${owners.map(o => `<div class="owner">${esc(OWNER_LABEL[o] || o)}</div><ul class="gaps">${byOwner[o].sort((a, b) => SEV_ORDER[a.severity] - SEV_ORDER[b.severity]).map(gapCard).join('')}</ul>`).join('')}` : ''}

<h2>어떻게 실행하나</h2>
<pre>yarn install --frozen-lockfile
yarn build:tuya        # ray build -t tuya
# 항목을 닫으려면 panel.json 을 고치고 다시 생성한다:
#   npm run generate &lt;panel.json&gt;</pre>

${terms.length ? `<h2>헤이홈 규격 리포트</h2>
<p class="muted">용어 권장 대체어 ${terms.length}건 (막지 않고 알린다):</p>
<ul class="gaps">${terms.slice(0, 12).map(t => `<li class="gap"><b>${esc(t.term)} → ${esc(t.use)}</b><span class="reason">[${esc(t.key)}] ${esc(t.value)}</span></li>`).join('')}</ul>` : ''}

<footer>생성: miniapp-panel-studio · 용어·색·문구는 헤이홈 디자인 시스템(design-guide) 규격을 따른다. 이 문서와 <code>HANDOFF.md</code> 는 저장소에 함께 커밋된다.</footer>
</div></body></html>
`;
}

/** HTML 용 평문 라벨(백틱 제거판). */
function pathLabelPlain(path) {
  return pathLabel(path).replace(/`/g, '');
}
