#!/usr/bin/env node
/**
 * p6.mjs — 신규 제품 일반성 증명 (P6).
 *
 *   node src/p6.mjs   (npm run p6)
 *
 * 파이프라인이 haatz 에 과적합되지 않았음을 두 번째 제품(스마트 플러그)으로 증명한다.
 * haatz 과 전혀 다른 DP·테마·화면 구성을 저작 → lift → Tuya 적용 → generate 까지
 * 태워, blocker 0 으로 실제 Ray 저장소가 나오는지 본다. 산출물(HANDOFF·QA)도 함께 검증.
 *
 * P1.5 가 haatz 왕복을 증명했다면, P6 는 "그 파이프라인이 임의 제품에 적용된다"를 증명한다.
 */
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lift } from './lift.mjs';
import { applyTuyaToPanel } from './tuya.mjs';
import { inferGaps } from './handoff.mjs';
import { generate } from './generate.mjs';

const __dir = dirname(fileURLToPath(import.meta.url));
const R = rel => resolve(__dir, '..', rel);

const PRODUCT = {
  id: 'plug-mini',
  studio: 'panels/plug-mini.studio.json',
  tuya: 'panels/plug-mini.tuya.json',
};

const line = '─'.repeat(64);
console.log(`\n${line}\n P6 일반성 증명 — 신규 제품 '${PRODUCT.id}' 전 파이프라인 통과\n${line}`);

// 1) 저작 모델 → lift → 부분 panel
const model = JSON.parse(readFileSync(R(PRODUCT.studio), 'utf8'));
const { panel, gaps } = lift(model);
const blk0 = inferGaps(panel).filter(g => g.severity === 'blocker');
console.log(`  1. lift  — DP ${panel.dps.length} · 화면 ${panel.routes.length} · blocker ${blk0.length}`);

// 2) Tuya 적용 (DP id·productTuya 채움)
const spec = JSON.parse(readFileSync(R(PRODUCT.tuya), 'utf8'));
const applied = applyTuyaToPanel(panel, spec);
console.log(`  2. tuya  — DP id 채움 ${applied.filledIds} · productTuya ${panel.meta.productTuya ? '채움' : '없음'}`);
if (applied.unmatchedTuya.length) console.log(`     ⚠ panel 에 없는 Tuya DP: ${applied.unmatchedTuya.join(', ')}`);
if (applied.unmatchedPanel.length) console.log(`     ⚠ 여전히 id 없는 DP: ${applied.unmatchedPanel.join(', ')}`);

// 3) 개발자 슬러그(meta.id) 부여 — Tuya 가 못 주는 유일한 blocker
panel.meta.id = PRODUCT.id;

const blk1 = inferGaps(panel).filter(g => g.severity === 'blocker');
console.log(`  3. meta.id 부여 후 blocker ${blk1.length}${blk1.length ? ` (${blk1.map(g => g.path).join(', ')})` : ''}`);

// 4) generate — 실제 Ray 저장소가 나오는가
const outPanel = R(`out/${PRODUCT.id}.panel.json`);
mkdirSync(dirname(outPanel), { recursive: true });
writeFileSync(outPanel, JSON.stringify(panel, null, 2) + '\n');
const result = generate(outPanel, R(`out/${PRODUCT.id}`));

const fail = [];
if (blk1.length !== 0) fail.push(`blocker 가 0 이 아니다 (${blk1.length})`);
if (result.blocked) fail.push('generate 가 blocked 되었다');
if (!result.written || result.written.length < 10) fail.push(`생성 파일이 너무 적다 (${result.written?.length})`);
const hasHandoff = (result.written || []).includes('HANDOFF.md');
const hasQa = (result.written || []).includes('QA.md');
if (!hasHandoff) fail.push('HANDOFF.md 미생성');
if (!hasQa) fail.push('QA.md 미생성');

console.log(`  4. generate — ${result.blocked ? 'BLOCKED' : `파일 ${result.written.length}개`} · HANDOFF ${hasHandoff ? '✔' : '✘'} · QA ${hasQa ? '✔' : '✘'}`);
console.log(`     테마 ${panel.theme.mode} · 용어 검사 ${result.report?.termHits?.length ?? 0}건`);

if (fail.length) {
  console.log(`\n${line}\n ✘ 실패 — ${fail.join(' · ')}\n${line}\n`);
  process.exit(1);
}
console.log(`\n${line}\n ✔ 통과 — 신규 제품이 저작→lift→tuya→generate 전 구간을 blocker 0 으로 통과.`);
console.log(`   파이프라인은 haatz 전용이 아니다. → ${result.root}\n${line}\n`);
