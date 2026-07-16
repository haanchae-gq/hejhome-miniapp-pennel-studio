# Haatz RoomController R6 — 개발자 인수인계

> 이 문서는 **자동 생성**된다 (miniapp-panel-studio). 저작도구가 패널의 데이터 레이어와
> 표준 위젯 바인딩까지 만들고, 나머지를 아래 체크리스트로 넘긴다. 항목을 닫는 방법은
> panel.json 을 고치고 다시 생성하는 것이다 (`dist/` 를 손대지 않는 design-guide 규칙과 같다).

- 생성 경로: `studio-lift`
- 남은 항목: **39건** (🔴 blocker 16 · 🟡 todo 17 · ⚪ note 6)
- 커스텀 화면 슬롯: **4개** (개발자가 위젯 배치)

## 🔴 먼저 — 이게 없으면 빌드/동작 안 함

- [ ] **`meta.id`** — panel.json 고유 id. 저작 모델엔 없다. generate 의 출력 디렉터리·패키지 이름이라 없으면 빌드 불가.
- [ ] **`meta.productTuya`** — Tuya 제품 등록 정보(productId·baseversion·의존 Kit 버전 등). Tuya IoT 콘솔에서 온다. 저작 모델 바깥.
- [ ] **DP `switch` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `mode` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `air_quality` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `fan_level` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `humidity_value` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `temp_current` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `fine_dust` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `co2` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `on_schedule` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `off_schedule` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `on_schdule_switch` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `off_schedule_switch` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `filter_reset_notice` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.
- [ ] **DP `fault_remind` 의 `id`** — Tuya DP 번호(정수 id). Tuya 제품 DP 스키마에서 온다 — 저작 모델은 code 만 안다.

## 🚧 커스텀 화면 슬롯 — 위젯 배치

생성된 골격(`src/pages/<name>/index.tsx`)에 표준 위젯을 배치한다. 데이터 레이어
(`useDpState`/`setDp`)는 이미 생성돼 있다. 각 화면이 노출하는 DP:

- [ ] **Home** (`/`) — `src/pages/Home/index.tsx`
- [ ] **Mode** (`/mode`) — `src/pages/Mode/index.tsx`
- [ ] **Setting** (`/setting`) — `src/pages/Setting/index.tsx`
- [ ] **AirQuality** (`/airQuality`) — `src/pages/AirQuality/index.tsx`

  바인딩 대상 DP (semantic → 권장 위젯):
  - `switch` (bool) → **power**
  - `mode` (enum) → **mode-select**
  - `air_quality` (enum) → **grade-badge**
  - `fan_level` (enum) → **level-dial**
  - `humidity_value` (value) → **metric**
  - `temp_current` (value) → **metric**
  - `fine_dust` (value) → **sensor-row**
  - `co2` (value) → **sensor-row**
  - `on_schedule` (string) → **schedule-time**
  - `off_schedule` (string) → **schedule-time**
  - `on_schdule_switch` (bool) → **toggle**
  - `off_schedule_switch` (bool) → **toggle**
  - `filter_reset_notice` (enum) → **action**

## 남은 인수인계 항목

### Tuya 제품 스키마 (IoT 콘솔 · DP 정의)

- [ ] 🟡 todo **`meta.functionalPages`** — Tuya functional-pages appid·entryCode. Tuya 콘솔 발급값.  _(P4)_
- [ ] 🟡 todo **DP `on_schedule` 의 `maxlen`** — 문자열 DP 최대 길이. Tuya DP 정의값.  _(P4)_
- [ ] 🟡 todo **DP `off_schedule` 의 `maxlen`** — 문자열 DP 최대 길이. Tuya DP 정의값.  _(P4)_
- [ ] 🟡 todo **DP `fault_remind` 의 `label`** — 결함 비트 라벨 목록(error_1..n). Tuya DP 정의값.  _(P4)_

### 디자인 · 에셋

