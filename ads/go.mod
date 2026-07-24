// 광고 서버 — 지금은 panel-studio 리포 안에 있지만, 검증 후 떼어낼 것을 전제로
// 모듈 경로를 목적지 이름으로 잡는다(분리 시 디렉터리만 옮기면 된다).
module github.com/teamgoqual/hej-adserver

go 1.25.0

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/redis/go-redis/v9 v9.21.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/text v0.29.0 // indirect
)
