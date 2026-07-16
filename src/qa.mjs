#!/usr/bin/env node
/**
 * qa.mjs — 실기기 QA 체크리스트 생성 (P5).
 *
 * 실기기 QA 는 자동화할 수 없다(오프라인·DP 리포트 경쟁·native 모듈 부재). 하지만
 * **무엇을 확인해야 하는지**는 패널에서 파생할 수 있다. 이 파일은 그 파생 체크리스트를
 * QA.md 로 낸다 — 테스터가 실기기 앞에서 그대로 따라가는 스크립트. "자동화 불가"를
 * "생성 가능한 체크리스트"로 바꾼다.
 *
 *   node src/qa.mjs <panel.json>     # QA.md 출력
 *   node src/qa.mjs --test           # 커버리지 셀프테스트(rw DP·webContent 누락 없나)
 *
 * generate 가 저장소에 QA.md 를 함께 낸다.
 */
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/** rw DP semantic → (동작, 기대) QA 문항. */
function rwStep(dp) {
  const n = dp.name || dp.code;
  switch (dp.semantic) {
    case 'power': return [`전원 토글`, `제품 전원이 실제로 켜지고/꺼지며 앱 상태가 따라온다`];
    case 'toggle': return [`${n} 스위치 토글`, `제품에 반영되고 앱 상태가 일치한다`];
    case 'mode-select': return [`${n} 각 값(${(dp.range || []).join('/')}) 선택`, `제품이 해당 모드로 전환된다`];
    case 'level-dial': return [`${n} 단계(${(dp.range || []).join('/')}) 조절`, `제품 풍량/세기가 단계대로 바뀐다`];
    case 'action': return [`${n} 실행`, `제품이 동작을 수행한다(예: 초기화 시퀀스)`];
    case 'schedule-time': return [`${n} 시간 저장`, `시간이 저장되고, 예약 스위치와 독립이다(저장≠켜짐)`];
    default: return [`${n}(${dp.type}) 값 변경`, `제품에 반영되고 앱 상태가 일치한다`];
  }
}

/** ro(렌더) DP → (확인, 기대). */
function roStep(dp) {
  const n = dp.name || dp.code;
  if (dp.semantic === 'grade-badge') return [`${n} 등급 뱃지`, `센서값 구간과 등급이 맞고, 색+텍스트를 함께 쓴다(색만 아님)`];
  return [`${n} 표시값${dp.unit ? `(${dp.unit})` : ''}`, `실기기 리포트값과 일치한다`];
}

