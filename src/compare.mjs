#!/usr/bin/env node
/**
 * compare.mjs — 원본 haatz 레포 vs panel.json 모델의 **의미 대조**.
 *
 *   node src/compare.mjs panels/haatz-r6.panel.json <원본레포경로>
 *
 * 바이트 비교가 아니다. 원본 파일에는 손으로 쓴 근거 주석이 가득한데, 그건 고정
 * 템플릿의 몫이지 모델에서 파생되는 것이 아니다. 대신 각 도메인의 **데이터**를
 * 양쪽에서 뽑아 같은지 본다 — DP·토큰·문구·링크·라우팅·아이콘.
 *
 * 통과 = 모델이 원본을 무손실로 담았다는 증거 (P0 왕복 검증).
 */
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const [, , panelArg, repoArg] = process.argv;
const panel = JSON.parse(readFileSync(resolve(panelArg), 'utf8'));
const REPO =
  repoArg ||
  '/tmp/claude-1000/-home-goqual-repos-work-design-guide/69b5678d-36c2-4eed-b382-af4f11549eba/scratchpad/haatz-r6-miniapp-panel';

const read = rel => readFileSync(resolve(REPO, rel), 'utf8');
const results = [];

/** 두 집합/맵을 비교해 결과 행을 만든다. */
function cmp(domain, orig, model) {
  const oKeys = Object.keys(orig).sort();
  const mKeys = Object.keys(model).sort();
  const missing = oKeys.filter(k => !(k in model));
  const extra = mKeys.filter(k => !(k in orig));
  const mismatched = oKeys.filter(k => k in model && String(orig[k]) !== String(model[k]));
  const ok = !missing.length && !extra.length && !mismatched.length;
  results.push({ domain, total: oKeys.length, ok, missing, extra, mismatched, orig });
}

/* ── 1. DP 코드 (dpCodes.ts) ── */
{
  const src = read('src/config/dpCodes.ts');
  const body = src.slice(src.indexOf('export default {'));
  const orig = {};
  for (const m of body.matchAll(/(\w+):\s*'([a-z0-9_]+)',/g)) orig[m[2]] = m[2];
  const model = Object.fromEntries(panel.dps.map(d => [d.code, d.code]));
  cmp('DP 코드', orig, model);
}

/* ── DP 스키마 (schema.ts) — 객체 블록 단위로 파싱해 경계 누수를 막는다 ── */
const dpBlocks = (() => {
  const src = read('src/devices/schema.ts');
  const arr = between(src, 'defaultSchema = [', '] as const;');
  // 각 DP 객체: `code:` 를 포함하는 `{ ... },` 조각
  return [...arr.matchAll(/\{[\s\S]*?\n  \},/g)]
    .map(m => m[0])
    .filter(b => /code:\s*'/.test(b));
})();

/* ── 2. DP enum 레인지 ── */
{
  const orig = {};
  for (const b of dpBlocks) {
    const code = b.match(/code:\s*'([a-z0-9_]+)'/)?.[1];
    const rangeM = b.match(/range:\s*\[([^\]]*)\]/);
    if (code && rangeM) {
      const range = rangeM[1].match(/'([a-z0-9_]+)'/g)?.map(s => s.replace(/'/g, '')) ?? [];
      orig[code] = range.join(',');
    }
  }
  const model = Object.fromEntries(
    panel.dps.filter(d => d.type === 'enum').map(d => [d.code, (d.range || []).join(',')])
  );
  cmp('DP enum 레인지', orig, model);
}

/* ── 3. DP value 레인지 ── */
{
  const orig = {};
  for (const b of dpBlocks) {
    const code = b.match(/code:\s*'([a-z0-9_]+)'/)?.[1];
    const v = b.match(/type:\s*'value',\s*min:\s*(\d+),\s*max:\s*(\d+),\s*scale:\s*(\d+),\s*step:\s*(\d+),\s*unit:\s*'([^']*)'/);
    if (code && v) orig[code] = `${v[1]}-${v[2]}/${v[3]}/${v[4]}/${v[5]}`;
  }
  const model = Object.fromEntries(
    panel.dps.filter(d => d.type === 'value').map(d => [d.code, `${d.min}-${d.max}/${d.scale}/${d.step}/${d.unit}`])
  );
  cmp('DP value 레인지', orig, model);
}

/* ── 4. 테마 COLOR (theme.ts) ── */
{
  const src = read('src/config/theme.ts');
  const colorBlock = between(src, 'export const COLOR', '} as const;');
  const orig = extractPairs(colorBlock, /(\w+):\s*'(#[0-9A-Fa-f]+|rgba?\([^)]*\))'/g);
  const model = Object.fromEntries(Object.entries(panel.theme.color).map(([k, v]) => [k, v.toUpperCase()]));
  // 대소문자 무시 비교
  for (const k in orig) orig[k] = orig[k].toUpperCase();
  cmp('테마 COLOR', orig, model);
}

/* ── 5. 테마 AQ_COLOR + OPACITY ── */
{
  const src = read('src/config/theme.ts');
  const aq = extractPairs(between(src, 'AQ_COLOR', '};'), /(\w+):\s*'(#[0-9A-Fa-f]+)'/g);
  const modelAq = { ...panel.theme.aqColor };
  for (const k in aq) aq[k] = aq[k].toUpperCase();
  for (const k in modelAq) modelAq[k] = modelAq[k].toUpperCase();
  cmp('테마 AQ_COLOR', aq, modelAq);

  const op = extractPairs(between(src, 'export const OPACITY', '} as const;'), /(\w+):\s*(0?\.\d+)/g);
  cmp('테마 OPACITY', op, mapStr(panel.theme.opacity));
}

/* ── 6. 외부 링크 (links.ts) ── */
{
  const src = read('src/config/links.ts');
  const block = between(src, 'WEB_LINKS', '} as const;');
  const orig = extractPairs(block, /(\w+):\s*'([^']+)'/g);
  const model = Object.fromEntries(Object.entries(panel.links).map(([k, v]) => [k, v.url]));
  cmp('외부 링크', orig, model);
}

