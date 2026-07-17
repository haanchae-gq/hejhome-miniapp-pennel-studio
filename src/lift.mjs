#!/usr/bin/env node
/**
 * lift.mjs — 저작 모델(authoring model) → 정본 panel.json 으로 **들어올린다**.
 *
 *   node src/lift.mjs panels/haatz-r6.studio.json [out.panel.json]
 *
 * 스튜디오(web/studio.html)가 편집하는 것은 panel.json 이 아니라 더 단순한 저작
 * 모델이다. 사람이 비개발자로서 채울 수 있는 것 — DP·위젯 바인딩·화면·색·한국어
 * 문구 — 만 담는다. 정본 panel.json 은 그보다 훨씬 넓다(Tuya DP 번호·영문·아이콘·
 * 결함 코드·Tuya 프로젝트 설정 등).
 *
 * lift 는 **파생 가능한 것만 파생**한다:
 *   camel(코드에서) · semantic(위젯 바인딩에서) · routes(화면에서) · type/range/min/max
 *   · theme 색(있는 키) · webContent(링크 mode 에서) · i18n.ko(작성된 키).
 *
 * 나머지는 만들지 않는다. **조용히 비우지 않고** `gaps` 로 사유와 함께 반환한다.
 * 이 gaps 가 곧 개발자 인수인계(HANDOFF.md)의 TODO 이고, p1.mjs 왕복 증명의 기준이다.
 * design-guide 의 "$exemptContrast — 조용한 면제는 없다" 와 같은 규율이다.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const camel = s => s.replace(/_([a-z])/g, (_, c) => c.toUpperCase());

/** 위젯 type → dp semantic. 저작 모델의 위젯 바인딩이 semantic 을 무손실로 담는다. */
const WIDGET_SEMANTIC = {
  power: 'power',
  levelDial: 'level-dial',
  metric: 'metric',
  gradeBadge: 'grade-badge',
  modeSelect: 'mode-select',
  scheduleRow: 'schedule-time',
  action: 'action',
  sensorRow: 'sensor-row',
  toggle: 'toggle',
  // 범용 DP 컨트롤 (Phase 1)
  statusIndicator: 'status-indicator',
  slider: 'slider',
  stepper: 'stepper',
  circleDial: 'value-dial',
  gauge: 'gauge',
  progress: 'progress',
  picker: 'picker',
  timePicker: 'time-picker',
  faultList: 'fault-list',
  // 카테고리 비즈니스 위젯 (Phase 2)
  hsvColorWheel: 'color-hsv',
  brightnessSlider: 'brightness',
  colorTempSlider: 'color-temp',
  sceneGrid: 'scene-select',
  temperatureDial: 'temperature-dial',
  hvacModeTabs: 'hvac-mode',
  humidityRing: 'humidity-ring',
  openCloseStop: 'cover-control',
  percentSlider: 'percent-slider',
  powerMetric: 'power-metric',
  energyChart: 'energy-chart',
};

/** 화면에 바인딩되지 않은 DP 의 semantic 을 타입으로 추론한다. */
function fallbackSemantic(dp) {
  if (dp.type === 'fault') return 'unrendered'; // 패널이 안 그린다 — 서버 푸시 담당
  if (dp.type === 'bool') return 'toggle';
  return null;
}

/**
 * 저작 모델 → { panel, gaps }.
 *  panel : 파생된 부분 panel.json (불완전할 수 있다)
 *  gaps  : 저작 모델이 표현하지 못하는 것들. { path, kinds, reason, owner, phase, severity }
 *          - path     : panel.json 안의 위치 (p1 이 diff 를 이 gap 에 대응시킨다. '*' 는 한 세그먼트 와일드카드)
 *          - kinds    : 이 gap 이 흡수하는 diff 종류 ('missing' | 'mismatch')
 *          - owner    : 누가 채우나 (tuya-schema | translator | design | dev)
 *          - phase    : 로드맵상 어디서 닫히나
 *          - severity : blocker(빌드 불가) | todo | note
 */
