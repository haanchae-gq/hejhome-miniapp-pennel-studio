# miniapp-panel-studio

Tuya Ray 미니앱 패널 **웹 저작도구**. 개발 지식이 없는 사람도 웹 서비스로 패널
초안을 만들 수 있게 한다.

핵심 아이디어: 패널 하나는 결국 **구조화된 데이터(`panel.json`) + 검증된 코드 한 겹**이다.
사용자는 데이터를 편집하고, 생성기가 그것을 **완전한 Ray 저장소**로 번역한다.
색·간격·서체·용어의 정본은 만들지 않는다 — 헤이홈 디자인 시스템(`design-guide`)의
`dist/` 를 소비하고, 완성물이 그 **규격**을 지키는지 검사한다.

```
panel.json ──▶ [generator] ──▶ 완전한 Ray 저장소 (src/, package.json, project.tuya.json…)
   ▲                │
   │                ├─ 데이터 레이어 (emit.mjs)      : DP·토큰·문구·링크·라우팅
[웹 에디터]          ├─ 고정 템플릿 (templates.mjs)   : seam·i18n·openExternal·빌드설정
 (web/studio.html)   │    └ 실기기 대응 지식이 박혀 있어 사용자가 안 건드린다
                     ├─ 커스텀 슬롯                   : 페이지 골격 (개발자가 위젯 채움)
                     └─ hej 규격 리포트 (hej.mjs)     : 테마 예외 사유 + 용어 검사
```

## 지금까지 (P0 — 왕복 검증)

원본 `teamgoqual/haatz-r6-miniapp-panel` 을 `panel.json` 으로 인코딩하고,
생성기로 되돌린 뒤 **원본과 의미 대조**했다.

```bash
npm run p0
# 생성 → hej 규격 리포트 → 원본과 의미 대조 (199/199 무손실)
```

| 도메인 | 항목 | 결과 |
|---|---|---|
| DP 코드 / enum / value 레인지 | 14 / 4 / 4 | ✔ |
| 테마 COLOR / AQ / OPACITY | 15 / 4 / 3 | ✔ |
| 외부 링크 · 라우팅 · 아이콘 | 2 / 4 / 25 | ✔ |
| i18n ko / en | 62 / 62 | ✔ |

이는 **모델이 원본 패널의 데이터 레이어를 무손실로 담는다**는 증거다.
바이트 비교가 아니다 — 원본의 손으로 쓴 근거 주석은 모델이 아니라 고정 템플릿의
몫이라, 각 도메인의 **데이터**를 양쪽에서 뽑아 대조한다.

## P1.5 — 스튜디오 왕복 증명 + 개발자 인수인계

스튜디오(`web/studio.html`)가 편집하는 것은 panel.json 이 **아니라** 더 단순한 **저작
모델**이다 — 비개발자가 채울 수 있는 것(DP·위젯 바인딩·화면·색·한국어 초안)만 담는다.
정본 panel.json 은 그보다 훨씬 넓다(Tuya DP 번호·영문·아이콘·결함 코드·Tuya 프로젝트
설정…). 그래서 저작 모델 → panel.json 은 **손실적**이다. P1.5 는 그 손실을 **측정하고
선언**한다 — 숨기지 않는다.

```bash
npm run p1     # 저작 seed → lift() → 정본 panel.json 과 deep-diff
```

`src/lift.mjs` 가 파생 가능한 것만 파생한다(camel·semantic·routes·타입·theme 색·i18n.ko).
`src/p1.mjs` 가 결과를 정본과 대조해 세 갈래로 나눈다:

| | 뜻 | haatz 실측 |
|---|---|---|
| ✔ reproduced | 저작 모델이 무손실로 담는 필드 | **171** |
| ○ declared-gap | lift 가 못 만든다고 **사유와 함께** 선언한 것 | **184** |
| ✘ drift | 틀린 파생·유령 필드·미선언 누락 | **0** |

**통과 = drift 0.** 저작 모델은 불완전해도 되지만, 모든 불완전은 **선언**되어야 한다
(design-guide 의 "조용한 면제는 없다"와 같은 규율). P0.5 가 "생성물이 실제로 빌드된다"를
증명했듯, P1.5 는 "스튜디오가 뱉는 것이 검증된 파이프라인으로 이어진다"를 증명한다.

**그 gap 목록이 곧 개발자 인수인계다.** `src/handoff.mjs` 가 gap 을 **무엇을·왜·누가**
체크리스트(`HANDOFF.md`)로 만들어 생성 저장소에 함께 싣는다. git 으로 전달되면 개발자는
흰 화면이 아니라 "남은 6할"의 정확한 목록을 받는다. blocker 15건이 Tuya DP 번호라는 것도
드러난다 — P4(Tuya OpenAPI 직결)가 왜 필요한지를 이 문서가 스스로 가리킨다.

