/**
 * 헤이홈 디자인 시스템 연결 — design-guide 저장소의 `dist/` 를 소비한다.
 *
 * 저작도구는 자체 팔레트를 만들지 않는다. 색·간격·서체의 정본은 design-guide 이고,
 * 여기서 그 산출물(`dist/tokens.json`, `tokens/terminology.json`)을 읽어
 *   1) 신규 패널의 **기본 토큰**을 hej 시맨틱으로 채우고
 *   2) 완성물이 **hej 규격**을 지키는지 검사한다.
 *
 * design-guide 위치는 기본 `../design-guide`, 환경변수 `HEJ_DESIGN_DIR` 로 덮는다.
 */
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __dir = dirname(fileURLToPath(import.meta.url));
const HEJ_DIR = process.env.HEJ_DESIGN_DIR || resolve(__dir, '../../design-guide');

function loadJson(rel) {
  return JSON.parse(readFileSync(resolve(HEJ_DIR, rel), 'utf8'));
}

let _tokens = null;
let _terms = null;

export function hejTokens() {
  if (!_tokens) _tokens = loadJson('dist/tokens.json');
  return _tokens;
}

export function hejTerms() {
  if (!_terms) {
    try {
      _terms = loadJson('tokens/terminology.json');
    } catch {
      _terms = { banned: [], allowContexts: [] };
    }
  }
  return _terms;
}

/** hej 시맨틱 토큰 값. 예: resolveSemantic('primary.primary-surface','light'). */
export function resolveSemantic(group, theme = 'light') {
  const t = hejTokens();
  const v = t[theme]?.[group];
  return typeof v === 'object' ? v?.value : v;
}

/** 규격 스케일 — 모델이 쓰는 간격·모서리 값이 이 안에 있어야 한다. */
export function hejScale() {
  const t = hejTokens();
  return {
    spacing: t.scale.spacing,
    radius: Object.entries(t.scale.radius)
      .filter(([k]) => !k.startsWith('$'))
      .map(([, v]) => v),
    fontWeight: t.scale.typography?.fontWeight ?? { normal: 500, bold: 700 },
  };
}

/**
 * UI 문구 용어 검사 — design-guide 의 `terminology.json` 규칙을 그대로 적용한다.
 * `npm run lint:terms` 와 같은 근거다. 위반은 막지 않고 **경고로 보고**한다:
 * haatz 처럼 실물 표기를 따르는 패널은 예외일 수 있으나, 비개발자가 규격에서
 * 벗어난 것을 **알고** 결정하게 한다.
 */
export function lintTerms(i18n) {
  const { banned = [], allowContexts = [] } = hejTerms();
  const hits = [];
  for (const [lang, table] of Object.entries(i18n || {})) {
    if (lang !== 'ko') continue; // 한글 UI 문구 규칙
    for (const [key, value] of Object.entries(table)) {
      if (typeof value !== 'string') continue;
      if (allowContexts.some(ctx => value.includes(ctx))) continue;
      for (const rule of banned) {
        if (rule.term && value.includes(rule.term)) {
          hits.push({ key, term: rule.term, use: rule.use, why: rule.why, value });
        }
      }
    }
  }
  return hits;
}

/**
 * 테마 규격 검사.
 *
 * design-guide 의 `$exemptContrast` 와 같은 원칙: 규격을 벗어나려면 **사유**가
 * 있어야 하고, 조용한 예외는 없다.
 *  - mode 'hej-semantic' → hej 시맨틱 토큰만 쓴다 (규격 준수).
 *  - mode 'bespoke-*'    → 사유(reason)가 필수. 없으면 실패.
 */
export function validateTheme(panel) {
  const theme = panel.theme || {};
  const out = { mode: theme.mode, ok: true, warnings: [], errors: [] };
  if (String(theme.mode || '').startsWith('bespoke')) {
    if (!theme.reason || !theme.reason.trim()) {
      out.ok = false;
      out.errors.push('bespoke 테마는 reason 이 필수다 (design-guide $exemptContrast 원칙).');
    } else {
      out.warnings.push(`bespoke 테마 — hej 시맨틱 대신 실물 색을 쓴다. 사유: ${theme.reason}`);
    }
  }
  return out;
}

