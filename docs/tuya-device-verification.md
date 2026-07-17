# Tuya 개발툴 / 실기기 최종 확인 가이드

miniapp-panel-studio 가 생성한 Ray 미니앱 패널을 **Tuya 개발자도구(시뮬레이터)** 와
**실기기** 에서 최종 확인하는 절차. 특히 헤드리스(CI)에서 검증 불가한 항목 —
드래그 다이얼 터치, 컬러휠, 램프 슬라이더의 **실제 인터랙션** — 을 사람이 눈으로 확인한다.

> 헤드리스에서 이미 통과한 것: `ray build -t tuya` 컴파일·번들, 위젯↔DP 규격(`p9`),
> 데이터 레이어 무손실(`p0`/`p1`), DragDial 각도→값 기하(수식 단위 검증).
> **여기서 확인할 것: 실기 렌더·터치·DP 송수신.**

---

## 0. 무엇을 왜 확인하나

| 항목 | 헤드리스 검증됨 | 실기 확인 필요 이유 |
|---|---|---|
| DragDial(온도 다이얼) | 각도→값 수식, 갭형 270° | `ty.createSelectorQuery` 좌표계와 `touches[0].clientX/Y` 프레임 일치, `onTouchMove` 발화 |
| HsvColorPicker(컬러휠) | 빌드·번들 | `lamp-color-wheel` hsColor 채도 스케일(0–1000 가정), 실제 색 반영 |
| 밝기/색온도 lamp 슬라이더 | 빌드·번들 | `lamp-bright-slider`/`lamp-temp-slider` 렌더·터치, 값 범위 |
| 각 위젯 DP 바인딩 | 규격(p9) | 실제 DP 송신 → 기기 반영, 리포트 수신 → 화면 갱신 |

---

## 1. 사전 준비

1. **Tuya IoT Platform 계정** 과 대상 **제품(Product)** — 카테고리에 맞는 것
   (조명 `dj`, 온도조절 `wk`, 커튼 `cl`, 소켓 `cz`). 제품의 **PID** 확보.
