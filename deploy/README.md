# 패널 스튜디오 배포 — Cloudflare 터널 뒤 컨테이너

패널 스튜디오(정적 `web/` + `/api/*` 백엔드)를 `proxy` 도커 네트워크 컨테이너로 두고,
**Cloudflare 터널**이 바깥에서 들여보낸다. 호스트 포트를 열지 않으므로 터널이 유일한 입구다.

```
브라우저 ──HTTPS──▶ Cloudflare (Access 인증)
                        │  panel-studio.goqual-internal.com
                        ▼
                   cloudflared  (proxy 네트워크, TUNNEL_TOKEN)
                        │  http://panel-studio:8797
                        ▼
                   panel-studio (proxy 네트워크, 호스트 포트 없음)
                        ├ 정적 web/studio.html
                        ├ /api/*  (generate·assets·precheck·build·kb)
                        └ postgres (같은 네트워크) — 패널 저장
```

- **접속**: <https://panel-studio.goqual-internal.com> — Cloudflare Access 가 먼저 인증을 요구하고,
  통과하면 스튜디오가 자체 **구글 로그인(@goqual.com)** 을 한 번 더 건다.
- **Caddy 는 이 경로에 없다.** `caddy` 컨테이너가 같은 `proxy` 네트워크에 있지만 `hejdev6.goqual.com`
  등 다른 서비스용이고, Caddyfile 에 `panel-studio` 항목이 없다. 예전 문서가 Caddy + IP 제한
  (`studio.hejdev6.goqual.com`)을 전제했는데 **그 구성은 더 이상 쓰지 않는다.**
  `caddy-studio.snippet` 은 그 시절 잔재라 참고만 해라.
- **터널 ingress 는 Cloudflare 대시보드가 관리한다**(`cloudflared` 는 `TUNNEL_TOKEN` 만 받고
  설정 파일 마운트가 없다). 호스트에서는 라우팅을 못 고친다 — 도메인·경로 변경은 대시보드에서.

## 재배포 (일상 작업)

소스를 고친 뒤에는 **스크립트 한 줄**이면 된다.

```bash
bash deploy/redeploy.sh
```

env 캡처 → 롤백 태그 → 빌드 → 컨테이너 교체 → 헬스체크를 순서대로 하고, 끝에 롤백 명령을 출력한다.
자세한 것은 [`redeploy.sh`](redeploy.sh) 주석 참고.

> ### ⚠️ `docker restart` 로는 새 코드가 안 올라간다
> 컨테이너는 만들어질 때의 **이미지 ID** 에 묶인다. `docker build` 로 `panel-studio:local`
> 태그를 새로 굽어도 기존 컨테이너는 옛 이미지 그대로 돈다. **반드시 `docker rm -f` 후
> `docker run`** 이다(스크립트가 그렇게 한다). 예전 문서의
> `docker build … && docker restart panel-studio` 는 **아무것도 배포하지 않는다.**

### 롤백

`redeploy.sh` 가 교체 전 이미지를 `panel-studio:pre-<YYYYMMDD-HHMM>` 으로 태그해 둔다.

```bash
docker images panel-studio                      # 롤백 후보 확인
docker rm -f panel-studio
docker run -d --name panel-studio --network proxy --restart unless-stopped \
  --env-file ~/.panel-studio.env \
  -v /disk-A/docker-data/panel-studio/data:/app/server/data \
  panel-studio:pre-20260723-1736
```

롤백 태그는 쌓이므로 안정되면 오래된 것부터 지운다(`docker rmi panel-studio:pre-…`).
레이어를 현재 이미지와 공유하므로 실제 회수량은 태그당 수 MB 수준이다.

## 최초 기동 (컨테이너가 아예 없을 때)

```bash
docker build -f deploy/Dockerfile -t panel-studio:local .

docker run -d --name panel-studio --network proxy --restart unless-stopped \
  -e GOOGLE_CLIENT_ID="<구글 OAuth 클라이언트 ID>" \
  -e GOOGLE_CLIENT_SECRET="<구글 OAuth 시크릿>" \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  -e OIDC_REDIRECT_URI="https://panel-studio.goqual-internal.com/api/auth/callback" \
  -e DATABASE_URL="postgres://<user>:<pw>@postgres:5432/studio" \
  -e TZ="Asia/Seoul" \
  -v /disk-A/docker-data/panel-studio/data:/app/server/data \
  panel-studio:local

# 확인 — 호스트 포트가 없으므로 컨테이너 IP 로 직접
curl -sS "http://$(docker inspect panel-studio \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):8797/api/health"
```

