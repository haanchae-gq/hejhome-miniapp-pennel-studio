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

export function generate(panelPath, outDir) {
  const panel = JSON.parse(readFileSync(panelPath, 'utf8'));
  const root = outDir || resolve(__dir, '../out', panel.meta.id);
  rmSync(root, { recursive: true, force: true });

  const written = [];

  // ── 프레임워크 스캐폴드 (고정 템플릿) + 패널 에셋 ──
  written.push(...copyScaffold(root, panel));
  written.push(...copyAssets(root, panelPath));

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
  const { root, written, report } = generate(resolve(panelArg), outArg && resolve(outArg));
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