2. **Tuya MiniApp 개발자도구(IDE)** 설치 — [Tuya 개발자도구 다운로드](https://developer.tuya.com/en/miniapp).
3. `panel.json` 의 `meta.productTuya` 를 **실제 값으로 교체**한다.
   현재 레퍼런스 패널(`panels/*.panel.json`)의 `productTuya.name`/`projectname` 등은
   빌드 통과용 스텁이다. 실기 확인 전 실제 제품 정보로 채운다:
   - `productId`(PID), `uiid`, `ownerType`, `baseversion`, `dependencies`(BaseKit 등)
   - 값은 Tuya IoT Platform 제품 페이지 또는 기존 Ray 프로젝트의 `project.tuya.json` 참고.
4. (선택) `meta.functionalPages` 의 `appid`/`entryCode` — 설정 페이지 사용 시.

---

## 2. 빌드 산출물 만들기

리포지토리 루트에서:

```bash
# 템플릿별 게이트 (생성 → frozen install → ray build)
npm run build:check:light        # 조명(RGBCW) — 컬러휠·밝기·색온도
npm run build:check:thermostat   # 온도조절 — DragDial
npm run build:check:curtain      # 커튼
npm run build:check:plug         # 플러그
# 또는 전부
npm run build:check:templates
```

산출물: `out/<template>/dist/tuya` (Tuya 개발툴이 로드하는 미니앱 번들).
개발 중 실시간 미리보기는 해당 프로젝트에서 `yarn start:tuya` (`ray start -t tuya`).

> ⚠ `npm run generate` 는 `out/<t>` 를 통째로 지운다. 재생성 후에는 `yarn install` 을
> 다시 해야 한다(각 `build:check:*` 스크립트가 그 순서를 포함).

---

## 3. 개발툴(시뮬레이터)에서 로드

1. Tuya 개발자도구 실행 → **프로젝트 열기** → `out/<template>/dist/tuya` (또는 소스
   프로젝트 폴더에서 `ray start -t tuya` 로 뜬 세션에 연결).
2. **가상 디바이스(Virtual Device)** 선택 — 대상 제품(PID)에 맞는 것.
3. 우측 **DP 목록**에서 값을 바꾸고 **Report** 로 리포트를 시뮬레이션한다.
   (화면이 그 값으로 갱신되는지 = 데이터 seam 정상.)

---

## 4. 항목별 체크리스트

### 4.1 DragDial (온도 다이얼) — 온도조절 템플릿
- [ ] 링을 손가락(마우스)으로 **드래그하면 값이 따라 변한다**.
- [ ] **12시 근처에서 값이 튀지 않는다** (갭형 270° — 최소·최대가 맞닿지 않음).
- [ ] **좌하(7:30)=최소, 우하(4:30)=최대**, 바닥 90°는 비어 있음(갭).
- [ ] 썸(thumb)이 호(arc) 위에 정확히 얹혀 이동한다 (썸-호 정렬).
- [ ] 중앙 온도 숫자가 실시간 갱신되고, 놓으면 그 값으로 **DP 송신**된다.
- 실패 시 확인: `ty.createSelectorQuery().boundingClientRect()` 좌표가 터치 `clientX/Y`
  와 같은 프레임인지. 어긋나면 `src/components/DragDial.tsx` 의 `measureCenter`/`apply`
  좌표 보정. 썸-호가 어긋나면 `DIAL_START`/`DIAL_SWEEP`(현재 135°/270°, `angleOffset=45`)
  가 native `mode="angle2"` 기하와 맞는지 재확인.

### 4.2 HsvColorPicker (컬러휠) — 조명 템플릿
- [ ] 컬러휠을 돌리면 **색상(H)·채도(S)** 가 바뀌고 스와치·기기 색이 반영된다.
- [ ] **프리셋 9색** 탭 시 즉시 해당 색으로 설정된다.
- [ ] **명도(V) 슬라이더**(smart-ui)로 밝기가 바뀐다.
- [ ] 값이 `colour_data` raw HSV hex 로 정확히 인코딩되어 송신된다.
- 실패 시 확인: `lamp-color-wheel` 의 `hsColor.s` 범위. 본 구현은 **0–1000** 가정
  (`colour_data` S 를 그대로 전달). 실기에서 채도가 과하거나 약하면 스케일 매핑을
  `src/emit.mjs` `emitHsvColorPickerComponent` 에서 조정.

### 4.3 밝기 / 색온도 lamp 슬라이더 — 조명 템플릿
- [ ] 밝기 슬라이더(`lamp-bright-slider`)가 렌더되고 드래그로 밝기 변경.
- [ ] 색온도 슬라이더(`lamp-temp-slider`)가 warm↔cool 로 변경.
- [ ] 값 범위: 밝기 `min~max`(DP 파생), 색온도 0–1000.

### 4.4 나머지 위젯 공통
- [ ] 전원/토글(Switch), 모드/씬(Button 그룹, active 강조), 커튼(열기·정지·닫기),
      전력 지표(Cell), 게이지/링 등이 각 DP 값에 맞게 표시·조작된다.
- [ ] `ro` DP 는 Report 시 화면만 갱신, `rw` DP 는 조작 시 송신된다.

---

## 5. 실기기 확인

1. 개발툴에서 **미리보기(Preview)** → QR 로 헤이홈/Tuya 앱(체험판)에서 실제 제품에 로드.
2. 실제 기기로 DP 송수신 왕복:
   - 앱에서 조작 → 기기 반응.
   - 기기 물리 조작/센서 → 앱 화면 갱신.
3. **DragDial·컬러휠·슬라이더의 터치 반응 지연/정확도** 를 실제 손가락으로 확인
   (마우스와 다를 수 있음).

---

## 6. 판정 기준

- **Pass**: 4·5의 모든 체크가 통과하고, 조작→기기, 기기→화면 왕복이 정상.
- **Fail**: 위젯 미표시/터치 무반응/DP 미송신/색·값 불일치 중 하나라도 발생.
  → 해당 위젯의 컴포넌트(`src/components/*` 또는 `src/emit.mjs` 의 emit)와 매핑 상수를
  수정하고 §2 부터 재검증.

---

## 부록: 참고 경로

| 대상 | 경로 |
|---|---|
| 드래그 다이얼 | `src/components/DragDial.tsx` (생성됨), 소스: `src/emit.mjs` `emitDragDialComponent` |
| 컬러 피커 | `src/components/HsvColorPicker.tsx`, 소스: `emitHsvColorPickerComponent` |
| 위젯→코드 매핑 | `src/emit.mjs` `emitWidget` / `emitPage` |
| 데이터 seam | `src/templates.mjs` `tplUseDp`(useDpState/setDp) |
| 레퍼런스 패널 | `panels/{light-rgbcw,thermostat,curtain,plug-energy}.panel.json` |
| 빌드 게이트 | `package.json` `build:check:*` |
| 인터랙티브 다이얼 시뮬(참고) | 아티팩트: 갭형 270° 드래그 다이얼(브라우저용, 실기 아님) |
