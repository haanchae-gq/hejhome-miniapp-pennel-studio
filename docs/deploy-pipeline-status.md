# 배포 파이프라인 현황 · 미결 (miniapp-panel-studio)

> 작성: 2026-07-23 · 임베디드AI엣지팀 · 실측 근거는 §부록.
> 목적: "Tuya 개발자도구(IDE)를 안 거치고 **빌드 → 배포**까지 자동화가 되는가"에 대한
> 현재 상태와 막힌 지점, 그리고 **Tuya 문의용 정리 메모**(§4)를 한곳에 남긴다.

---

## 0. 한 줄 결론

- **빌드(컴파일 → dist 번들)**: 이미 **IDE 없이** 된다. studio `/api/build` 가 헤드리스로 `ray build -t tuya` 를 돌려 `dist.tar.gz` 까지 뽑는다. ✅
- **배포(패널 버전 업로드 → PID 바인딩 → 제출/릴리스 → 실기기 렌더)**: **아직 안 된다.** `ray` CLI 에 발행 커맨드가 없고, **Cube API 는 이 일을 하는 물건이 아니다**(§3). → Tuya 확인이 선결(§4). ⛔

---

## 1. 파이프라인 단계별 현황

| 단계 | 하는 일 | IDE-free? | 현 상태 |
|---|---|---|---|
| 저작 | 기획자가 웹에서 패널 그림 (DP·화면·위젯) | ✅ | studio 웹 (`panel-studio.goqual-internal.com`) |
| 생성 | 저작 모델 → Ray 저장소 (lift→generate) | ✅ | `/api/generate`, `src/generate.mjs` |
| 에셋 | 업로드 이미지 → 애니 WebP (sharp·ffmpeg) | ✅ | `/api/assets/convert` |
| **빌드** | Ray 저장소 → `dist/tuya` 번들 (`ray build -t tuya`) | ✅ | `/api/build` (프리베이크 node_modules, 패널당 ~1.8s) |
| **업로드** | 번들을 Tuya 에 올려 **패널 버전** 생성 | ⛔ | **경로 없음** — `ray` CLI 미지원, Cube API 밖 |
| **바인딩** | 패널 버전 ↔ 제품(PID) 연결 | ⛔ | IDE / Tuya IoT 개발 플랫폼 소관 |
| **릴리스** | 제출·심사·발행 → 실기기 앱에 렌더 | ⛔ | 〃 |

빌드까지는 파이프라인이 닫혀 있다. **업로드~릴리스 3단계가 통째로 미결**이다.

---

## 2. 무엇이 막고 있나 — `ray` CLI 의 한계

`ray` CLI(`@ray-js/cli` **1.5.47**)가 등록하는 커맨드는 **`build`·`start` 둘뿐**이다(실측 §부록). `upload`/`publish`/`login`/`deploy` 가 **없다.** 즉 이 CLI 는 순수 로컬 빌드·개발 도구다.

패널의 **업로드·버전·발행**을 실제로 하는 것은 **Tuya MiniApp 개발자도구(IDE)** 이고, IDE 는 내부적으로 Tuya 의 *miniapp open-service 업로드 API* 를 감싸고 있다. WeChat 진영의 `miniprogram-ci`(헤드리스 업로더)에 해당하는 공개 도구가 Tuya Ray 쪽에 있는지가 핵심 질문이다(§4-ⓐ).

---

## 3. Cube API 로는 안 되는 이유 (배포 관점)

**Cube = Tuya Cube 5.0(프라이빗 클라우드)**. 우리 것은 우리 AWS 계정에 Tuya 가 설치했고 정문은 `cube.goqual.com` 이다. 이건 **런타임 IoT 클라우드**지, 미니앱 패널 **프론트를 발행하는 개발 플랫폼이 아니다.**

- **Cube OpenAPI**(highway 채널 = `openapi-cube`)가 다루는 것: 기기 제어·유저/홈/씬·OTA·MQTT 등 **런타임 IoT 오퍼레이션**. (KB `13-cube-api/api-gateway-1..3`)
- **"Cube App > Build"** 문서: **호스트 모바일 앱(APK/IPA) 빌드**지 패널이 아니다. (KB `14-cube-api-ko/cube-app`)
- 우리 배포본 실측(KB `13-cube-api/our-deployment`): `prod` 네임스페이스 **122개 deployment** 중 **Fusion 계열만 커스텀으로 확정**, 나머지는 미분류. **패널 발행 엔드포인트가 있다는 증거는 없다.**

정리하면 — **문서화된 범위 안에서 Cube API 에 패널 배포 엔드포인트는 없다.** "된다"가 아니라 **"모른다, Tuya 에 표준 diff 를 요청해 확인해야 한다"** 가 정직한 상태다.

---

## 4. Tuya 문의용 정리 메모 (그대로 보내도 되게)

