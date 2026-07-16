#!/usr/bin/env node
/**
 * p7.mjs — 스튜디오↔CLI 왕복 게이트 (P7).
 *
 *   node src/p7.mjs   (npm run p7)
 *
 * 왕복은 세 손을 거친다: 스튜디오 export(.studio.json) → CLI `precheck --studio --write`
 * (links 에 verdict 기록) → 스튜디오 '가져오기'(FileReader)로 재수입 → 링크 인스펙터에
 * 프레임 판정 배지. 브라우저·네트워크가 낀 왕복이라, 여기선 그 접점인 `precheckStudio` 의
 * 매핑을 **mock fetch** 로 오프라인 검증한다(분류기 자체는 p2 가 검증).
 */
import { precheckStudio } from './precheck.mjs';

// 헤더를 URL 로 흉내내는 mock fetch (네트워크 없음)
const HEADERS = {
  'https://deny': { 'x-frame-options': 'DENY' },
  'https://open': {},
  'https://self': { 'content-security-policy': "frame-ancestors 'self' *.x.com" },
};
const mockFetch = async (url) => ({
  url, status: 200,
  headers: { get: (k) => HEADERS[url]?.[k.toLowerCase()] ?? null },
});

const model = {
  meta: { name: 'round-trip 픽스처' },
  links: {
    a: { url: 'https://deny' },
    b: { url: 'https://open' },
    c: { url: 'https://self' },
    d: { desc: 'URL 없는 링크는 건너뛴다' },
  },
};

const res = await precheckStudio(model, { fetchImpl: mockFetch });
const got = Object.fromEntries(res.map(r => [r.key, r.verdict]));
const want = { a: 'C', b: 'A', c: 'B' }; // d 는 url 없음 → 결과에 없음

let fail = 0;
for (const [k, v] of Object.entries(want)) {
  const ok = got[k] === v;
  if (!ok) fail++;
  console.log(`  ${ok ? '✔' : '✘'} [${k}] 기대 ${v} · 판정 ${got[k]}`);
}
const dSkipped = !('d' in got);
console.log(`  ${dSkipped ? '✔' : '✘'} [d] URL 없음 → 건너뜀`);
if (!dSkipped) fail++;

// --write 시 저작 모델 links 에 verdict 가 실제로 박히는지(스튜디오가 읽는 필드)
for (const r of res) model.links[r.key].verdict = r.verdict;
const written = model.links.a.verdict === 'C' && model.links.c.verdict === 'B';
console.log(`  ${written ? '✔' : '✘'} links[].verdict 기록(스튜디오 배지 소스)`);
if (!written) fail++;

const line = '─'.repeat(64);
if (fail) { console.log(`\n${line}\n ✘ P7 실패 — ${fail}건\n${line}\n`); process.exit(1); }
console.log(`\n${line}\n ✔ P7 통과 — precheckStudio 가 링크→verdict 를 매핑하고 저작 모델에 되쓴다.`);
console.log(`   왕복: export → precheck --studio --write → 가져오기 → 판정 배지.\n${line}\n`);
