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
 (P1+, 미구현)        │    └ 실기기 대응 지식이 박혀 있어 사용자가 안 건드린다
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
- **P1 (진행)** — 표준 위젯 팔레트 + DP 바인딩 + **시뮬레이터**(기기 없이 동작 확인) + 웹 에디터 UI
  → `web/studio.html` (헤이홈 디자인시스템 규격의 저작도구 UI)
- **P2** — 웹인앱 빌더 + URL 프레임 프리체크(A/B/C 판정)
- **P3** — git 푸시 export + 개발자 인수인계 뷰
- **P4** — Tuya OpenAPI 직결(제품에서 DP 자동 로드)

## 못 하는 것 (정직하게)

- 실기기 QA(오프라인·DP 리포트 경쟁·native 모듈 부재)는 자동화 불가.
- Tuya 스토어 심사·서명·productId/appid 발급은 도구 밖.
- FanArc 같은 수제 비주얼은 초안 이후 개발자 몫 — 커스텀 슬롯으로 남긴다.

## 구조

```
panels/haatz-r6.panel.json   레퍼런스 모델 (haatz 를 인코딩)
schema/panel.schema.json     panel.json 스키마 (P1 에서 확장)
src/hej.mjs                  design-guide dist/ 소비 + 규격 검사
src/emit.mjs                 데이터 레이어 emitter
src/templates.mjs            고정 템플릿 (실기기 대응 지식)
src/generate.mjs             오케스트레이터 (CLI)
src/compare.mjs              원본 의미 대조 (CLI)
out/<id>/                    생성물 (gitignore)
```