export function lift(model) {
  const gaps = [];
  const gap = g => gaps.push(g);

  // ── 위젯 바인딩 맵 (dp code → semantic) ──
  const boundSemantic = {};
  for (const screen of model.screens || []) {
    for (const w of screen.widgets || []) {
      if (w.dp && WIDGET_SEMANTIC[w.type]) boundSemantic[w.dp] = WIDGET_SEMANTIC[w.type];
    }
  }

  // ── meta ──
  const panel = { meta: { name: model.meta.name, deviceKey: model.meta.deviceKey } };
  gap({ path: '$schema', kinds: ['missing'], severity: 'note', owner: 'dev', phase: 'P1.5',
        reason: 'panel.json 스키마 포인터. 정본 파일에만 붙는 편집기 힌트라 저작 모델엔 없다.' });
  gap({ path: 'dps.*.note', kinds: ['missing'], severity: 'note', owner: 'dev', phase: '-',
        reason: 'DP 주석(오타 의도·서버 푸시 담당 등 사람이 남긴 근거). 저작 모델은 자유 주석 필드를 담지 않는다.' });
  gap({ path: 'meta.id', kinds: ['missing'], severity: 'blocker', owner: 'dev', phase: 'P1.5',
        reason: 'panel.json 고유 id. 저작 모델엔 없다. generate 의 출력 디렉터리·패키지 이름이라 없으면 빌드 불가.' });
  gap({ path: 'meta.framework', kinds: ['missing'], severity: 'note', owner: 'dev', phase: 'P1.5',
        reason: '고정값 ray-tuya. lift 가 임의로 박지 않고 명시적으로 남긴다.' });
  gap({ path: 'meta.reference', kinds: ['missing'], severity: 'note', owner: 'dev', phase: 'P1.5',
        reason: '참조 원본 저장소 경로. 신규 패널이면 없을 수 있다.' });
  gap({ path: 'meta.productTuya', kinds: ['missing'], severity: 'blocker', owner: 'tuya-schema', phase: 'P4',
        reason: 'Tuya 제품 등록 정보(productId·baseversion·의존 Kit 버전 등). Tuya IoT 콘솔에서 온다. 저작 모델 바깥.' });
  gap({ path: 'meta.functionalPages', kinds: ['missing'], severity: 'todo', owner: 'tuya-schema', phase: 'P4',
        reason: 'Tuya functional-pages appid·entryCode. Tuya 콘솔 발급값.' });

  // ── dps ──
  panel.dps = (model.dps || []).map(d => {
    const out = { code: d.code, camel: camel(d.code), name: d.name, mode: d.mode, type: d.type };
    if (d.range) out.range = d.range;
    if (d.type === 'value') {
      if (d.min != null) out.min = d.min;
      if (d.max != null) out.max = d.max;
      if (d.unit != null) out.unit = d.unit;
      // scale·step 은 관례로 찍지 않는다 — 제품마다 다르고(전력 scale 1·kWh scale 3) Tuya DP 정의값이다.
      gap({ path: `dps.${d.code}.scale`, kinds: ['missing'], severity: 'todo', owner: 'tuya-schema', phase: 'P4',
            reason: '값 DP 소수 자리(scale). 제품마다 다르다 — Tuya DP 정의값.' });
      gap({ path: `dps.${d.code}.step`, kinds: ['missing'], severity: 'todo', owner: 'tuya-schema', phase: 'P4',
            reason: '값 DP 증가 단위(step). Tuya DP 정의값.' });
    }
    const sem = boundSemantic[d.code] || fallbackSemantic(d);
    if (sem) out.semantic = sem;
    else gap({ path: `dps.${d.code}.semantic`, kinds: ['missing'], severity: 'todo', owner: 'dev', phase: 'P1.5',
               reason: `DP '${d.code}' 가 어느 화면에도 바인딩되지 않아 semantic 을 추론할 수 없다.` });

    // 파생 불가한 DP 메타데이터
    gap({ path: `dps.${d.code}.id`, kinds: ['missing'], severity: 'blocker', owner: 'tuya-schema', phase: 'P4',
          reason: 'Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.' });
    if (d.type === 'string')
      gap({ path: `dps.${d.code}.maxlen`, kinds: ['missing'], severity: 'todo', owner: 'tuya-schema', phase: 'P4',
            reason: '문자열 DP 최대 길이. Tuya DP 정의값.' });
    if (d.type === 'fault') {
      gap({ path: `dps.${d.code}.label`, kinds: ['missing'], severity: 'todo', owner: 'tuya-schema', phase: 'P4',
            reason: '결함 비트 라벨 목록(error_1..n). Tuya DP 정의값.' });
      gap({ path: 'faults', kinds: ['missing'], severity: 'todo', owner: 'dev', phase: 'P4',
            reason: '결함 코드 → 한국어 메시지 매핑. 저작 모델에 없다. 실기기 결함 사양에서 온다.' });
    }
    // code 가 의도적 오타라 자동 camel 과 정본 교정본이 다를 수 있는 알려진 자리
    if (camel(d.code) !== camelExpectedOverride(d.code))
      gap({ path: `dps.${d.code}.camel`, kinds: ['mismatch'], severity: 'note', owner: 'dev', phase: 'P1.5',
            reason: `DP code '${d.code}' 가 의도적 오타(Tuya DP 정의 일치)라, 자동 camel(${camel(d.code)})과 정본 교정 식별자가 다르다. 저작 모델은 code 만 담아 교정 정보를 표현 못 한다.` });
    return out;
  });

  // ── theme ──
  panel.theme = {
    mode: model.theme.mode,
    reason: model.theme.reason,
    color: { ...model.theme.color },
    aqColor: { ...model.theme.aqColor },
  };
  gap({ path: 'theme.base', kinds: ['missing'], severity: 'note', owner: 'dev', phase: 'P1.5',
        reason: '테마 베이스(dark-fixed 등). 저작 모델은 색 값만 담고 베이스 이름은 안 담는다.' });
  gap({ path: 'theme.opacity', kinds: ['missing'], severity: 'todo', owner: 'dev', phase: 'P1.5',
        reason: '상태 투명도(offline·disabled·pressed). 저작 모델 색 팔레트 밖.' });
  for (const k of ['bgSheet', 'bgSheetAlert', 'textOnSheetAlert', 'alertCancel', 'toggleOff', 'powerOff'])
    gap({ path: `theme.color.${k}`, kinds: ['missing'], severity: 'todo', owner: 'design', phase: 'P1.5',
          reason: `보조 색 '${k}'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.` });

  // ── links / webContent ──
  panel.links = {};
  panel.webContent = [];
  for (const [key, v] of Object.entries(model.links || {})) {
    panel.links[key] = { url: v.url, desc: v.desc };
    if (v.image) panel.links[key].image = v.image;         // 배너 WebP(data URI) — generate 가 src/res 로 뽑는다
    if (v.imageMeta) panel.links[key].imageMeta = v.imageMeta;
    if (v.mode) {
      panel.webContent.push({ key, mode: v.mode });
      gap({ path: `webContent.${key}.reason`, kinds: ['missing'], severity: 'todo', owner: 'dev', phase: 'P2',
            reason: `링크 '${key}' 의 외부 열기 사유(프레임 임베드 불가 판정 근거 등). URL 프레임 프리체크(P2)가 채운다.` });
    }
  }

  // ── routes (화면에서 완전 파생) ──
  //  custom:true  = 수제 슬롯 (위젯 안 담음 — 개발자 인수). haatz 처럼 실기기 미러링 화면.
  //  custom:false = 표준 위젯 배치 → generator 가 실제 smart-ui 페이지를 낸다(Phase 3).
  panel.routes = (model.screens || []).map(s => {
    const custom = s.custom ?? false;
    const route = { route: s.route, path: `/pages/${s.name}/index`, name: s.name, custom };
    if (!custom) route.widgets = (s.widgets || []).map(projectWidget);
    return route;
  });

  // ── icons ──
  panel.icons = [];
  gap({ path: 'icons', kinds: ['missing'], severity: 'todo', owner: 'design', phase: 'P1.5',
        reason: '아이콘 에셋 이름 목록. 저작 모델은 아이콘을 열거하지 않는다 — 디자인 발주/에셋에서 온다.' });

  // ── i18n ──
  panel.i18n = { ko: { ...(model.i18n_ko || {}) } };
  gap({ path: 'i18n.ko.*', kinds: ['missing'], severity: 'todo', owner: 'dev', phase: 'P1.5',
        reason: '저작 모델이 담은 것보다 많은 한국어 키가 필요하다(확인 다이얼로그·도움말·표 헤더 등). 작성되지 않은 키.' });
  gap({ path: 'i18n.en', kinds: ['missing'], severity: 'todo', owner: 'translator', phase: 'P1.5',
        reason: '영문 로케일 전체. 저작 모델은 한국어만 담는다 — 번역 필요.' });

  return { panel, gaps };
}