- 저작 모델 export: 스튜디오 상단 **저작 모델 내보내기** → `<deviceKey>.studio.json`
- 그 파일 → `node src/lift.mjs <studio.json> out.panel.json` → `node src/generate.mjs` → Ray 저장소 + `HANDOFF.md`
- 예시 인수인계 문서: 저장소 루트 `HANDOFF.example.md` (`npm run p1` 이 갱신)

## P2 — URL 프레임 프리체크 (A/B/C)

유형2(웹인앱)의 냉정한 제약: 대부분의 외부 사이트가 `X-Frame-Options`(XFO) 또는 CSP
`frame-ancestors` 로 프레임 삽입을 거부한다. `src/precheck.mjs` 가 이 두 헤더로 URL 을
셋 중 하나로 판정한다:

| | 뜻 | 헤더 | 권장 실현 |
|---|---|---|---|
| 🟢 **A** | 임베드 허용 | 제약 없음 / `frame-ancestors *` | embedded (단 Ray 호스트 web-view 능력 확인) |
| 🟡 **B** | 제한적 | `SAMEORIGIN` · `frame-ancestors 'self'`/허용목록 | embedded-selfhosted (허용 오리진서 호스팅). 3rd파티 그대로면 external-browser |
| 🔴 **C** | 임베드 불가 | `DENY` · `frame-ancestors 'none'` | external-browser 뿐 |

**CSP frame-ancestors 가 있으면 XFO 를 이긴다** (브라우저 규칙 그대로).

```bash
npm run precheck <url> ...              # 실측 판정
npm run p2                              # 분류기 셀프테스트(9건, 네트워크 없음)
npm run precheck --panel <panel.json> [--write]   # webContent 일괄 판정 → verdict·reason 기록
```

**verdict(정책) 와 recommend(실현)은 다르다.** 프리체크는 URL 의 **헤더 정책**만 판정한다.
실제 mode 는 두 가지가 더 필요하다: ① Ray 커스텀 렌더 호스트가 `web-view` 를 그리는가,
② 우리가 그 콘텐츠를 허용 오리진에서 호스팅할 수 있는가. 그래서 프리체크는 `verdict`·`reason`
(증거)만 기록하고 `mode` 는 사람이 정한다.

> 실측 예: haatz `service`(haatz.com)는 헤더상 **A** 지만 정본은 external-browser 다 —
> Ray 커스텀 렌더가 `web-view` 를 안 만들기 때문. `filter`(m.haatzmall.com)는 **B**
> (`frame-ancestors 'self' *.haatzmall.com`)지만 3rd파티라 역시 external-browser. 프리체크가
> 이 **근거**를 P1.5 의 `webContent.reason` gap 에 채워 넣는다 — P2 가 P1.5 를 닫는다.

**브라우저에선 프리체크를 못 한다(정직하게).** 타 오리진의 응답 헤더는 CORS 로 가려져
정적 스튜디오(브라우저)에서 읽을 수 없다. 프리체크는 **CLI/서버 전용**이다. 스튜디오는
webContent 링크를 *선언*하고, 판정은 파이프라인(`npm run precheck --panel`)이 붙인다.

## P3 — git export + 개발자 인수인계 뷰

생성물을 개발자에게 넘기는 마지막 한 뼘. 두 가지다.

**개발자 인수인계 뷰 (`HANDOFF.html`).** `HANDOFF.md`(체크리스트)에 더해, 저장소마다
자립형 hej 스타일 HTML 한 장을 함께 낸다 — **무엇이 되어 있나 / 남은 것(owner·severity) /
어떻게 실행 / 규격 리포트**. 열기만 하면 인수인계 상태가 한눈에 보인다. `npm run generate`
가 `HANDOFF.md`·`HANDOFF.html` 을 함께 emit 한다(빌드가 막힌 부분 panel 도 인수인계 문서는 낸다).

**git export (`src/export.mjs`).**

```bash
npm run export <repoDir> [--remote <url>] [--push]
```

