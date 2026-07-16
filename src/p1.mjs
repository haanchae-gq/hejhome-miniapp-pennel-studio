#!/usr/bin/env node
/**
 * p1.mjs — P1.5 왕복 증명. 스튜디오 저작 모델이 검증된 파이프라인에 무손실로 들어가는가.
 *
 *   node src/p1.mjs   (npm run p1)
 *
 * P0.5 가 "생성 저장소가 실제로 ray build 된다"를 증명했듯, P1.5 는 "스튜디오가 뱉는
 * 것이 실제 generator 가 소비하는 panel.json 과 이어진다"를 증명한다. 방법:
 *
 *   저작 seed(panels/haatz-r6.studio.json)
 *        │ lift()                    ← 브라우저 재구현이 아니라 정본 변환기
 *        ▼
 *   부분 panel.json  ──deep-diff──▶  정본 panels/haatz-r6.panel.json
 *
 * 모든 diff 를 세 갈래로 분류한다:
 *   ✔ reproduced   : lift 가 정본과 똑같이 파생한 것 (저작 모델이 담는 부분)
 *   ○ declared-gap : lift 가 못 만든다고 **사유와 함께** 선언한 것 (인수인계 TODO)
 *   ✘ drift        : lift 가 **틀리게** 만들었거나(값 불일치·유령 필드), 선언 없이 빠뜨린 것
 *
 * 통과 = drift 0. 저작 모델은 불완전해도 되지만, 모든 불완전은 **선언되어야** 한다.
 * (design-guide 의 "조용한 면제는 없다"와 같은 규율.)
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lift } from './lift.mjs';
import { buildHandoff } from './handoff.mjs';

const __dir = dirname(fileURLToPath(import.meta.url));
const R = rel => resolve(__dir, '..', rel);

/** panel 을 leafPath→value 로 평탄화. 자연키가 있는 배열은 그 키로 묶는다. */
const ARRAY_KEY = { dps: 'code', routes: 'name', webContent: 'key' };
function flatten(node, path, out) {
  if (Array.isArray(node)) {
    if (path === 'icons') {
      for (const el of node) out[`icons.${el}`] = true; // 집합 원소
      return;
    }
    const key = ARRAY_KEY[path];
    if (key) {
      for (const el of node) flatten(el, `${path}.${el[key]}`, out);
      return;
    }
    out[path] = JSON.stringify(node); // range·label 등 원시 배열은 단일 잎
    return;
  }
  if (node && typeof node === 'object') {
    for (const [k, v] of Object.entries(node)) flatten(v, path ? `${path}.${k}` : k, out);
    return;
  }
  out[path] = node;
}

/** gap.path(와일드카드 '*' / 접두 허용)가 diff 경로를 포함하는가. */
function gapMatches(gapPath, diffPath) {
  const g = gapPath.split('.'), d = diffPath.split('.');
  if (g.length > d.length) return false;
  return g.every((seg, i) => seg === '*' || seg === d[i]);
}

function classify(lifted, canon, gaps) {
  const L = {}, C = {};
  flatten(lifted, '', L);
  flatten(canon, '', C);

  const reproduced = [], declared = [], drift = [];
  const paths = new Set([...Object.keys(L), ...Object.keys(C)]);

  for (const p of paths) {
    const inL = p in L, inC = p in C;
    let kind;
    if (inL && inC) {
      if (String(L[p]) === String(C[p])) { reproduced.push(p); continue; }
      kind = 'mismatch';
    } else if (inC && !inL) {
      kind = 'missing'; // 정본엔 있는데 lift 가 안 만듦
    } else {
      kind = 'extra'; // lift 가 정본에 없는 걸 만듦 — 언제나 drift
    }

    const covered = kind !== 'extra' &&
      gaps.some(g => g.kinds.includes(kind) && gapMatches(g.path, p));
    if (covered) declared.push({ path: p, kind });
    else drift.push({ path: p, kind, lift: L[p], canon: C[p] });
  }
  return { reproduced, declared, drift, gaps };
}

// ── 실행 ──
const model = JSON.parse(readFileSync(R('panels/haatz-r6.studio.json'), 'utf8'));
const canon = JSON.parse(readFileSync(R('panels/haatz-r6.panel.json'), 'utf8'));
const { panel, gaps } = lift(model);
const { reproduced, declared, drift } = classify(panel, canon, gaps);

const line = '─'.repeat(64);
console.log(`\n${line}\n P1.5 왕복 증명 — studio 저작 모델 → panel.json\n${line}`);
console.log(`  ✔ reproduced   ${String(reproduced.length).padStart(3)}  (저작 모델이 무손실로 담는 필드)`);
console.log(`  ○ declared-gap ${String(declared.length).padStart(3)}  (사유 있는 인수인계 TODO)`);
console.log(`  ✘ drift        ${String(drift.length).padStart(3)}  (틀린 파생·유령 필드·미선언 누락)`);

// 도메인별 커버리지 요약
const domainOf = p => p.split('.')[0];
const domains = {};
for (const p of reproduced) (domains[domainOf(p)] ??= { ok: 0, gap: 0 }).ok++;
for (const d of declared) (domains[domainOf(d.path)] ??= { ok: 0, gap: 0 }).gap++;
console.log(`\n  도메인       재현 / gap`);
for (const [dom, s] of Object.entries(domains).sort())
  console.log(`   ${dom.padEnd(11)} ${String(s.ok).padStart(3)}  / ${s.gap}`);

// gap 을 owner·severity 별로
console.log(`\n── 인수인계 gap ${gaps.length}건 (owner 별) ──`);
const byOwner = {};
for (const g of gaps) (byOwner[g.owner] ??= []).push(g);
for (const [owner, gs] of Object.entries(byOwner).sort()) {
  const blk = gs.filter(g => g.severity === 'blocker').length;
  console.log(`   ${owner.padEnd(12)} ${String(gs.length).padStart(2)}건${blk ? `  (blocker ${blk})` : ''}`);
}

if (drift.length) {
  console.log(`\n✘ DRIFT — 저작 모델이 담는데 lift 가 틀렸다. 선언되지 않은 발산:`);
  for (const d of drift.slice(0, 20))
    console.log(`   [${d.kind}] ${d.path}\n       lift=${JSON.stringify(d.lift)}  canon=${JSON.stringify(d.canon)}`);
  if (drift.length > 20) console.log(`   … 외 ${drift.length - 20}건`);
  console.log(`\n${line}\n ✘ 실패 — drift 를 없애라. 저작 모델이 담는 필드는 정확히 파생돼야 한다.\n${line}\n`);
  process.exit(1);
}

// 인수인계 문서 생성 (git 에 커밋되는 산출물)
const handoff = buildHandoff({ panel, gaps, source: 'studio-lift', name: model.meta.name });
const outPath = R('HANDOFF.example.md');
writeFileSync(outPath, handoff);
console.log(` 인수인계 문서 → ${outPath}`);
console.log(`\n${line}\n ✔ 통과 — drift 0. 저작 모델의 모든 불완전이 사유와 함께 선언됨.`);
console.log(`   재현 ${reproduced.length} · 선언된 gap ${declared.length}. studio → panel.json → (P0/P0.5) → Ray 저장소.\n${line}\n`);
