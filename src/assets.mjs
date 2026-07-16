#!/usr/bin/env node
/**
 * assets.mjs — 이미지·애니메이션 에셋을 WebP 로 변환한다 (광고 배너·히어로용).
 *
 *   node src/assets.mjs <in> <out.webp> [--quality 80]     # 단일 변환
 *   node src/assets.mjs --dir <srcDir> <outDir>            # 폴더 일괄(→ 같은 이름 .webp)
 *   node src/assets.mjs --selftest                         # PNG→WebP 왕복(sharp 동작)
 *
 * 정지 이미지(PNG/JPG)와 **애니메이션 GIF → 애니메이션 WebP** 를 지원한다(sharp+libwebp).
 * MP4 등 영상은 이 경로로 못 한다(ffmpeg 필요, 이 호스트엔 없음) — GIF 로 주거나 프레임을
 * 미리 뽑아 와야 한다. 정지 이미지는 스튜디오가 브라우저에서 이미 WebP 로 만들어 넣으므로,
 * 이 CLI 는 주로 **애니메이션(GIF)** 을 위한 것이다.
 */
import sharp from 'sharp';
import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync } from 'node:fs';
import { resolve, basename, extname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

/** 입력(경로 또는 Buffer) → WebP 파일. 애니메이션이면 애니 WebP 로 보존. */
export async function toWebp(input, outPath, { quality = 80, effort = 4 } = {}) {
  const probe = await sharp(input, { animated: true }).metadata();
  const animated = (probe.pages || 1) > 1;
  // GIF 의 loop 는 0=무한. 일부 GIF 는 65536 으로 보고하는데 webp 는 0–65535 만 받는다
  // → %65536 으로 접어 65536→0(무한)으로 매핑한다.
  const loop = animated ? (probe.loop ?? 0) % 65536 : undefined;
  await sharp(input, { animated })
    .webp({ quality, effort, loop })
    .toFile(outPath);
  return { animated, pages: probe.pages || 1, from: probe.format, out: outPath };
}

/** data URI(스튜디오가 넣은 브라우저 산출 WebP 등) → 파일. 이미 WebP 면 그대로 기록, 아니면 변환. */
export async function dataUriToWebp(dataUri, outPath, opts) {
  const m = /^data:([^;,]+)?(;base64)?,(.*)$/s.exec(dataUri || '');
  if (!m) throw new Error('data URI 형식이 아니다');
  const mime = m[1] || '', buf = m[2] ? Buffer.from(m[3], 'base64') : Buffer.from(decodeURIComponent(m[3]));
  if (mime === 'image/webp') { writeFileSync(outPath, buf); return { animated: false, passthrough: true, out: outPath }; }
  return toWebp(buf, outPath, opts);
}

const IMG_EXT = new Set(['.png', '.jpg', '.jpeg', '.gif', '.webp', '.tiff', '.avif']);

/** srcDir 의 이미지들을 outDir 에 <이름>.webp 로 일괄 변환. */
export async function convertDir(srcDir, outDir, opts) {
  if (!existsSync(srcDir)) return [];
  mkdirSync(outDir, { recursive: true });
  const done = [];
  for (const f of readdirSync(srcDir)) {
    if (!IMG_EXT.has(extname(f).toLowerCase())) continue;
    const out = join(outDir, basename(f, extname(f)) + '.webp');
    const r = await toWebp(resolve(srcDir, f), out, opts);
    done.push({ src: f, ...r });
  }
  return done;
}

async function selftest() {
  // 2×2 빨강 PNG 를 만들어 WebP 로 변환 → 다시 읽어 크기 확인
  const png = await sharp({ create: { width: 2, height: 2, channels: 3, background: { r: 255, g: 0, b: 0 } } }).png().toBuffer();
  const tmp = resolve(fileURLToPath(new URL('.', import.meta.url)), '__selftest.webp');
  const r = await toWebp(png, tmp);
  const meta = await sharp(readFileSync(tmp)).metadata();
  const ok = meta.format === 'webp' && meta.width === 2 && meta.height === 2;
  try { (await import('node:fs')).unlinkSync(tmp); } catch {}
  console.log(`  ${ok ? '✔' : '✘'} PNG → WebP (${meta.format} ${meta.width}×${meta.height})`);
  console.log(`  sharp ${sharp.versions?.sharp} · libwebp ${sharp.versions?.webp}`);
  return ok;
}

// CLI
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const argv = process.argv.slice(2);
  const q = (i => (i >= 0 ? Number(argv[i + 1]) : 80))(argv.indexOf('--quality'));
  (async () => {
    if (argv.includes('--selftest')) { console.log('── assets(sharp) 셀프테스트 ──'); process.exit((await selftest()) ? 0 : 1); }
    if (argv[0] === '--dir') {
      const done = await convertDir(resolve(argv[1]), resolve(argv[2]), { quality: q });
      done.forEach(d => console.log(`  ${d.animated ? '🎞 애니' : '🖼 정지'} ${d.src} → ${basename(d.out)} (${d.pages}f)`));
      console.log(`\n  ✔ ${done.length}개 변환 → ${argv[2]}`);
      process.exit(0);
    }
    const [inp, out] = argv.filter(a => !a.startsWith('--'));
    if (!inp || !out) { console.error('usage: node src/assets.mjs <in> <out.webp> | --dir <src> <out> | --selftest'); process.exit(1); }
    const r = await toWebp(resolve(inp), resolve(out), { quality: q });
    console.log(`  ✔ ${r.animated ? `애니 WebP (${r.pages}프레임)` : '정지 WebP'} → ${out}`);
    process.exit(0);
  })();
}
