#!/usr/bin/env node
/**
 * tuya.mjs — Tuya OpenAPI 직결 (P4). 제품의 DP 스펙을 받아 panel 에 채운다.
 *
 * P1.5 가 남긴 blocker 의 대부분(dps.*.id·maxlen·label, meta.productTuya)은 **Tuya 제품
 * DP 스키마**에서 온다. 이 파일이 그 스키마를 받아 panel 에 병합해 그 blocker 를 닫는다.
 *
 *   node src/tuya.mjs --fixture <spec.json> --panel <panel.json> [--write]   # 오프라인(저장된 스펙)
 *   node src/tuya.mjs --product <productId> --panel <panel.json> [--write]   # 라이브(env 크리덴셜 필요)
 *   node src/tuya.mjs --selftest                                             # 매핑 셀프테스트
 *
 * 라이브는 env: TUYA_ACCESS_ID · TUYA_ACCESS_SECRET · TUYA_ENDPOINT(기본 openapi.tuyaus.com).
 * 서명은 Tuya Cloud v2 규격(HMAC-SHA256). 크리덴셜이 없으면 --fixture 로 같은 매핑을 검증한다.
 */
import { createHash, createHmac } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/* ── DP 타입·값 매핑 (검증의 핵심) ───────────────────────────────────── */

/** Tuya function/status 타입 → panel dp 타입. */
export function mapTuyaType(t) {
  const T = { bool: 'bool', boolean: 'bool', enum: 'enum', value: 'value', integer: 'value',
              string: 'string', bitmap: 'fault', fault: 'fault', raw: 'raw', json: 'raw' };
  return T[String(t || '').toLowerCase()] || 'raw';
}

/** Tuya function/status 한 건 → panel dp 부분(부분 필드). */
export function mapTuyaDp(fn) {
  const type = mapTuyaType(fn.type);
  const dp = { id: fn.dp_id ?? fn.abilityId ?? fn.id, code: fn.code, type };
  let v = {};
  try { v = typeof fn.values === 'string' ? JSON.parse(fn.values || '{}') : (fn.values || {}); } catch { v = {}; }
  if (type === 'enum' && Array.isArray(v.range)) dp.range = v.range;
  if (type === 'value') {
    if (v.min != null) dp.min = v.min;
    if (v.max != null) dp.max = v.max;
    if (v.scale != null) dp.scale = v.scale;
    if (v.step != null) dp.step = v.step;
    if (v.unit != null) dp.unit = v.unit;
  }
  if (type === 'string' && v.maxlen != null) dp.maxlen = v.maxlen;
  if (type === 'fault' && Array.isArray(v.label)) dp.label = v.label;
  return dp;
}

/** Tuya 스펙(functions+status) → { code: dpPartial }. */
export function tuyaSpecToDps(spec) {
  const all = [...(spec.functions || []), ...(spec.status || [])];
  const out = {};
  for (const fn of all) out[fn.code] = mapTuyaDp(fn);
  return out;
}

/**
 * Tuya 스펙을 panel 에 병합한다. code 로 맞춰 id 와 빠진 메타(maxlen·label·scale…)를 채운다.
 * 이미 있는 값은 덮지 않는다(저작자가 정한 것을 존중). meta.productTuya 도 있으면 채운다.
 * @returns {{panel, applied:string[], filledIds:number, unmatchedTuya:string[], unmatchedPanel:string[]}}
 */
export function applyTuyaToPanel(panel, spec) {
  const byCode = tuyaSpecToDps(spec);
  const applied = [];
  let filledIds = 0;
  const panelCodes = new Set((panel.dps || []).map(d => d.code));

  for (const d of panel.dps || []) {
    const t = byCode[d.code];
    if (!t) continue;
    const before = JSON.stringify(d);
    for (const [k, val] of Object.entries(t)) {
      if (k === 'code') continue;
      if (d[k] == null) { d[k] = val; if (k === 'id') filledIds++; }
    }
    if (JSON.stringify(d) !== before) applied.push(d.code);
  }

  // meta.productTuya (제품 등록 정보)
  if (spec.product && !panel.meta.productTuya) {
    panel.meta.productTuya = spec.product;
  }

  const unmatchedTuya = Object.keys(byCode).filter(c => !panelCodes.has(c));
  const unmatchedPanel = (panel.dps || []).filter(d => d.id == null).map(d => d.code);
  return { panel, applied, filledIds, unmatchedTuya, unmatchedPanel };
}

/* ── Tuya Cloud v2 서명 클라이언트 (라이브) ──────────────────────────── */

const EMPTY_BODY_SHA = createHash('sha256').update('').digest('hex');

function sign(str, secret) {
  return createHmac('sha256', secret).update(str, 'utf8').digest('hex').toUpperCase();
}

/** t·nonce·stringToSign 으로 Tuya 서명 헤더를 만든다. accessToken 이 있으면 비즈니스 서명. */
function signedHeaders({ accessId, secret, accessToken = '', method, path, bodySha = EMPTY_BODY_SHA, nonce = '' }) {
  const t = String(tuyaNow());
  const stringToSign = [method, bodySha, '', path].join('\n');
  const str = accessId + accessToken + t + nonce + stringToSign;
  return {
    client_id: accessId,
    sign: sign(str, secret),
    t, nonce,
    sign_method: 'HMAC-SHA256',
    ...(accessToken ? { access_token: accessToken } : {}),
  };
}

// Date.now() 는 워크플로 스크립트에선 막혀 있으나 일반 실행에선 쓴다. 테스트 경로는 이걸 안 탄다.
function tuyaNow() { return Date.now(); }

