# 광고 서버 — 정적 단일 바이너리.
# 랜딩 렌더는 스튜디오(Node)가 하므로 여기엔 런타임 의존이 없다.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/adserver ./cmd/adserver \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/adsync   ./cmd/adsync

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 ads
ENV TZ=Asia/Seoul ADS_PORT=8880
COPY --from=build /out/adserver /usr/local/bin/adserver
COPY --from=build /out/adsync   /usr/local/bin/adsync
USER ads
EXPOSE 8880
ENTRYPOINT ["/usr/local/bin/adserver"]