`.gitignore` 를 쓰고 `git init`(main) 하고, 인수인계 요약을 커밋 메시지에 담아 한 커밋으로
묶는다. **blocker 가 있으면 커밋 메시지에 경고를 박는다**("빌드 blocker N건 — 채우기 전엔
ray build 안 됨"). push 는 부작용이라 `--push` 를 줄 때만, 원격은 `--remote` 로 지정한다.
커밋 아이덴티티는 저장소 config 를 건드리지 않도록 `-c` 로만 준다(공용 호스트 배려).
빌드 blocker 로 멈춘 인수인계-only 디렉터리(HANDOFF 만 있는)도 export 된다 — "무엇이 막는지"를
개발자가 git 으로 받는다.

## 헤이홈 디자인 시스템 규격

- 신규 패널의 기본 토큰은 `design-guide/dist/tokens.json` 의 **hej 시맨틱**에서 온다.
- 임의 팔레트를 못 만든다. 벗어나려면 `theme.mode: "bespoke-*"` + **사유(reason)** 필수 —
  `design-guide` 의 `$exemptContrast` 원칙과 동형. 사유 없으면 생성 실패.
  (haatz 는 실물 룸컨트롤러 다크 디스플레이 미러링이라 이 예외를 쓴다.)
- UI 문구는 `terminology.json` 규칙으로 검사한다. haatz 재생성 시 `기기→제품`,
  `하시겠습니까→할까요` 등 6건을 규격 권장으로 보고했다 (막지 않고 알린다).

## 두 패널 유형

1. **실기기 + DP 바인딩** — haatz 가 이 유형. DP 스키마 → 위젯 바인딩 → 시뮬레이터.
2. **웹인앱(광고·3rd파티)** — `webContent` 로 모델링. 냉정한 제약이 있다:
   - `external-browser` : 시스템 브라우저로 랜딩. **확실히 됨** (haatz 필터/AS 패턴).
   - `embedded-selfhosted` : 우리가 호스팅해 `frame-ancestors` 를 연 콘텐츠 +
     `web-view` 태그가 렌더되는 호스트에서만. **호스트 능력 탐지 + 경고 게이팅 필요.**
   - 임의 3rd파티 URL 임베드 : 상대가 `X-Frame-Options` 로 막으면 **원천 불가.**
     (Ray 커스텀 렌더 모드는 `web-view` 태그 자체를 안 만들기도 한다 — 실기기 실증.)

## 로드맵

- **P0 ✔** — panel.json 모델 + 생성기 + 원본 왕복 검증 (199/199 무손실)
- **P0.5 ✔** — 생성 저장소가 `ray build -t tuya` 로 **실제 빌드됨** (`npm run build:check`).
  `dist/tuya/` 에 `app.js`·`app.json`·4개 페이지·i18n·devices 산출. 34초.
- **P1 ✔** — 표준 위젯 팔레트 + DP 바인딩 + **시뮬레이터**(기기 없이 동작 확인) + 웹 에디터 UI
  → `web/studio.html` (헤이홈 디자인시스템 규격의 저작도구 UI)
- **P1.5 ✔** — **스튜디오 왕복 증명 + 개발자 인수인계.** 스튜디오 저작 모델이 검증된
  파이프라인에 무손실로 들어가는지 증명하고(`npm run p1`), 저작 모델이 못 채우는 것을
  개발자 TODO 로 넘긴다(`HANDOFF.md`). 아래 "P1.5" 절 참고.
- **P2 ✔** — 웹인앱 빌더 + URL 프레임 프리체크(A/B/C 판정). 프리체크 엔진 `src/precheck.mjs`
  + 스튜디오 링크 인스펙터 빌더(URL·mode·판정 배지). 아래 "P2" 절.
- **P3 ✔** — git export(`src/export.mjs`) + 개발자 인수인계 뷰(`HANDOFF.html`). 아래 "P3" 절.
- **P4** — Tuya OpenAPI 직결(제품에서 DP 자동 로드)

## 못 하는 것 (정직하게)

- 실기기 QA(오프라인·DP 리포트 경쟁·native 모듈 부재)는 자동화 불가.
- Tuya 스토어 심사·서명·productId/appid 발급은 도구 밖.
- FanArc 같은 수제 비주얼은 초안 이후 개발자 몫 — 커스텀 슬롯으로 남긴다.

## 구조

```
panels/haatz-r6.panel.json   레퍼런스 모델 — 정본 panel.json (haatz 를 인코딩)
panels/haatz-r6.studio.json  저작 모델 seed — 스튜디오가 편집하는 단순 표현 (SSOT)
schema/panel.schema.json     panel.json 스키마 (P1 에서 확장)
src/hej.mjs                  design-guide dist/ 소비 + 규격 검사
src/emit.mjs                 데이터 레이어 emitter
src/templates.mjs            고정 템플릿 (실기기 대응 지식)
src/generate.mjs             오케스트레이터 (CLI) — 저장소 + HANDOFF.md emit
src/compare.mjs              원본 의미 대조 (CLI, P0)
src/lift.mjs                 저작 모델 → panel.json 변환 + gap 선언 (CLI)
src/p1.mjs                   왕복 증명 (CLI, P1.5) — lift vs 정본 deep-diff
src/precheck.mjs             URL 프레임 프리체크 A/B/C (CLI, P2) — XFO·CSP frame-ancestors
src/handoff.mjs              gap → 인수인계 문서 HANDOFF.md + HANDOFF.html(뷰) 생성
src/export.mjs               생성 저장소 → git 인수인계 export (CLI, P3)
web/studio.html              저작도구 UI (에디터·시뮬레이터·규격 리포트·export)
HANDOFF.example.md           예시 인수인계 문서 (npm run p1 이 갱신)
out/<id>/                    생성물 (gitignore) — HANDOFF.md 포함
```
