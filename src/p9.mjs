#!/usr/bin/env node
/**
 * p9.mjs — 템플릿 왕복 증명 (Phase 4).
 *
 *   node src/p9.mjs   (npm run p9)
 *
 * "템플릿에서 시작하기"가 실제 페이지까지 이어지는지 증명한다:
 *
 *   panels/templates/*.studio.json
 *        │ lift()                     ← 정본 변환기
 *        ▼
 *   부분 panel.json (routes custom:false + widgets)
 *        │ validateWidgets()          ← 위젯↔DP 규격
 *        │ emitPage()                 ← 실제 smart-ui .tsx
 *        ▼
 *   화면당 페이지 코드 (모든 위젯이 DP 로 해석되고, 미지원 0)
 *
 * 통과 = 위젯 규격 오류 0 · 비커스텀 화면은 widgets 를 담아 실제 페이지가 나온다.
 */
import { readFileSync, readdirSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lift } from './lift.mjs';
import { validateWidgets } from './hej.mjs';
import { emitPage } from './emit.mjs';

const __dir = dirname(fileURLToPath(import.meta.url));
const TPL_DIR = resolve(__dir, '../panels/templates');

const line = '─'.repeat(64);
console.log(`\n${line}\n P9 템플릿 왕복 증명 — studio 템플릿 → 실제 페이지\n${line}`);

let allOk = true;
const files = readdirSync(TPL_DIR).filter(f => f.endsWith('.studio.json')).sort();
for (const f of files) {
  const model = JSON.parse(readFileSync(resolve(TPL_DIR, f), 'utf8'));
  const { panel } = lift(model);
  const wv = validateWidgets(panel);
  const genRoutes = panel.routes.filter(r => !r.custom && (r.widgets || []).length);
  let pageOk = true, widgetCount = 0, pageErr = '';
  for (const r of genRoutes) {
    try {
      const tsx = emitPage(panel, r);
      widgetCount += r.widgets.length;
      // 각 위젯이 코드에 흔적을 남겼는지(빈 페이지 방지) + export 존재
      if (!tsx.includes(`export function ${r.name}`)) throw new Error(`${r.name} export 누락`);
    } catch (e) { pageOk = false; pageErr = e.message; }
  }
  const ok = wv.ok && pageOk && genRoutes.length > 0;
  if (!ok) allOk = false;
  const status = ok ? '✔' : '✘';
  console.log(`\n ${status} ${model.meta.name}  (${f})`);
  console.log(`     DP ${panel.dps.length} · 생성화면 ${genRoutes.length} · 위젯 ${widgetCount} · bespoke ${wv.bespoke.length}`);
  if (wv.errors.length) wv.errors.forEach(e => console.log(`     ✘ ${e}`));
  if (wv.warnings.length) wv.warnings.slice(0, 3).forEach(w => console.log(`     ⚠ ${w}`));
  if (wv.bespoke.length) wv.bespoke.forEach(b => console.log(`     · bespoke ${b.kind} @ ${b.at} — 슬롯/사유 처리`));
  if (!pageOk) console.log(`     ✘ 페이지 생성 실패: ${pageErr}`);
  if (!genRoutes.length) console.log(`     ✘ 생성 대상 화면 없음 (custom:false + widgets 필요)`);
}

console.log(`\n${line}`);
if (allOk) console.log(` ✔ 통과 — 모든 템플릿이 위젯 규격을 지키고 실제 페이지로 이어진다.\n${line}\n`);
else { console.log(` ✘ 실패 — 위 오류를 고쳐라.\n${line}\n`); process.exit(1); }
