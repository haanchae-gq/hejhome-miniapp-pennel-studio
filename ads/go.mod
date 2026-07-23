// 광고 서버 — 지금은 panel-studio 리포 안에 있지만, 검증 후 떼어낼 것을 전제로
// 모듈 경로를 목적지 이름으로 잡는다(분리 시 디렉터리만 옮기면 된다).
module github.com/teamgoqual/hej-adserver

go 1.25

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)
