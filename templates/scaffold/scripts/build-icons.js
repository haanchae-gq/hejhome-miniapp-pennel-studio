/**
 * P5 — 아이콘 자산 생성. `assets/icons/*.svg` → `src/res/*.png` + `src/res/index.ts`.
 *
 *   node scripts/build-icons.js
 *
 * **왜 색상별로 PNG 를 미리 굽는가.**
 * 아이콘은 상태에 따라 색이 달라야 한다 — 홈 상단의 모드 아이콘은 accent 이고,
 * 같은 아이콘이 S2 행에서는 흰색이다. 자산 하나로 이를 해결하는 방법은 CSS
 * `mask-image` 나 인라인 `<svg>` 인데, 둘 다 미니앱 WebView 가 렌더하는지
 * **실기기 없이 확인할 수 없다** (P4-2 의 SVG, P4-3 의 CSS 변수와 같은 종류의
 * 미지수다). 지원되지 않으면 아이콘이 통째로 사라진다.
 *
 * `<Image src>` 로 PNG 를 그리는 것은 렌더러 기능에 아무것도 요구하지 않는다.
 * 대가는 자산 수가 늘어나는 것인데, 실제로 두 가지 색이 필요한 아이콘은 모드
 * 4종뿐이라 감당할 만하다. 색이 바뀌면 이 스크립트를 다시 돌린다.
 *
 * 실기기에서 mask-image 가 된다는 것이 P7 에서 확인되면 이 단계는 걷어낼 수 있다.
 */
const fs = require('fs');
const path = require('path');
const { Resvg } = require('@resvg/resvg-js');

const ROOT = path.resolve(__dirname, '..');
const SRC_DIR = path.join(ROOT, 'assets', 'icons');
const OUT_DIR = path.join(ROOT, 'src', 'res');

/**
 * `src/config/theme.ts` 의 값을 그대로 복제한다. 스크립트는 빌드 파이프라인 밖에서
 * 도는 순수 Node 라 TS 를 import 할 수 없다. `test/p5-icons.test.ts` 가 두 곳이
 * 어긋나면 실패시킨다 — P4-3 의 theme.ts ↔ variables.less 와 같은 장치다.
 */
const TINT = {
  accent: '#00A8F0',
  textPrimary: '#FFFFFF',
  textSecondary: '#9A9A9A',
  dockGlyph: '#8A8A8A',
};

/** 자산 1개당 렌더 높이. icon_back 이 72px(@3x of 24) 이므로 그 관례를 따른다. */
const SIZE = 72;

/**
 * 아이콘별로 **실제 쓰이는 색만** 굽는다. 안 쓰는 색을 구우면 죽은 자산이 된다.
 * 각 항목의 주석은 그 색이 어디서 필요한지를 적는다.
 */
const ICONS = [
  // 모드 4종 — 홈 아크 중앙(accent) 와 S2 ToggleRow(흰색) 양쪽에 쓰인다.
  { name: 'bypass', tints: ['accent', 'textPrimary'] },
  { name: 'cook', tints: ['accent', 'textPrimary'] },
  { name: 'clean_ventilation', tints: ['accent', 'textPrimary'] },
  { name: 'clean_rotation', tints: ['accent', 'textPrimary'] },

  // 홈 하단. 전원은 두 색이다 — 켜짐(파란 원 위 흰 글리프)과 대기 화면(어두운 원 위
  // accent 글리프). 대기 화면에서 색이 뒤집히는 것이 곧 '꺼져 있다' 는 신호다.
  { name: 'power', tints: ['textPrimary', 'accent'] },
  { name: 'mode', tints: ['dockGlyph'] },
  { name: 'setting', tints: ['dockGlyph'] },

  // S2 예약 2행.
  { name: 'schedule_on', tints: ['textPrimary'] },
  { name: 'schedule_off', tints: ['textPrimary'] },

  // S3 필터 교체 1행.
  { name: 'filter', tints: ['textPrimary'] },

  // S3 외부 링크 2행 — 필터 교체 행과 같은 색이어야 한 목록으로 읽힌다.
  { name: 'service', tints: ['textPrimary'] },
  { name: 'cart', tints: ['textPrimary'] },

  // S3 하단 서비스 문의. accent 다 — 눌리는 것(전화 연결)임을 색으로 말한다.
  { name: 'phone', tints: ['accent'] },

  // S4 센서 2행.
  { name: 'fine_dust', tints: ['textPrimary'] },
  { name: 'co2', tints: ['textPrimary'] },

  // 어포던스 — 전부 보조색.
  { name: 'help', tints: ['textSecondary'] },
  { name: 'chevron_right', tints: ['textSecondary'] },
  { name: 'chevron_up', tints: ['textSecondary'] },
  { name: 'chevron_down', tints: ['textSecondary'] },
  // NavBar 뒤로가기. 기존 icon_back.png 를 대체한다.
  { name: 'chevron_left', tints: ['textPrimary'] },
];

/** 자산 파일명. 단색이 하나뿐이어도 색을 붙인다 — 나중에 색이 늘어도 이름이 안 바뀐다. */
const assetName = (name, tint) => `icon_${name}_${tint}`;

/**
 * SVG 소스는 검정(`#000000`)으로 그려져 있다. 이를 목표색으로 치환한다.
 * `fill` 과 `stroke` 양쪽에 나타나므로 문자열 전체를 바꾼다.
 */
function recolour(svg, hex) {
  return svg.replace(/#000000/g, hex);
}

function main() {
  if (!fs.existsSync(OUT_DIR)) fs.mkdirSync(OUT_DIR, { recursive: true });

  // 이전 산출물을 먼저 지운다. 아이콘을 목록에서 빼도 PNG 가 남아 있으면
  // res/index.ts 에서는 사라졌는데 파일은 남는 유령 자산이 된다.
  fs.readdirSync(OUT_DIR)
    .filter(f => f.startsWith('icon_') && f.endsWith('.png'))
    .forEach(f => fs.unlinkSync(path.join(OUT_DIR, f)));

  const written = [];

  ICONS.forEach(({ name, tints }) => {
    const svgPath = path.join(SRC_DIR, `${name}.svg`);
    if (!fs.existsSync(svgPath)) throw new Error(`SVG 소스가 없다: ${svgPath}`);
    const svg = fs.readFileSync(svgPath, 'utf8');

    tints.forEach(tint => {
      const hex = TINT[tint];
      if (!hex) throw new Error(`알 수 없는 틴트: ${tint} (${name})`);

      const png = new Resvg(recolour(svg, hex), {
        fitTo: { mode: 'height', value: SIZE },
        // 배경 투명. 아이콘은 어떤 배경 위에도 올라간다.
        background: 'rgba(0,0,0,0)',
      })
        .render()
        .asPng();

      const asset = assetName(name, tint);
      fs.writeFileSync(path.join(OUT_DIR, `${asset}.png`), png);
      written.push(asset);
    });
  });

  const lines = written.map(a => `import ${a} from './${a}.png';`).join('\n');
  const entries = written.map(a => `  ${a},`).join('\n');

  fs.writeFileSync(
    path.join(OUT_DIR, 'index.ts'),
    `/**
 * 아이콘 자산 레지스트리 — **생성 파일이다. 직접 고치지 마라.**
 *
 *   node scripts/build-icons.js
 *
 * 소스는 \`assets/icons/*.svg\`, 색 배정은 \`scripts/build-icons.js\` 의 \`ICONS\` 표에 있다.
 */
${lines}

export default {
${entries}
};
`,
    'utf8'
  );

  console.log(`아이콘 ${written.length}개 생성 → src/res/`);
}

main();