async function tuyaFetch(endpoint, path, headers) {
  const res = await fetch(endpoint + path, { headers });
  const json = await res.json();
  if (!json.success) throw new Error(`Tuya API ${path}: ${json.msg || res.status} (code ${json.code})`);
  return json.result;
}

/** 라이브: 제품 DP 스펙을 받는다. env 크리덴셜 필요. */
export async function fetchProductSpec(productId, env = process.env) {
  const accessId = env.TUYA_ACCESS_ID, secret = env.TUYA_ACCESS_SECRET;
  const endpoint = env.TUYA_ENDPOINT || 'https://openapi.tuyaus.com';
  if (!accessId || !secret) throw new Error('TUYA_ACCESS_ID·TUYA_ACCESS_SECRET 환경변수가 필요하다 (또는 --fixture 를 써라).');

  const tokenPath = '/v1.0/token?grant_type=1';
  const token = await tuyaFetch(endpoint, tokenPath, signedHeaders({ accessId, secret, method: 'GET', path: tokenPath }));
  const access_token = token.access_token;

  const path = `/v1.0/iot-03/products/${productId}/functions`;
  const spec = await tuyaFetch(endpoint, path, signedHeaders({ accessId, secret, accessToken: access_token, method: 'GET', path }));
  // Tuya 응답을 우리 fixture shape 로 정규화
  return { product_id: productId, functions: spec.functions || [], status: spec.status || [] };
}

/* ── 셀프테스트 (오프라인 매핑 검증) ─────────────────────────────────── */
function runSelftest() {
  const cases = [
    ['bool → bool', { code: 'sw', dp_id: 1, type: 'bool', values: '{}' }, { id: 1, code: 'sw', type: 'bool' }],
    ['enum → range', { code: 'm', dp_id: 3, type: 'enum', values: '{"range":["a","b"]}' }, { id: 3, code: 'm', type: 'enum', range: ['a', 'b'] }],
    ['value → min/max/unit', { code: 'h', dp_id: 105, type: 'value', values: '{"min":0,"max":100,"scale":0,"step":1,"unit":"%"}' },
      { id: 105, code: 'h', type: 'value', min: 0, max: 100, scale: 0, step: 1, unit: '%' }],
    ['string → maxlen', { code: 's', dp_id: 132, type: 'string', values: '{"maxlen":5}' }, { id: 132, code: 's', type: 'string', maxlen: 5 }],
    ['bitmap → fault+label', { code: 'f', dp_id: 107, type: 'bitmap', values: '{"label":["e1","e2"]}' }, { id: 107, code: 'f', type: 'fault', label: ['e1', 'e2'] }],
    ['abilityId 별칭', { code: 'x', abilityId: 9, type: 'bool', values: '{}' }, { id: 9, code: 'x', type: 'bool' }],
  ];
  let fail = 0;
  for (const [name, input, want] of cases) {
    const got = mapTuyaDp(input);
    const ok = JSON.stringify(got) === JSON.stringify(want);
    if (!ok) fail++;
    console.log(`  ${ok ? '✔' : '✘'} ${name}`);
    if (!ok) console.log(`      got  ${JSON.stringify(got)}\n      want ${JSON.stringify(want)}`);
  }
  console.log(`\n  ${fail ? '✘' : '✔'} 매핑 ${cases.length}건 · 실패 ${fail}`);
  return fail === 0;
}

// CLI
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const argv = process.argv.slice(2);
  const opt = (k) => { const i = argv.indexOf(k); return i >= 0 ? argv[i + 1] : undefined; };

  if (argv.includes('--selftest')) {
    console.log('── Tuya DP 매핑 셀프테스트 ──');
    process.exit(runSelftest() ? 0 : 1);
  }

  const panelArg = opt('--panel');
  if (!panelArg) {
    console.error('usage: node src/tuya.mjs (--fixture <spec.json> | --product <id>) --panel <panel.json> [--write]  |  --selftest');
    process.exit(1);
  }
  const panelPath = resolve(panelArg);
  const panel = JSON.parse(readFileSync(panelPath, 'utf8'));

  let spec;
  if (opt('--fixture')) spec = JSON.parse(readFileSync(resolve(opt('--fixture')), 'utf8'));
  else if (opt('--product')) spec = await fetchProductSpec(opt('--product'));
  else { console.error('--fixture <spec.json> 또는 --product <id> 중 하나가 필요하다.'); process.exit(1); }

  const before = (panel.dps || []).filter(d => d.id == null).length;
  const r = applyTuyaToPanel(panel, spec);
  console.log(`\n── Tuya DP 적용 — ${panel.meta?.name || panelPath} ──`);
  console.log(`  DP id 채움: ${r.filledIds}건 (적용 전 미할당 ${before} → 후 ${r.unmatchedPanel.length})`);
  console.log(`  병합된 DP: ${r.applied.join(', ') || '없음'}`);
  if (spec.product) console.log(`  meta.productTuya: ${panel.meta.productTuya ? '채움' : '이미 있음/생략'}`);
  if (r.unmatchedTuya.length) console.log(`  ⚠ panel 에 없는 Tuya DP: ${r.unmatchedTuya.join(', ')}`);
  if (r.unmatchedPanel.length) console.log(`  ⚠ 여전히 id 없는 panel DP: ${r.unmatchedPanel.join(', ')}`);

  if (argv.includes('--write')) {
    writeFileSync(panelPath, JSON.stringify(panel, null, 2) + '\n');
    console.log(`\n  ✔ panel 갱신 → ${panelPath}`);
  } else {
    console.log(`\n  (--write 를 붙이면 panel.json 에 기록한다)`);
  }
  process.exit(0);
}
