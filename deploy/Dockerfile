# 패널 스튜디오 백엔드 (S1+) — 정적 web/ + /api/* 를 한 프로세스가 서빙.
# sharp(npm, prebuilt) + ffmpeg(apk) 로 에셋 변환, node 로 lift/generate/precheck.
# 빌드: docker build -f deploy/Dockerfile -t panel-studio:local .   (컨텍스트=repo 루트)

# ── 프리베이크: 생성 저장소의 Ray 빌드 의존성 ──────────────────────────
# 생성 저장소의 의존성 집합은 패널과 무관하게 고정이다(generate 가
# package.reference.json 을 이름만 바꿔 그대로 쓴다). 그래서 여기서 한 번만
# 설치해 두고 /api/build 가 패널마다 심볼릭 링크로 재사용한다.
# 실측(alpine/musl): 설치 33s(이미지 1회) · 빌드 1.8s(패널당) · 레이어 약 600MB.
# yarn 전용 resolutions + frozen-lockfile 이라 npm 으로 대체 불가.
# node_modules 를 저장소 '안'에 둔 채로 구워야 한다 — 밖에 두고 심볼릭 링크하면
# ray 가 _npm 출력 경로를 저장소 밖으로 계산해 산출물이 망가진다(실측).
FROM node:20-alpine AS panel-deps
WORKDIR /opt/panel-build
COPY templates/scaffold/package.reference.json ./package.json
COPY templates/scaffold/yarn.lock ./yarn.lock
RUN yarn install --frozen-lockfile && yarn cache clean

FROM node:20-alpine

# 영상→애니 WebP 용 ffmpeg
RUN apk add --no-cache ffmpeg

WORKDIR /app
# 의존성 먼저(레이어 캐시). sharp 는 alpine musl prebuilt 를 받는다.
COPY package.json package-lock.json ./
RUN npm ci --omit=dev

# Ray 빌드 작업장(프리베이크). 요청마다 소스만 갈아끼우고 node_modules 는 그대로 둔다.
# 없으면 /api/build 는 503 으로 정직하게 거절한다.
COPY --from=panel-deps /opt/panel-build /opt/panel-build
ENV PANEL_BUILD_DIR=/opt/panel-build

# 앱 소스 (web/ · src/ · server/ · templates/). 생성물/데이터는 제외(.dockerignore).
COPY web/ ./web/
COPY src/ ./src/
COPY server/ ./server/
COPY templates/ ./templates/

ENV PORT=8797
EXPOSE 8797
# 패널 파일 스토어는 /app/server/data — 볼륨으로 영속화 권장(S3 에서 Postgres 로 이전)
VOLUME ["/app/server/data"]
CMD ["node", "server/index.mjs"]
