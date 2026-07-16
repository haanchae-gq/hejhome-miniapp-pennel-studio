# 정한님께 — 패널 스튜디오(SaaS) 배포 검토 부탁

임베디드AI엣지팀 **패널 스튜디오**를 서비스로 완성했습니다(백엔드+DB+인증). 팀 Caddy·
Postgres·Authelia 가 정한님 소유(`openglg-config`)라 접근이 안 돼서 여기서 넘깁니다.
남의 관리형 인프라라 임의로 안 건드렸어요.

## 무엇인가 (P0–P8 + S1–S4)
비개발자가 웹에서 Tuya 미니앱 패널을 그리면 **백엔드가 Ray 저장소·WebP 에셋·프레임 판정을
만들어 주는** 서비스. 정적 프론트가 아니라 **백엔드 컨테이너**(정적 web/ + `/api/*` 한 프로세스)다.
- 업로드 → 서버가 sharp·ffmpeg 로 **애니 WebP** 변환
- "Ray 저장소 받기" → 서버가 lift→generate 해서 `.tar.gz`
- 유저별 패널 저장(Postgres), Authelia 로그인, 접속 IP `211.106.187.123` 제한

## 준비해 둔 것 (`~/repos/work/miniapp-panel-studio/deploy/`)
- `Dockerfile` — 백엔드 이미지(node:20-alpine + ffmpeg + sharp). **빌드+스모크테스트 검증 완료**
- `caddy-studio.snippet` — Caddy 블록(IP 제한 + `import authelia` + `reverse_proxy panel-studio:8797`)
- `README.md` — 빌드·기동·env·적용 전 절차

## 정한님이 해줄 것
1. **Postgres** — proxy 네트워크의 `postgres` 에 DB `studio` + 계정 하나. (없으면 백엔드가 파일
   스토어로 폴백하니, 우선 DB 없이 띄워도 동작은 함 — 멀티유저 영속만 DB 필요.)
2. **컨테이너 기동** (repo 루트):
   ```
   docker build -f deploy/Dockerfile -t panel-studio:local .
   docker run -d --name panel-studio --network proxy --restart unless-stopped \
     -e TRUST_FORWARD_AUTH=true \
     -e DATABASE_URL="postgres://<user>:<pw>@postgres:5432/studio" \
     -v panel-studio-data:/app/server/data panel-studio:local
   docker exec caddy wget -qO- http://panel-studio:8797/api/health
   ```
3. **DNS** (서브도메인 안이면) `studio.hejdev6.goqual.com → 115.68.110.91`
4. **Caddy** — `caddy-studio.snippet` 블록 추가 후 `docker exec caddy caddy reload --config /etc/caddy/Caddyfile`

## 인증 (구글 로그인)
- 인증은 **백엔드가 구글 OIDC 로 직접** 합니다(@goqual.com 회사 계정만). **Authelia 안 걸어도 됩니다** —
  Caddy 블록의 `import authelia` 는 꺼둔 상태입니다.
- 구글 클라우드 OAuth 클라이언트는 **우리(임베디드팀)가 만들어** `GOOGLE_CLIENT_ID/SECRET` env 로 넘길게요.
  승인 리디렉션 URI 는 `https://studio.hejdev6.goqual.com/api/auth/callback` 입니다(도메인 확정되면 등록).
- `SESSION_SECRET` 은 꼭 고정값으로(`openssl rand -hex 32`). 안 주면 재시작마다 로그인 풀립니다.

## 확인/결정 부탁
- **서브도메인 vs 경로** (리디렉션 URI 등록에 필요하니 알려 주세요).
- 공유 proxy 네트워크·postgres 에 붙이는 것은 소유자 확인 먼저라 넘깁니다. 관리 주체 정해 주시면 문서 맞추겠습니다.

이미지·스니펫·env 다 맞춰뒀으니 위 절차면 뜹니다. 궁금한 점 알려주세요.
— 임베디드AI엣지팀 (goqual, devteam@goqual.com)