**그 다음 Cloudflare 대시보드**에서 터널 ingress 에
`panel-studio.goqual-internal.com → http://panel-studio:8797` 을 추가하고, Access 정책을 건다.

### env

| 키 | 뜻 |
|---|---|
| `GOOGLE_CLIENT_ID`·`GOOGLE_CLIENT_SECRET` | 구글 OAuth 2.0 웹 클라이언트(아래 설정). 있으면 **@goqual.com 계정만 로그인**(서버가 이메일·`hd` 클레임 검증)하고 유저별로 패널이 격리된다. 없으면 로그인 없이 anonymous 단일 버킷(로컬 개발용). |
| `SESSION_SECRET` | 세션 쿠키 HMAC 키. **꼭 고정** — 안 주면 재시작마다 로그인이 풀린다. `openssl rand -hex 32`. |
| `OIDC_REDIRECT_URI` | 리다이렉트 URI. 안 주면 `X-Forwarded-*` 로 유도하지만, 터널 뒤에서는 **명시하는 편이 안전**하다. |
| `DATABASE_URL` | 같은 네트워크의 `postgres`. 없으면 파일 스토어로 떨어진다 — 운영에서는 반드시 준다. |
| `ALLOWED_DOMAIN` | 허용 이메일 도메인. **현재 미설정 = 기본값 `goqual.com`**. |
| `TZ` | `Asia/Seoul`. |
| `PORT`·`PANEL_BUILD_DIR` | 이미지가 굽는 값(8797 / `/opt/panel-build`). 손대지 않는다. |

> `--env-file` 로 되먹일 때 `PATH`·`NODE_*`·`YARN_*` 이 섞이면 컨테이너 `PATH` 를 덮어써 깨진다.
> `redeploy.sh` 가 걸러낸다.

### 데이터

`/disk-A/docker-data/panel-studio/data` 를 `/app/server/data` 로 bind mount 한다.
`DATABASE_URL` 이 있으면 패널 본체는 Postgres 에 살고 이 디렉터리는 부수 파일용이다.

## 구글 로그인 설정 (Google Cloud Console)

1. **OAuth 동의 화면** — Workspace 조직이면 User type = **Internal**. Scope 는 `openid`·`email`·`profile`.
2. **사용자 인증 정보 → OAuth 2.0 클라이언트 ID → 웹 애플리케이션**
   - 승인된 리디렉션 URI: `https://panel-studio.goqual-internal.com/api/auth/callback`
   - 승인된 JavaScript 원본: `https://panel-studio.goqual-internal.com`
3. 발급된 클라이언트 ID·시크릿을 위 env 로 넣는다.

## 제거

```bash
docker rm -f panel-studio
# + Cloudflare 대시보드에서 터널 ingress·Access 정책·DNS 삭제
# + 데이터가 필요 없으면 /disk-A/docker-data/panel-studio/data 정리
```

## 공용 호스트 주의

`panel-studio` 는 공용 `proxy` 네트워크에 붙어 있고 여러 사람이 쓴다.

- 재배포는 **짧게라도 끊긴다.** 남이 작업 중일 수 있으니 필요하면 알리고 한다.
- **컨테이너를 지우기 전에 env 를 먼저 뽑아라.** `docker run` 때 준 시크릿은 컨테이너를 지우면
  되찾을 수 없고, 비운 채 다시 띄우면 로그인 세션과 DB 연결이 통째로 날아간다.
  `redeploy.sh` 는 env 가 5줄 미만이면 컨테이너를 건드리기 전에 멈춘다.
- 같은 이미지로 뜬 컨테이너가 여럿이면 **어느 것이 터널에 물려 있는지** 먼저 가려라.
  `proxy` 네트워크에 없는 컨테이너는 `cloudflared` 가 이름으로 찾지 못한다.
  (2026-07-23: 기본 bridge 에만 있던 `ps-studio` 를 그렇게 판별해 정리했다.)