> **제목**: HejHome 미니앱 패널 — 헤드리스(IDE 없이) 업로드·발행 API 가능 여부 문의
>
> **배경**: 우리(HejHome/Goqual, 임베디드AI엣지팀)는 사내 웹 저작도구(panel-studio)에서
> Tuya Ray 미니앱 패널을 생성·빌드(`ray build -t tuya`)까지 **CI에서 헤드리스로** 처리한다.
> 남은 것은 **업로드 → 제품(PID) 바인딩 → 버전 발행** 단계이며, 현재는 Tuya MiniApp
> 개발자도구(IDE)를 수동으로 거쳐야 한다. 이 단계를 API/CLI 로 자동화하려 한다.
> 우리는 **자체 Cube 5.x 프라이빗 클라우드**(`cube.goqual.com`, 우리 AWS)를 운영 중이다.
>
> **질문**
> - **ⓐ 헤드리스 업로드·발행 API/CLI**: IDE 가 내부적으로 호출하는 패널 **업로드·버전
>   생성·발행** API 를, 우리 계정(OEM/프라이빗 클라우드 딜)에 공식 제공할 수 있는가?
>   `miniprogram-ci` 같은 CI 업로더에 해당하는 것이 Tuya Ray 에 있는가? 있으면 문서·인증
>   방식·레이트리밋을 알려달라.
> - **ⓑ 인증 체계 정합**: 그 업로드 API 의 인증·서명이 우리 **Cube OpenAPI**(`openapi-cube`,
>   highway 채널)의 서명 체계와 같은가, 아니면 별도 개발자-플랫폼 자격증명이 필요한가?
> - **ⓒ Cube 소관 여부**: 미니앱 패널의 호스팅/배포가 우리 프라이빗 **Cube** 쪽 서비스로
>   처리되는가, 아니면 Tuya Global(public) 개발 플랫폼에만 존재하는가? (우리 Cube 배포본
>   122개 서비스 중 패널 발행 관련 엔드포인트가 있는지 = **표준 Cube 5.x 서비스/엔드포인트
>   목록을 주면 우리 배포본과 diff 를 떠서 확정하겠다.**)
> - **ⓓ 제출·심사 흐름**: 발행에 Tuya 측 심사(review)가 개입하는가? 자동 릴리스가 가능한
>   범위(개발용 preview / 정식 릴리스)를 구분해 알려달라.
>
> **첨부/참고**: 대상 제품 카테고리(조명 dj·온도조절 wk·커튼 cl·소켓 cz·환풍기 등),
> 샘플 `dist/tuya` 번들, 우리 Cube 정문 채널(atop `a1~a6` / fusion `px1-hybrid` / highway `openapi`).

---

## 5. 미결 항목 (Open Items)

- [ ] **ⓐ Tuya 문의 발송** — §4 메모로 헤드리스 업로드·발행 API 가능 여부 확인 (담당: TBD)
- [ ] **표준 Cube 5.x 서비스/엔드포인트 목록 확보** — 우리 배포본과 diff → 패널 발행 경로 유무 확정 (KB `13-cube-api/our-deployment` §3 한계 해소)
- [ ] **업로드 API 확인 시**: studio 에 `/api/publish` 추가 설계 (`/api/build` 뒤에 연결, PID·버전·심사 상태 반영)
- [ ] **인증 배선** — 업로드 API 자격증명 보관(서버 env, fail-safe), Cube OpenAPI 서명과 공유 가능하면 재사용
- [ ] **대안 검토** — 업로드 API 가 안 열리면: (i) IDE 반자동(스크립트가 번들만 준비, 사람이 IDE 로 업로드) 절차 표준화, (ii) Tuya 측 CI 연동 상품 여부 타진

---

## 부록 A. 실측 근거

```
# ray CLI 커맨드 — build/start 만 등록 (upload/publish/login 없음)
@ray-js/cli@1.5.47  bin: { ray: ./bin/ray }
등록 커맨드: build, start        # 소스 grep 실측 (2026-07-23)

# 헤드리스 빌드 — studio 백엔드가 실제로 실행하는 것
server/index.mjs:244  spawn(.../ray, ['build','-t','tuya'], {cwd, env:{CI:'1'}})
→ dist/tuya 번들 → dist.tar.gz (TTL 10분 아티팩트)
```

## 부록 B. 참고 경로

| 대상 | 위치 |
|---|---|
| 헤드리스 빌드 엔드포인트 | `server/index.mjs` `/api/build` (§195~) |
| 빌드 게이트(로컬) | `package.json` `build:check:*` |
| Tuya 실기기 확인 가이드 | `docs/tuya-device-verification.md` |
| Cube OpenAPI(런타임 IoT) | KB `13-cube-api/api-gateway-1..3` (`openapi-cube`) |
| Cube App 빌드(호스트 앱) | KB `14-cube-api-ko/cube-app` |
| 우리 Cube 배포본 실체 | KB `13-cube-api/our-deployment` (`cube.goqual.com`) |

> ⚠️ 이 문서의 SSOT 는 이 repo(`docs/deploy-pipeline-status.md`)다. Confluence 로 옮길 때는
> 붙여넣기 사본이며, 갱신은 여기서 하고 Confluence 는 링크 또는 재붙여넣기로 맞춘다.