/** panel → QA.md 문자열. */
export function buildQaChecklist(panel) {
  const dps = panel.dps || [];
  const rw = dps.filter(d => d.mode === 'rw' && d.semantic && d.semantic !== 'unrendered');
  const ro = dps.filter(d => d.mode === 'ro' && d.semantic && d.semantic !== 'unrendered');
  const faults = dps.filter(d => d.type === 'fault' || d.semantic === 'unrendered');
  const wc = panel.webContent || [];
  const name = panel.meta?.name || panel.meta?.id || '(이름 미정)';

  const L = [];
  const H = (t) => (L.push('', `## ${t}`, ''));
  const item = (a, e) => L.push(`- [ ] **${a}** — 기대: ${e}`);

  L.push(`# ${name} — 실기기 QA 체크리스트`);
  L.push('');
  L.push('> 자동 생성(miniapp-panel-studio). 실기기 QA 는 자동화할 수 없어, **사람이 확인할');
  L.push('> 항목**을 패널에서 파생해 여기 모았다. 실기기·실계정에서 아래를 따라가며 확인한다.');
  L.push('');
  L.push(`- 제어 DP ${rw.length} · 센서 DP ${ro.length} · 외부 링크 ${wc.length} · 결함 DP ${faults.length}`);

  if (rw.length) { H('1. DP 제어 (읽기·쓰기)'); for (const d of rw) item(...rwStep(d)); }
  if (ro.length) { H('2. 센서 표시 (읽기 전용)'); for (const d of ro) item(...roStep(d)); }

  H('3. 오프라인 · 통신');
  item('와이파이 차단 후 화면 확인', '오프라인 배너가 뜨고 컨트롤이 비활성/딤 처리된다');
  item('여러 DP 를 빠르게 연속 변경', '마지막 상태가 유지된다(setDps 배치 — 리포트 경쟁으로 되돌아가지 않음)');
  item('앱 재진입 시 상태', '실기기의 현재 DP 상태로 복원된다');

  if (wc.length) {
    H('4. 외부 링크 (웹인앱)');
    for (const w of wc) {
      const url = w.url || panel.links?.[w.key]?.url || '';
      if (w.mode === 'external-browser')
        item(`[${w.key}] 링크 탭`, `시스템 브라우저로 ${url} 가 열린다(미니앱 내 흰 화면 아님)`);
      else if (w.mode === 'embedded-selfhosted')
        item(`[${w.key}] 링크 탭`, `허용 오리진에서 임베드 렌더(호스트 web-view 능력 확인). 안 되면 external-browser 로`);
      else
        item(`[${w.key}] 링크 탭`, `임베드 렌더된다(Ray 커스텀 렌더 호스트의 web-view 지원 확인)`);
    }
  }

  if (faults.length) {
    H('5. 결함 처리');
    for (const d of faults)
      item(`${d.name || d.code} 결함 발생`, `서버 푸시로 알림이 오고, 패널은 이 DP 를 그리지 않는다(설계대로)`);
  }

  if (panel.theme?.mode?.startsWith('bespoke')) {
    H('6. 테마 (실물 미러)');
    item('패널 색과 실물 디스플레이 대조', `실물 색과 일치한다 — 사유: ${panel.theme.reason || '(테마 예외)'}`);
  }

  H('7. 접근성 · 터치');
  item('흑백으로 보기(색각 시뮬레이션)', '색만으로 상태를 나르지 않는다(점 모양·라벨 병행)');
  item('터치 타깃 크기', '누를 수 있는 것은 최소 44×44px 히트 영역');
  item('키보드/포커스(웹 뷰)', '포커스 링이 보이고 Tab 으로 모든 기능 접근');

  L.push('', '---', '', '자동 생성: miniapp-panel-studio · 항목이 패널과 어긋나면 panel.json 을 고치고 다시 생성한다.', '');
  return L.join('\n');
}

/** 셀프테스트: 모든 rw DP·webContent 가 체크리스트에 나오는가. */
function runSelftest() {
  const fixture = {
    meta: { name: 'QA 픽스처' },
    theme: { mode: 'bespoke-x', reason: 'r' },
    dps: [
      { code: 'switch', name: '전원', mode: 'rw', type: 'bool', semantic: 'power' },
      { code: 'mode', name: '모드', mode: 'rw', type: 'enum', range: ['a', 'b'], semantic: 'mode-select' },
      { code: 'temp', name: '온도', mode: 'ro', type: 'value', unit: '℃', semantic: 'metric' },
      { code: 'fault_remind', name: '오류', mode: 'ro', type: 'fault', semantic: 'unrendered' },
    ],
    webContent: [{ key: 'shop', url: 'https://x', mode: 'external-browser' }],
    links: { shop: { url: 'https://x' } },
  };
  const md = buildQaChecklist(fixture);
  const must = ['전원 토글', '모드 각 값', '온도 표시값', '[shop] 링크 탭', '와이파이 차단', '결함 발생', '실물 미러', '터치 타깃'];
  let fail = 0;
  for (const m of must) {
    const ok = md.includes(m);
    if (!ok) fail++;
    console.log(`  ${ok ? '✔' : '✘'} 포함: "${m}"`);
  }
  console.log(`\n  ${fail ? '✘' : '✔'} 커버리지 ${must.length}건 · 실패 ${fail}`);
  return fail === 0;
}

// CLI
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [, , arg] = process.argv;
  if (arg === '--test') {
    console.log('── QA 체크리스트 커버리지 셀프테스트 ──');
    process.exit(runSelftest() ? 0 : 1);
  }
  if (!arg) { console.error('usage: node src/qa.mjs <panel.json> | --test'); process.exit(1); }
  const panel = JSON.parse(readFileSync(resolve(arg), 'utf8'));
  process.stdout.write(buildQaChecklist(panel));
}
