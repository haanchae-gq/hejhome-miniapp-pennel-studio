# 패널 스튜디오 정적 서버 — 팀 Caddy(proxy 네트워크) 뒤에 두는 컨테이너.
# web/(studio.html·index.html)만 담는다. 생성물(HANDOFF/QA·out/)은 담지 않는다.
# 빌드: docker build -f deploy/Dockerfile -t panel-studio:local .   (컨텍스트=repo 루트)
FROM caddy:2-alpine
# 기본 caddy 이미지는 /usr/share/caddy 를 :80 으로 파일 서빙한다.
COPY web/ /usr/share/caddy/
