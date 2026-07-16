# 패널 스튜디오 백엔드 (S1+) — 정적 web/ + /api/* 를 한 프로세스가 서빙.
# sharp(npm, prebuilt) + ffmpeg(apk) 로 에셋 변환, node 로 lift/generate/precheck.
# 빌드: docker build -f deploy/Dockerfile -t panel-studio:local .   (컨텍스트=repo 루트)
FROM node:20-alpine

# 영상→애니 WebP 용 ffmpeg
RUN apk add --no-cache ffmpeg

WORKDIR /app
# 의존성 먼저(레이어 캐시). sharp 는 alpine musl prebuilt 를 받는다.
COPY package.json package-lock.json ./
RUN npm ci --omit=dev

# 앱 소스 (web/ · src/ · server/). 생성물/데이터는 제외(.dockerignore).
COPY web/ ./web/
COPY src/ ./src/
COPY server/ ./server/

ENV PORT=8797
EXPOSE 8797
# 패널 파일 스토어는 /app/server/data — 볼륨으로 영속화 권장(S3 에서 Postgres 로 이전)
VOLUME ["/app/server/data"]
CMD ["node", "server/index.mjs"]
