#!/usr/bin/env bash
# 패널 스튜디오 재배포 — 라이브 컨테이너(panel-studio)를 현재 소스로 갈아끼운다.
#
#   bash deploy/redeploy.sh
#
# 왜 스크립트인가: 재배포는 "env 를 잃지 않고 컨테이너를 갈아끼우는 것"이 전부다.
# 긴 docker run 을 손으로 붙여넣다 줄이 잘리면 env 가 비어 로그인 세션과 DB 연결이
# 통째로 날아간다. 그 사고를 막으려고 한 파일로 묶고, 매 단계에서 멈출 조건을 건다.
#
# 롤백: docker rm -f panel-studio && docker run … panel-studio:pre-adpack
#       (이 스크립트가 끝에 정확한 명령을 출력한다)
set -euo pipefail

NAME=panel-studio
IMAGE=panel-studio:local
NET=proxy
DATA=/disk-A/docker-data/panel-studio/data
ENVF="$HOME/.${NAME}.env"
ROLLBACK="panel-studio:pre-$(date +%Y%m%d-%H%M)"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

say() { printf '\n\033[1m▸ %s\033[0m\n' "$1"; }
die() { printf '\n\033[31m✘ %s\033[0m\n' "$1" >&2; exit 1; }

# ── 0) 대상 확인 ─────────────────────────────────────────────────────────────
say "대상 확인"
docker inspect "$NAME" >/dev/null 2>&1 || die "컨테이너 '$NAME' 이 없다. 이름을 확인해라."
docker inspect "$NAME" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}' \
  | command grep -qx "$NET" || die "'$NAME' 이 '$NET' 네트워크에 없다 — 라이브 컨테이너가 맞는지 확인해라."
echo "  $NAME · 네트워크 $NET · 데이터 $DATA"

# ── 1) env 캡처 (가장 중요) ──────────────────────────────────────────────────
# 컨테이너를 지우면 docker run 때 준 env(시크릿)는 되찾을 수 없다. 먼저 뽑고,
# 개수가 모자라면 아예 진행하지 않는다. PATH·NODE_*·YARN_* 은 이미지가 주는 것이라
# --env-file 로 되먹이면 컨테이너 PATH 를 덮어써 깨진다 — 걸러낸다.
say "env 캡처 → $ENVF"
umask 077
docker inspect "$NAME" --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | command grep -vE '^(PATH|NODE_|YARN_|HOME=|HOSTNAME=)|^$' > "$ENVF"
chmod 600 "$ENVF"
# 새 env 를 추가할 자리. 캡처는 "지금 도는 컨테이너"에서 오므로, 아직 컨테이너에 없는
# 값(새 기능의 env)은 여기에 둔다. 없으면 무시한다.
EXTRA="$HOME/.${NAME}.extra.env"
if [ -f "$EXTRA" ]; then
  # 같은 키가 양쪽에 있으면 EXTRA 가 이긴다(의도적으로 바꾸려는 값이므로).
  command awk -F= 'NR==FNR{k[$1];next} !($1 in k)' "$EXTRA" "$ENVF" > "$ENVF.tmp"
  command cat "$EXTRA" >> "$ENVF.tmp"
  command mv "$ENVF.tmp" "$ENVF"
  chmod 600 "$ENVF"
  echo "  추가 env 병합: $EXTRA ($(command wc -l < "$EXTRA") 줄)"
fi

N=$(command wc -l < "$ENVF")
[ "$N" -ge 5 ] || die "env 가 $N 줄뿐이다(5줄 미만). 캡처 실패로 보고 중단한다 — $ENVF 를 확인해라."
echo "  $N 개 캡처:"
command cut -d= -f1 "$ENVF" | command sed 's/^/    /'

# ── 2) 롤백 태그 ─────────────────────────────────────────────────────────────
say "롤백 태그 $ROLLBACK"
docker tag "$IMAGE" "$ROLLBACK"

# ── 3) 빌드 ──────────────────────────────────────────────────────────────────
# 실패하면 여기서 멈춘다 — 기존 컨테이너는 아직 살아 있다(무중단).
say "이미지 빌드"
docker build -f "$REPO/deploy/Dockerfile" -t "$IMAGE" "$REPO"

# ── 4) 교체 ──────────────────────────────────────────────────────────────────
say "컨테이너 교체"
docker rm -f "$NAME"
docker run -d --name "$NAME" --network "$NET" --restart unless-stopped \
  --env-file "$ENVF" -v "$DATA:/app/server/data" "$IMAGE"

# ── 5) 확인 ──────────────────────────────────────────────────────────────────
say "헬스체크"
IP=$(docker inspect "$NAME" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
for _ in $(seq 1 15); do
  if curl -sS --max-time 3 "http://$IP:8797/api/health" 2>/dev/null | command grep -q '"ok":true'; then
    echo "  ✔ http://$IP:8797/api/health 응답 정상"
    curl -sS "http://$IP:8797/api/health"; echo
    say "완료 — panel-studio.goqual-internal.com 에서 확인"
    echo "  롤백이 필요하면:"
    echo "    docker rm -f $NAME"
    echo "    docker run -d --name $NAME --network $NET --restart unless-stopped --env-file $ENVF -v $DATA:/app/server/data $ROLLBACK"
    exit 0
  fi
  sleep 1
done

die "헬스체크 실패. 로그: docker logs $NAME --tail 50 / 롤백 이미지: $ROLLBACK"
