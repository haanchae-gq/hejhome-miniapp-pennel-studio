# 패널 스튜디오 배포 — 팀 Caddy(hejdev6) 뒤에 IP 제한 HTTPS 로

패널 스튜디오 UI(`web/`)를 팀 공용 Caddy 뒤에 두고, **특정 IP(211.106.187.123)** 에서만
HTTPS 로 접속하게 한다. 호스트 포트를 열지 않고 `proxy` 도커 네트워크 안에서만 노출하므로
Caddy 가 유일한 입구다(IP 제한 우회 불가). **호스트 `ufw` 는 건드리지 않는다** — 공용 호스트라
전역 방화벽 변경은 위험(순서 틀리면 SSH 2228 포함 전원 차단)하고, IP 제한은 Caddy 로 충분하다.

## 구성

```
클라이언트(211.106.187.123) ──HTTPS──▶ 팀 Caddy(:443, proxy 네트워크)
                                          │  remote_ip 아니면 403
                                          ▼
                                   panel-studio:80  (proxy 네트워크 컨테이너, 호스트 포트 없음)
                                          └ web/studio.html · index.html
```

## 적용 (junghan — docker + Caddy config 소유자)

**1) 스튜디오 이미지 빌드 + 컨테이너 기동** (repo 루트에서)

```bash
docker build -f deploy/Dockerfile -t panel-studio:local .
# 백엔드(정적 web/ + /api/*). 패널 파일 스토어는 볼륨으로 영속화(S3 에서 Postgres 로 이전).
docker run -d --name panel-studio --network proxy --restart unless-stopped \
  -v panel-studio-data:/app/server/data panel-studio:local

# 확인 — 같은 네트워크의 caddy 에서 이름:포트로 닿는지
docker exec caddy wget -qO- http://panel-studio:8797/api/health
```

**2) DNS** (옵션 A 서브도메인일 때만)

```
studio.hejdev6.goqual.com   A   115.68.110.91
```

**3) Caddy 사이트 블록 추가** — `deploy/caddy-studio.snippet` 의 옵션 A(또는 B)를
   `openglg-config/caddy/Caddyfile` 에 넣고 reload:

```bash
docker exec caddy caddy reload --config /etc/caddy/Caddyfile
```

**4) 확인** — 허용 IP 에서 `https://studio.hejdev6.goqual.com/` → 스튜디오(우상단에
   "● 서버 연결" 뱃지), 그 외 IP 에서 403. 업로드→즉시 WebP 변환, "Ray 저장소 받기" 동작.

## 업데이트 (studio.html 바뀌면)

```bash
docker build -f deploy/Dockerfile -t panel-studio:local . && docker restart panel-studio
```
> 이미지에 `web/` 를 굽는다(런타임에 호스트 경로 의존 없음). 그래서 web/ 를 고치면
> 재빌드+재시작이 필요하다.

## 제거

```bash
docker rm -f panel-studio            # 컨테이너
# + Caddyfile 에서 블록 제거 후 caddy reload, DNS 레코드 삭제
```

## 결정 필요
- **옵션 A(서브도메인, DNS 추가) vs B(경로, DNS 불필요)** — 취향/정책.
- 인증(Authelia)도 걸지: 기본은 IP 제한만. 원하면 블록에 `import authelia` 추가.
- 컨테이너 소유/관리 주체: 지금은 goqual 계정이 빌드했지만 `panel-studio` 는 공용
  proxy 네트워크에 붙으므로, 관리(재시작·업데이트) 주체를 팀에서 합의.