/**
 * 위젯 규격 검사 — route.widgets 가 DP 와 호환되는지, 미지원 위젯은 없는지,
 * 그리고 규격을 벗어나는 bespoke 비주얼(raw 컬러·등급색)을 **선언**하는지 본다.
 * 스튜디오 에디터가 accepts 로 이미 막지만, 손으로 쓴/템플릿 panel.json 을 위한 방어선이다.
 * (design-guide "조용한 예외 없음" 원칙 — 규격 밖 위젯은 사유·소유가 드러나야 한다.)
 */
const WIDGET_SPEC = {
  // bool
  power: { dp: ['bool'], mode: 'rw' }, toggle: { dp: ['bool'], mode: 'rw' }, statusIndicator: { dp: ['bool'], mode: 'ro' },
  // value rw
  slider: { dp: ['value'], mode: 'rw' }, stepper: { dp: ['value'], mode: 'rw' }, circleDial: { dp: ['value'], mode: 'rw' },
  brightnessSlider: { dp: ['value'], mode: 'rw' }, colorTempSlider: { dp: ['value'], mode: 'rw' },
  percentSlider: { dp: ['value'], mode: 'rw' }, temperatureDial: { dp: ['value'], mode: 'rw' },
  // value (ro/rw 무관 표시)
  metric: { dp: ['value'] }, sensorRow: { dp: ['value'] }, gauge: { dp: ['value'] }, progress: { dp: ['value'] },
  powerMetric: { dp: ['value'] }, humidityRing: { dp: ['value'] }, energyChart: { dp: ['value'] },
  // enum rw
  modeSelect: { dp: ['enum'], mode: 'rw' }, levelDial: { dp: ['enum'], mode: 'rw' }, picker: { dp: ['enum'], mode: 'rw' },
  hvacModeTabs: { dp: ['enum'], mode: 'rw' }, sceneGrid: { dp: ['enum'], mode: 'rw' }, openCloseStop: { dp: ['enum'], mode: 'rw' },
  // enum ro (bespoke 등급색)
  gradeBadge: { dp: ['enum'], mode: 'ro', bespoke: 'aqColor(등급색)' },
  // action / string / fault
  action: { dp: ['enum', 'bool'], mode: 'rw' }, scheduleRow: { dp: ['string'], mode: 'rw' }, timePicker: { dp: ['string'], mode: 'rw' },
  faultList: { dp: ['fault'] },
  // color (bespoke raw HSV)
  hsvColorWheel: { dp: ['color'], mode: 'rw', bespoke: 'color-raw(HSV)' },
  // 비-DP (링크/텍스트)
  linkTile: { link: true }, ctaButton: { link: true }, adBanner: { link: true }, adHero: { link: true }, adPoint: { text: true },
};

export function validateWidgets(panel) {
  const out = { ok: true, errors: [], warnings: [], bespoke: [] };
  const dpByCode = Object.fromEntries((panel.dps || []).map(d => [d.code, d]));
  for (const route of panel.routes || []) {
    for (const w of route.widgets || []) {
      const spec = WIDGET_SPEC[w.type];
      const at = `${route.name}/${w.type}`;
      if (!spec) { out.ok = false; out.errors.push(`${at}: 미지원 위젯 타입.`); continue; }
      if (spec.link) { if (!w.link) out.warnings.push(`${at}: 링크 위젯인데 link 가 비었다.`); continue; }
      if (spec.text) continue;
      const dp = dpByCode[w.dp];
      if (!dp) { out.ok = false; out.errors.push(`${at}: DP '${w.dp}' 없음.`); continue; }
      if (spec.dp && !spec.dp.includes(dp.type))
        { out.ok = false; out.errors.push(`${at}: DP '${w.dp}'(${dp.type}) 타입 불호환 — ${spec.dp.join('|')} 필요.`); }
      if (spec.mode && dp.mode !== spec.mode)
        out.warnings.push(`${at}: DP '${w.dp}'(${dp.mode}) 모드 권장(${spec.mode})과 다름.`);
      if (spec.bespoke) out.bespoke.push({ at, dp: w.dp, kind: spec.bespoke });
    }
  }
  return out;
}

export const HEJ_INFO = { dir: HEJ_DIR };
