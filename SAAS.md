# 패널 스튜디오 — 서비스(SaaS) 로드맵

저작 프론트엔드(P0–P8)를 **실제 운영 서비스**로 완결한다. 핵심 결핍은 두 가지였다:
영속성 0(새로고침하면 날아감), 그리고 **저작 ↔ 파이프라인 단절**(가치사슬이 전부 CLI라
비개발자는 아무 산출물도 못 얻음). 백엔드가 이 둘을 잇는다.

## 아키텍처

```
브라우저 스튜디오 ──fetch /api/*──▶ 백엔드(한 컨테이너: 정적 web/ + API)
                                      │  기존 CLI 모듈 재사용
                                      ├─ /api/assets/convert  (sharp·ffmpeg → WebP)
                                      ├─ /api/generate        (lift→tuya→generate → .tar.gz)
                                      ├─ /api/precheck        (서버측, CORS 없음)
                                      ├─ /api/panels          (저장/불러오기)
                                      └─ /api/me              (Authelia Remote-User)
                                      ▼
                              Postgres(패널 저장) · Authelia(인증) — proxy 스택에 이미 있음
```

## 단계 (여러 턴 순차)

- **S1 — 백엔드 API** ✅(이번 턴) — `server/index.mjs`. 정적 web/ + `/api/{health,me,assets/convert,
  precheck,generate,panels}`. 기존 모듈 재사용. 패널 저장은 파일 스토어(→ S3 에서 Postgres).
  generate 는 .tar.gz 다운로드(node zlib, 의존성 0). 로컬 검증.
- **S2 — 스튜디오 ↔ 백엔드 배선** ✅ — 백엔드 감지(우상단 연결 뱃지) · 업로드 즉시 **서버 변환
  (애니 WebP 포함)** · **"Ray 저장소 받기"**(→.tar.gz) · **localStorage 자동저장**(새로고침에 살아남음,
  "이어서 작업" 카드로 복원) · **라이브 프레임 판정**("지금 판정"). 백엔드 없으면 브라우저/localStorage 폴백.
  배포 Dockerfile 을 정적→백엔드(node+ffmpeg+sharp)로 갱신, 이미지 빌드+스모크테스트 검증.
- **S3 — 영속·멀티유저(DB)** — Postgres 패널 스토어, 유저별 소유. `/api/panels` 를 DB 로.
- **S4 — 인증** — Caddy `forward_auth`(Authelia) → `Remote-User` 로 유저 식별·권한.

## 배포

백엔드도 정적 스튜디오처럼 proxy 네트워크 컨테이너로 두고 Caddy 가 프록시한다.
`deploy/` 의 Dockerfile·스니펫을 S2 이후 백엔드 버전으로 갱신해 junghan 에게 인계.
(공유 인프라·postgres·Authelia 는 junghan 소유라 붙이는 것은 인수인계.)
