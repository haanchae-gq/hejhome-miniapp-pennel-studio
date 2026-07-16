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

## P4 — Tuya OpenAPI 직결 (P1.5 blocker 를 닫는다)

P1.5 가 남긴 blocker 의 대부분은 **Tuya 제품 DP 스키마**에서 온다 — DP 번호(`dps.*.id`),
문자열 길이(`maxlen`), 결함 라벨(`label`), 제품 등록 정보(`meta.productTuya`). 저작 모델
바깥의 값이라 사람이 못 채운다. `src/tuya.mjs` 가 그 스키마를 받아 panel 에 병합해 닫는다.

```bash
node src/tuya.mjs --fixture <spec.json> --panel <panel.json> [--write]   # 오프라인(저장된 스펙)
node src/tuya.mjs --product <productId>  --panel <panel.json> [--write]   # 라이브(env 크리덴셜)
npm run p4                                                                # 매핑 셀프테스트(6건)
```

라이브는 env `TUYA_ACCESS_ID`·`TUYA_ACCESS_SECRET`·`TUYA_ENDPOINT`(기본 openapi.tuyaus.com).
서명은 Tuya Cloud v2(HMAC-SHA256). Tuya 타입 → panel 타입 매핑: `bool`→bool · `enum`→enum(range)
· `value`→value(min/max/scale/step/unit) · `string`→string(maxlen) · `bitmap`→fault(label).
**있는 값은 덮지 않는다**(저작자가 정한 것을 존중) — id 같은 빈 칸만 채운다.

**전체 체인이 닫힌다 (검증됨):**

```
studio 저작모델 ──lift──▶ 부분 panel.json      (blocker 16: DP id 14 + meta.id + productTuya)
                            │ tuya --fixture --write
                            ▼
                    DP id·productTuya 채움      (blocker 16 → 1: meta.id 만)
                            │ meta.id 부여(개발자 슬러그)
                            ▼
                    generate ✔ → Ray 저장소 + HANDOFF
```

P4 가 15/16 을 닫고, 남는 하나(`meta.id`)는 Tuya 가 줄 수 없는 개발자 슬러그다 —
HANDOFF.md 가 예측한 그대로다.

## P5 — 실기기 QA 체크리스트

실기기 QA 는 자동화할 수 없다(오프라인·DP 리포트 경쟁·native 모듈 부재). 하지만
**무엇을 확인해야 하는지**는 패널에서 파생할 수 있다. `src/qa.mjs` 가 그 파생 체크리스트를
`QA.md` 로 낸다 — 테스터가 실기기 앞에서 그대로 따라가는 스크립트.

```bash
node src/qa.mjs <panel.json>    # QA.md 출력
npm run p5                      # 커버리지 셀프테스트(rw DP·webContent 누락 없나)
```

DP 제어(semantic 별 문항)·센서 표시·오프라인/리포트 경쟁·외부 링크(mode 별)·결함(서버 푸시)·
테마 미러·접근성/터치 를 덮는다. `npm run generate` 가 저장소에 `QA.md` 를 함께 낸다.
"자동화 불가"를 **"생성 가능한 체크리스트"** 로 바꾼 것 — 커버리지는 셀프테스트가 지킨다.

## P6 — 신규 제품 저작 (일반성 증명)

파이프라인이 haatz 에 과적합되지 않았음을 **두 번째 제품(스마트 플러그)** 으로 증명한다.
haatz 과 전혀 다른 DP(전력 센서·정전 후 복구)·테마(hej 시맨틱, 비-bespoke)·화면 구성을
저작 → lift → Tuya 적용 → generate 까지 태워, **blocker 0 으로 실제 Ray 저장소**가 나오는지 본다.

```bash
npm run p6      # plug-mini 를 전 파이프라인에 태워 blocker 0·HANDOFF·QA 확인
```

- 픽스처: `panels/plug-mini.studio.json`(저작 모델) · `panels/plug-mini.tuya.json`(DP 스펙).
- **P6 가 실제로 haatz 과적합 버그 2건을 잡았다:** `emit.mjs` 가 `theme.opacity`·
  `meta.functionalPages` 존재를 가정했는데, 공기질 없는 플러그엔 없다. 없으면 빈 슬롯으로
  관대하게 처리하도록 고쳤다(둘 다 blocker 아닌 gap). 두 번째 제품이 없었으면 안 드러났다.
- 같은 계기로 lift 의 `scale`·`step` 관례 주입도 제거했다 — 제품마다 다르고(전력 scale 1,
  kWh scale 3) Tuya DP 정의값이라, 관례로 0 을 박으면 플러그에서 틀린다. 이제 gap 으로 선언하고
  Tuya 가 채운다. (haatz 왕복 p1 은 여전히 drift 0.)

P1.5 가 haatz 왕복을 증명했다면, P6 는 **그 파이프라인이 임의 제품에 적용된다**를 증명한다.

## P7 — 스튜디오 ↔ CLI 왕복 (판정 재수입)

프리체크는 브라우저에서 못 돈다(CORS). 그래서 판정은 CLI 가 하고, 그 결과를 스튜디오로
**되가져온다**. 왕복은 세 손을 거친다:

```
스튜디오 [저작 모델 내보내기] ─▶ x.studio.json
        node src/precheck.mjs --studio x.studio.json --write   (links 에 verdict·reason 기록)
스튜디오 [가져오기] ◀─ x.studio.json                            (링크 인스펙터에 프레임 판정 배지)
```

- `precheck --studio <file> [--write]` — 저작 모델의 `links[].url` 을 판정해 `links[].verdict`·
  `reason` 을 되쓴다. (`--panel` 은 panel.json 의 webContent, `--studio` 는 저작 모델의 links.)
- 스튜디오 상단 **가져오기** — `.studio.json` 을 FileReader 로 읽어 새 프로젝트로 연다.
  verdict 가 든 파일이면 카드 뱃지와 링크 인스펙터 배지에 판정이 뜬다.
- `npm run p7` — `precheckStudio` 의 링크→verdict 매핑을 **mock fetch** 로 오프라인 검증
  (분류기 자체는 p2 가 검증). 브라우저·네트워크가 낀 왕복의 접점만 게이트한다.

## P8 — 광고 에셋 WebP (배너·히어로)

광고 배너(`adBanner`)·히어로(`adHero`)에 이미지/애니메이션을 올려 **WebP** 로 패널에 넣는다.
WebP 특성상 경로가 둘로 갈린다:

| 소스 | 어디서 변환 | 결과 |
|---|---|---|
| 정지 이미지(PNG·JPG), 영상 첫 프레임 | **브라우저**(Canvas, 의존성 0) | 정지 WebP — 스튜디오가 모델에 data URI 로 임베드 |
| **애니메이션 GIF** | **CLI `src/assets.mjs`**(sharp+libwebp) | **애니 WebP**(프레임 보존) |
| **영상(MP4·MOV·WebM 등)** | **CLI `src/assets.mjs`**(ffmpeg+libwebp) | **애니 WebP** |

- **스튜디오**: 링크 위젯 인스펙터 "배너 이미지(WebP)" 업로드 → Canvas 로 WebP 변환(정지, ≤640px
  다운스케일) → 프리뷰(adBanner/adHero)에 즉시 표시 → export 에 함께 실림.
- **파이프라인**: `generate` 가 임베드된 WebP data URI 를 `src/res/ad-<key>.webp` 로 뽑는다(이미
  WebP 라 동기, sharp 불필요). 애니는 `npm run assets` 로 별도 변환.

```bash
npm run assets <in> <out.webp>          # 단일 (정지·애니 GIF·영상)
npm run assets --dir <src> <out>        # 폴더 일괄
npm run p8                              # sharp 셀프테스트(PNG→WebP)
```

> **브라우저는 *움직이는* WebP 를 못 만든다**(네이티브 인코더 없음). 그래서 정지는 브라우저,
> 애니는 CLI 로 나눴다. GIF 는 `sharp`(npm 의존성, root 불필요), 영상은 `ffmpeg`(시스템 설치)로.
> 실측: 44프레임 GIF 1MB → 애니 WebP 244KB · 2초 MP4 → 30프레임 애니 WebP.
> 스튜디오는 영상 업로드 시 첫 프레임만 정지 WebP 로 임베드하고, 움직이는 결과는 CLI 로 만든다.

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
- **P4 ✔** — Tuya OpenAPI 직결(`src/tuya.mjs`) — 제품 DP 스키마로 P1.5 blocker 를 닫는다. 아래 "P4" 절.
- **P5 ✔** — 실기기 QA 체크리스트 생성(`src/qa.mjs`) — 자동화 못 하는 QA 를 파생 체크리스트로. 아래 "P5" 절.
- **P6 ✔** — 신규 제품(비-haatz) 저작으로 파이프라인 일반성 증명. 아래 "P6" 절.
- **P7 ✔** — 스튜디오↔CLI 왕복(판정·DP 재수입). 아래 "P7" 절.
- **P8 ✔** — 광고 배너·히어로 이미지 업로드 → WebP(`src/assets.mjs`). 정지는 브라우저, 애니 GIF 는 sharp. 아래 "P8" 절.

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
src/tuya.mjs                 Tuya OpenAPI DP 스키마 → panel 병합 (CLI, P4)
src/qa.mjs                   패널 → 실기기 QA 체크리스트 QA.md (CLI, P5)
src/p6.mjs                   신규 제품 일반성 증명 (CLI, P6) — plug-mini 전 파이프라인
src/p7.mjs                   스튜디오↔CLI 왕복 게이트 (CLI, P7) — precheckStudio mock 검증
src/assets.mjs               이미지·애니 GIF → WebP (CLI, P8) — sharp+libwebp
panels/plug-mini.*.json      두 번째 제품(스마트 플러그) 저작 모델·Tuya 스펙 픽스처
panels/haatz-r6.tuya.json    Tuya DP 스펙 픽스처 (오프라인 P4 검증·데모)
web/studio.html              저작도구 UI (에디터·시뮬레이터·규격 리포트·export)
HANDOFF.example.md           예시 인수인계 문서 (npm run p1 이 갱신)
out/<id>/                    생성물 (gitignore) — HANDOFF.md 포함
```