- [ ] 🟡 todo **테마 색 `bgSheet`** — 보조 색 'bgSheet'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.  _(P1.5)_
- [ ] 🟡 todo **테마 색 `bgSheetAlert`** — 보조 색 'bgSheetAlert'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.  _(P1.5)_
- [ ] 🟡 todo **테마 색 `textOnSheetAlert`** — 보조 색 'textOnSheetAlert'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.  _(P1.5)_
- [ ] 🟡 todo **테마 색 `alertCancel`** — 보조 색 'alertCancel'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.  _(P1.5)_
- [ ] 🟡 todo **테마 색 `toggleOff`** — 보조 색 'toggleOff'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.  _(P1.5)_
- [ ] 🟡 todo **테마 색 `powerOff`** — 보조 색 'powerOff'. 저작 모델의 기본 팔레트에 없다 — 시트·알림·토글 등 세부 표면 색.  _(P1.5)_
- [ ] 🟡 todo **`icons`** — 아이콘 에셋 이름 목록. 저작 모델은 아이콘을 열거하지 않는다 — 디자인 발주/에셋에서 온다.  _(P1.5)_

### 번역

- [ ] 🟡 todo **`en` 로케일** — 영문 로케일 전체. 저작 모델은 한국어만 담는다 — 번역 필요.  _(P1.5)_

### 개발자

- [ ] 🟡 todo **`faults`** — 결함 코드 → 한국어 메시지 매핑. 저작 모델에 없다. 실기기 결함 사양에서 온다.  _(P4)_
- [ ] 🟡 todo **`theme.opacity`** — 상태 투명도(offline·disabled·pressed). 저작 모델 색 팔레트 밖.  _(P1.5)_
- [ ] 🟡 todo **링크 `service` 의 외부 열기 사유** — 링크 'service' 의 외부 열기 사유(프레임 임베드 불가 판정 근거 등). URL 프레임 프리체크(P2)가 채운다.  _(P2)_
- [ ] 🟡 todo **링크 `filter` 의 외부 열기 사유** — 링크 'filter' 의 외부 열기 사유(프레임 임베드 불가 판정 근거 등). URL 프레임 프리체크(P2)가 채운다.  _(P2)_
- [ ] 🟡 todo **한국어 문구 (추가 키)** — 저작 모델이 담은 것보다 많은 한국어 키가 필요하다(확인 다이얼로그·도움말·표 헤더 등). 작성되지 않은 키.  _(P1.5)_
- [ ] ⚪ note **`$schema`** — panel.json 스키마 포인터. 정본 파일에만 붙는 편집기 힌트라 저작 모델엔 없다.  _(P1.5)_
- [ ] ⚪ note **DP `*` 의 `note`** — DP 주석(오타 의도·서버 푸시 담당 등 사람이 남긴 근거). 저작 모델은 자유 주석 필드를 담지 않는다.
- [ ] ⚪ note **`meta.framework`** — 고정값 ray-tuya. lift 가 임의로 박지 않고 명시적으로 남긴다.  _(P1.5)_
- [ ] ⚪ note **`meta.reference`** — 참조 원본 저장소 경로. 신규 패널이면 없을 수 있다.  _(P1.5)_
- [ ] ⚪ note **DP `on_schdule_switch` 의 `camel`** — DP code 'on_schdule_switch' 가 의도적 오타(Tuya DP 정의 일치)라, 자동 camel(onSchduleSwitch)과 정본 교정 식별자가 다르다. 저작 모델은 code 만 담아 교정 정보를 표현 못 한다.  _(P1.5)_
- [ ] ⚪ note **`theme.base`** — 테마 베이스(dark-fixed 등). 저작 모델은 색 값만 담고 베이스 이름은 안 담는다.  _(P1.5)_

---

생성: miniapp-panel-studio · 항목을 닫으려면 `panel.json` 수정 후 `npm run generate`.
용어·색·문구는 헤이홈 디자인 시스템(design-guide) 규격을 따른다.