/** 저작 위젯 → panel.json route.widgets 항목. 스튜디오 전용 필드(id)는 뺀다. */
function projectWidget(w) {
  const o = { type: w.type };
  for (const k of ['dp', 'labelKey', 'switchDp', 'link', 'text']) if (w[k] != null) o[k] = w[k];
  return o;
}

/** 정본이 code 오타를 교정한 것으로 알려진 camel. 모르면 자동 camel 과 같다고 본다. */
function camelExpectedOverride(code) {
  const KNOWN = { on_schdule_switch: 'onScheduleSwitch' };
  return KNOWN[code] ?? camel(code);
}

// CLI
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [, , inArg, outArg] = process.argv;
  if (!inArg) {
    console.error('usage: node src/lift.mjs <studio.json> [out.panel.json]');
    process.exit(1);
  }
  const model = JSON.parse(readFileSync(resolve(inArg), 'utf8'));
  const { panel, gaps } = lift(model);
  if (outArg) {
    writeFileSync(resolve(outArg), JSON.stringify(panel, null, 2) + '\n');
    console.log(`✔ 들어올린 부분 panel.json → ${outArg}`);
  } else {
    console.log(JSON.stringify(panel, null, 2));
  }
  const bySev = gaps.reduce((m, g) => ((m[g.severity] = (m[g.severity] || 0) + 1), m), {});
  console.error(`\n── 저작 모델이 표현 못 한 gap ${gaps.length}건 ──`);
  console.error(`  blocker ${bySev.blocker || 0} · todo ${bySev.todo || 0} · note ${bySev.note || 0}`);
  console.error(`  (개발자 인수인계 TODO 가 된다 — handoff.mjs)`);
}
