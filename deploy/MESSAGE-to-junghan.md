# 정한님께 — 패널 스튜디오를 팀 Caddy 뒤에 붙이고 싶어요 (검토 부탁)

임베디드AI엣지팀에서 만든 **패널 스튜디오**(Tuya 미니앱 패널 웹 저작도구)를 외부에서
접속할 수 있게 하려는데, 80/443 을 정한님 Caddy(`openglg-config/caddy`)가 쥐고 있고
그 config 레포는 제 계정으로 접근이 안 돼서(권한 없음) 여기서 넘깁니다. 남의 관리형·
Authelia 인증 인프라라 제가 임의로 손대지 않았어요.

## 원하는 것
- `https://studio.hejdev6.goqual.com/` (또는 경로 `/panel-studio/`)로 스튜디오 접속
- **접속 허용 IP: `211.106.187.123` 한 곳만**, 나머지는 403
- 호스트 `ufw` 는 **건드리지 말자**는 쪽 — 공용 호스트라 전역 방화벽은 위험하고,
  IP 제한은 Caddy `remote_ip` 로 충분합니다.

## 제가 준비해 둔 것 (repo `~/repos/work/miniapp-panel-studio/deploy/`)
- `Dockerfile` — 스튜디오 정적 서버 이미지(`web/` 만, 생성물은 안 담음)
- `caddy-studio.snippet` — 추가할 Caddy 사이트 블록(IP 제한 포함, 서브도메인/경로 2안)
- `README.md` — 빌드·기동·적용·업데이트·제거 전 절차

## 정한님이 해줄 것 (docker + Caddy + DNS 소유자)
1. 컨테이너 기동 — repo 루트에서:
   ```
   docker build -f deploy/Dockerfile -t panel-studio:local .
   docker run -d --name panel-studio --network proxy --restart unless-stopped panel-studio:local
   ```
   (proxy 네트워크에 붙여야 caddy 가 이름으로 닿습니다. 호스트 포트는 안 엽니다.)
2. (서브도메인 안이면) DNS A: `studio.hejdev6.goqual.com → 115.68.110.91`
3. `caddy-studio.snippet` 의 블록을 Caddyfile 에 추가 후:
   ```
   docker exec caddy caddy reload --config /etc/caddy/Caddyfile
   ```

## 확인/결정 부탁
- **서브도메인 vs 경로** 중 팀 관례에 맞는 쪽으로 골라 주세요.
- 컨테이너를 공용 proxy 네트워크에 두는 것 — 제가 직접 붙이지 않고 넘기는 이유가
  "공유 인프라라 소유자 확인 먼저"여서예요. 관리 주체(재시작·업데이트)는 편한 대로
  정해 주시면 그에 맞춰 문서 고칠게요.
- Authelia 도 걸까요? 기본은 IP 제한만 해뒀습니다(블록에 `import authelia` 한 줄로 추가 가능).

궁금한 점 있으면 알려주세요. 이미지·스니펫 다 맞춰뒀으니 위 3스텝이면 뜹니다.
— 임베디드AI엣지팀 (goqual 계정, devteam@goqual.com)