/* ── 7. 라우팅 (routes.config.ts) ── */
{
  const src = read('src/routes.config.ts');
  const orig = {};
  for (const m of src.matchAll(/route:\s*'([^']+)',\s*path:\s*'([^']+)',\s*name:\s*'([^']+)'/g))
    orig[m[3]] = `${m[1]}|${m[2]}`;
  const model = Object.fromEntries(panel.routes.map(r => [r.name, `${r.route}|${r.path}`]));
  cmp('라우팅', orig, model);
}

/* ── 8. i18n ko / en ── */
for (const lang of ['ko', 'en']) {
  const src = read('src/i18n/strings.ts');
  const block = langBlock(src, lang);
  const orig = extractPairs(block, /(\w+):\s*\n?\s*'((?:[^'\\]|\\.)*)'/g);
  cmp(`i18n ${lang}`, orig, panel.i18n[lang]);
}

/* ── 9. 아이콘 (res/index.ts) ── */
{
  const src = read('src/res/index.ts');
  const block = between(src, 'export default {', '};');
  const orig = {};
  for (const m of block.matchAll(/(icon_\w+),/g)) orig[m[1]] = m[1];
  const model = Object.fromEntries(panel.icons.map(n => [n, n]));
  cmp('아이콘', orig, model);
}

/* ── 출력 ── */
console.log(`\n원본:  ${REPO}`);
console.log(`모델:  ${resolve(panelArg)}\n`);
console.log('도메인                  원본  일치  상태');
console.log('─'.repeat(52));
let allOk = true;
let totalItems = 0;
let totalMatch = 0;
for (const r of results) {
  const match = r.total - r.missing.length - r.mismatched.length;
  totalItems += r.total;
  totalMatch += match;
  if (!r.ok) allOk = false;
  const status = r.ok ? '✔' : '✘';
  console.log(`${r.domain.padEnd(22)} ${String(r.total).padStart(4)} ${String(match).padStart(5)}  ${status}`);
  if (!r.ok) {
    if (r.missing.length) console.log(`      누락(원본엔 있는데 모델에 없음): ${r.missing.join(', ')}`);
    if (r.extra.length) console.log(`      과잉(모델에만 있음): ${r.extra.join(', ')}`);
    for (const k of r.mismatched) console.log(`      값 불일치 [${k}]: 원본='${r.orig[k]}'`);
  }
}
console.log('─'.repeat(52));
console.log(`합계  ${totalMatch}/${totalItems} 일치  ${allOk ? '✔ 무손실' : '✘ 차이 있음'}\n`);
process.exit(allOk ? 0 : 1);

/* ── helpers ── */
function between(src, startMark, endMark) {
  const s = src.indexOf(startMark);
  if (s < 0) return '';
  const e = src.indexOf(endMark, s);
  return src.slice(s, e < 0 ? undefined : e);
}
function extractPairs(block, re) {
  const out = {};
  for (const m of block.matchAll(re)) out[m[1]] = m[2];
  return out;
}
function mapStr(obj) {
  return Object.fromEntries(Object.entries(obj).map(([k, v]) => [k, String(v)]));
}
function langBlock(src, lang) {
  const s = src.indexOf(`${lang}: {`);
  if (s < 0) return '';
  // 다음 최상위 언어 키 또는 끝까지
  const next = lang === 'ko' ? src.indexOf('en: {', s + 1) : src.length;
  return src.slice(s, next < 0 ? undefined : next);
}
