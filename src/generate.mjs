#!/usr/bin/env node
/**
 * generate.mjs — panel.json → 완전한 Ray 저장소.
 *
 *   node src/generate.mjs panels/haatz-r6.panel.json [outDir]
 *
 * 낸다:
 *  · 데이터 레이어  (emit.mjs)      — DP·토큰·문구·링크·라우팅
 *  · 고정 템플릿    (templates.mjs) — seam·i18n·openExternal·빌드설정
 *  · 커스텀 슬롯    — 각 페이지의 index.config.ts + 개발자가 채울 index.tsx 골격
 *  · hej 규격 리포트 (hej.mjs)      — 테마 예외 사유 + 용어 검사
 */
import { readFileSync, mkdirSync, writeFileSync, rmSync, cpSync, existsSync, readdirSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as E from './emit.mjs';
import * as T from './templates.mjs';
import { validateTheme, lintTerms, HEJ_INFO } from './hej.mjs';
import { buildHandoff, buildHandoffHtml, inferGaps } from './handoff.mjs';
import { buildQaChecklist } from './qa.mjs';

const __dir = dirname(fileURLToPath(import.meta.url));
const SCAFFOLD = resolve(__dir, '../templates/scaffold');

function write(root, rel, content) {
  const p = resolve(root, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, content);
  return rel;
}

/** 프레임워크 공통 고정 파일을 그대로 복사 (landmine 픽스 포함). package.json 은 이름만 치환. */
function copyScaffold(root, panel) {
  const copied = [];
  const walk = (dir, base = '') => {
    for (const name of readdirSync(dir, { withFileTypes: true })) {
      if (name.name === 'package.reference.json') continue;
      const rel = base ? `${base}/${name.name}` : name.name;
      const abs = resolve(dir, name.name);
      if (name.isDirectory()) walk(abs, rel);
      else {
        cpSync(abs, resolve(root, rel));
        copied.push(rel);
      }
    }
  };
  walk(SCAFFOLD);
  // package.json = 검증된 원본 deps + 패널 이름
  const ref = JSON.parse(readFileSync(resolve(SCAFFOLD, 'package.reference.json'), 'utf8'));
  ref.name = panel.meta.id;
  writeFileSync(resolve(root, 'package.json'), JSON.stringify(ref, null, 2) + '\n');
  copied.push('package.json');
  return copied;
}

/** 패널 에셋(아이콘 PNG)을 src/res 로 복사. panels/<id>.assets/res 에서. */
function copyAssets(root, panelPath) {
  const assetsDir = panelPath.replace(/\.panel\.json$/, '.assets');
  const resDir = resolve(assetsDir, 'res');
  const out = [];
  if (existsSync(resDir)) {
    for (const f of readdirSync(resDir)) {
      cpSync(resolve(resDir, f), resolve(root, 'src/res', f));
      out.push(`src/res/${f}`);
    }
  }
  return out;
}

/** 스튜디오가 넣은 배너 이미지(data URI, 이미 WebP)를 src/res/ad-<key>.webp 로 뽑는다.
 *  이미 WebP 라 sharp 없이 동기로 기록한다. 애니메이션 GIF→WebP 는 npm run assets 로 별도. */
function writeEmbeddedAssets(root, panel) {
  const out = [];
  for (const [key, lk] of Object.entries(panel.links || {})) {
    if (!lk.image) continue;
    const m = /^data:(image\/\w+)?;base64,(.*)$/s.exec(lk.image);
    if (!m) continue;
    const ext = (m[1] || 'image/webp').split('/')[1].replace('jpeg', 'jpg');
    const rel = `src/res/ad-${key}.${ext}`;
    const p = resolve(root, rel);
    mkdirSync(dirname(p), { recursive: true });
    writeFileSync(p, Buffer.from(m[2], 'base64'));
    out.push(rel);
  }
  return out;
}

export function generate(panelPath, outDir) {
  const panel = JSON.parse(readFileSync(panelPath, 'utf8'));
  // meta.id 가 없을 수 있다(저작 모델에서 lift 한 부분 panel). 안전한 폴백으로 디렉터리를 잡는다.
  const root = outDir || resolve(__dir, '../out', panel.meta.id || panel.meta.deviceKey || 'UNNAMED');
  rmSync(root, { recursive: true, force: true });

  // ── preflight: blocker gap 이 있으면 저장소를 만들지 않고 인수인계 문서만 낸다 ──
  // 저작 모델에서 갓 lift 한 부분 panel 은 Tuya DP 번호·meta.id 등이 비어 빌드가 불가능하다.
  // 크래시 대신, "무엇이·왜 막는지" 를 HANDOFF.md 로 남기고 멈춘다.
  const blockers = inferGaps(panel).filter(g => g.severity === 'blocker');
  if (blockers.length) {
    mkdirSync(root, { recursive: true });
    writeFileSync(resolve(root, 'HANDOFF.md'), buildHandoff({ panel, source: 'generate (blocked)', name: panel.meta.name }));
    writeFileSync(resolve(root, 'HANDOFF.html'), buildHandoffHtml({ panel, source: 'generate (blocked)', name: panel.meta.name }));
    return { root, blocked: true, blockers, written: ['HANDOFF.md', 'HANDOFF.html'], panel };
  }

  const written = [];

  // ── 프레임워크 스캐폴드 (고정 템플릿) + 패널 에셋 ──
  written.push(...copyScaffold(root, panel));
  written.push(...copyAssets(root, panelPath));
  written.push(...writeEmbeddedAssets(root, panel));

  // ── 데이터 레이어 ──
  written.push(write(root, 'src/config/dpCodes.ts', E.emitDpCodes(panel)));
  written.push(write(root, 'src/devices/schema.ts', E.emitSchema(panel)));
  written.push(write(root, 'src/config/theme.ts', E.emitTheme(panel)));
  written.push(write(root, 'src/config/links.ts', E.emitLinks(panel)));
  written.push(write(root, 'src/routes.config.ts', E.emitRoutes(panel)));
  written.push(write(root, 'src/global.config.ts', E.emitGlobalConfig(panel)));
  written.push(write(root, 'src/i18n/strings.ts', E.emitStrings(panel)));
  written.push(write(root, 'src/res/index.ts', E.emitRes(panel)));
  written.push(write(root, 'project.tuya.json', E.emitProjectTuya(panel)));

  // ── 고정 템플릿 (seam·glue·빌드) ──
  written.push(write(root, 'src/devices/useDp.ts', T.tplUseDp(panel)));
  written.push(write(root, 'src/devices/index.ts', T.tplDevicesIndex(panel)));
  written.push(write(root, 'src/composeLayout.tsx', T.tplComposeLayout(panel)));
  written.push(write(root, 'src/app.tsx', T.tplApp(panel)));
  written.push(write(root, 'src/i18n/index.ts', T.tplI18nIndex()));
  written.push(write(root, 'src/config/openExternal.ts', T.tplOpenExternal()));
  written.push(write(root, 'ray.config.ts', T.tplRayConfig()));
  written.push(write(root, '.npmrc', T.tplNpmrc()));

  // ── 커스텀 페이지 슬롯 ──
  for (const r of panel.routes) {
    const dir = `src/pages/${r.name}`;
    written.push(write(root, `${dir}/index.config.ts`, pageConfig()));
    written.push(write(root, `${dir}/index.tsx`, pageStub(panel, r)));
  }

  // ── hej 규격 리포트 ──
  const theme = validateTheme(panel);
  const terms = lintTerms(panel.i18n);
  const report = { hejDir: HEJ_INFO.dir, theme, termHits: terms };
  written.push(write(root, 'STUDIO-REPORT.json', JSON.stringify(report, null, 2) + '\n'));

  // ── 개발자 인수인계 문서 ──
  // 저작도구가 만든 것과 개발자가 채울 것을 무엇을·왜·누가 체크리스트로 저장소에 싣는다.
  // git 으로 전달되면 이 문서가 인수인계다 (P3 인수인계 뷰의 문서 산출물).
  written.push(write(root, 'HANDOFF.md', buildHandoff({ panel, source: 'generate', name: panel.meta.name })));
  written.push(write(root, 'HANDOFF.html', buildHandoffHtml({ panel, report, stats: { files: written.length + 1 }, source: 'generate', name: panel.meta.name })));
  written.push(write(root, 'QA.md', buildQaChecklist(panel)));

  return { root, written, report, panel };
}

function pageConfig() {
  return `/* AUTO-GENERATED. */
import { COLOR } from '@/config/theme';
export default {
  backgroundColor: COLOR.bgHome,
  disableScroll: true,
  navigationStyle: 'custom',
};
`;
}

function pageStub(panel, route) {
  const bound = panel.dps
    .filter(d => d.semantic !== 'unrendered')
    .map(d => `//   ${d.camel.padEnd(20)} ${d.type.padEnd(7)} ${d.semantic}`)
    .join('\n');
  return `/* AUTO-GENERATED 골격 — 커스텀 슬롯. 개발자가 위젯을 채운다.
 *
 * ${panel.meta.name} · ${route.name} (${route.route})
 *
 * 이 패널이 노출하는 DP 와 권장 위젯(semantic):
${bound}
 *
 * 데이터 레이어(useDp/setDp)는 이미 생성돼 있다. 여기서 useDpState() 로 읽고
 * setDp()/setDps() 로 쓴다. 표준 위젯 조합은 저작도구가, 수제 비주얼은 개발자가.
 */
import React from 'react';
import { View, Text } from '@ray-js/ray';
import { useDpState, useOnline } from '@/devices/useDp';

export function ${route.name}() {
  const dp = useDpState();
  const online = useOnline();
  return (
    <View>
      <Text>${route.name} — 커스텀 슬롯 (TODO: 위젯 배치)</Text>
    </View>
  );
}

export default ${route.name};
`;
}

// CLI
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [, , panelArg, outArg] = process.argv;
  if (!panelArg) {
    console.error('usage: node src/generate.mjs <panel.json> [outDir]');
    process.exit(1);
  }
  const result = generate(resolve(panelArg), outArg && resolve(outArg));
  if (result.blocked) {
    console.error(`\n✘ 생성 중단 — blocker ${result.blockers.length}건 (빌드 불가한 미완성)`);
    for (const b of result.blockers) console.error(`   🔴 ${b.path} — ${b.reason}`);
    console.error(`\n  인수인계 문서를 남겼다 → ${resolve(result.root, 'HANDOFF.md')}`);
    console.error(`  panel.json 에서 위 항목을 채우고 다시 생성해라.\n`);
    process.exit(1);
  }
  const { root, written, report } = result;
  console.log(`\n✔ 생성 완료 → ${root}`);
  console.log(`  파일 ${written.length}개\n`);
  console.log(`── hej 규격 리포트 ──`);
  console.log(`  테마 모드: ${report.theme.mode}  ${report.theme.ok ? '✔' : '✘'}`);
  report.theme.warnings.forEach(w => console.log(`  ⚠ ${w}`));
  report.theme.errors.forEach(e => console.log(`  ✘ ${e}`));
  console.log(`  용어 검사: ${report.termHits.length}건 (hej 규격 권장 대체어)`);
  const shown = report.termHits.slice(0, 6);
  shown.forEach(h => console.log(`    · [${h.key}] "${h.term}" → "${h.use}"  (${h.value})`));
  if (report.termHits.length > shown.length) console.log(`    · … 외 ${report.termHits.length - shown.length}건`);
  console.log('');
}
