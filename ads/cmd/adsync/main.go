// Command adsync — 프로파일 동기화 잡.
//
//	StarRocks  v_ad_device_profile  ──(추후)──┐
//	                                          ├─▶ Valkey (세대 원자 교체)
//	JSONL 파일  (지금)  ──────────────────────┘
//
//	adsync -in profiles.jsonl                    # 파일에서
//	adsync -in -                                 # 표준입력에서
//
// **지금은 JSONL 입력만 지원한다.** StarRocks 직결은 데이터팀 리뷰(ads/analytics/README.md)
// 로 MV·서빙 뷰가 확정된 뒤에 붙인다 — 결합키·계정 매핑이 아직 미확정이라 지금 붙이면
// 틀린 쿼리를 박게 된다. 그때까지는 `v_ad_device_profile` 를 JSONL 로 뽑아 이 잡에 물린다.
//
// 입력 한 줄 = v_ad_device_profile 한 행:
//
//	{"dev_id":"<가명화된 키>","category_id":"airpurifier","usage_level":"heavy",
//	 "event_count_28d":420,"active_days_28d":26}
//
// ⚠ dev_id 는 **이미 가명화된 값**이어야 한다. 원본 식별자를 광고 서버로 넘기지 않는다.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type row struct {
	DevID         string            `json:"dev_id"`
	CategoryID    string            `json:"category_id"`
	ProductKey    string            `json:"product_key"`
	UsageLevel    string            `json:"usage_level"`
	EventCount28d int64             `json:"event_count_28d"`
	ActiveDays28d int               `json:"active_days_28d"`
	Sectors       []audience.Sector `json:"sectors,omitempty"`
}

func main() {
	in := flag.String("in", "-", "입력 JSONL 파일 ('-' 는 표준입력)")
	hashInput := flag.Bool("hash-input", false,
		"입력 dev_id 가 원본일 때 지정 — 이 잡이 광고 서버와 같은 방식으로 가명화한다(ADS_HASH_SECRET 필요)")
	flag.Parse()

	// 가명화 경계는 이 잡이다. -hash-input 을 쓰면 원본 dev_id 가 Valkey 로 넘어가지 않는다.
	hasher := track.NewHasher(os.Getenv("ADS_HASH_SECRET"))
	keyOf := func(id string) string {
		if *hashInput {
			return hasher.HashStable(id)
		}
		return id
	}

	addr := env("ADS_VALKEY_ADDR", "127.0.0.1:6379")
	db, _ := strconv.Atoi(env("ADS_VALKEY_DB", "0"))
	ttl, err := time.ParseDuration(env("ADS_PROFILE_TTL", "48h"))
	if err != nil {
		log.Fatalf("ADS_PROFILE_TTL 파싱 실패: %v", err)
	}
	vk := store.NewValkey(addr, os.Getenv("ADS_VALKEY_PASSWORD"), db, ttl, os.Getenv("ADS_VALKEY_PREFIX"))
	defer vk.Close()

	ctx := context.Background()
	if err := vk.Ping(ctx); err != nil {
		log.Fatalf("Valkey 연결 실패(%s): %v", addr, err)
	}

	var f *os.File
	if *in == "-" {
		f = os.Stdin
	} else {
		f, err = os.Open(*in)
		if err != nil {
			log.Fatalf("입력 열기 실패: %v", err)
		}
		defer f.Close()
	}

	w, err := vk.BeginWrite(ctx, "jsonl")
	if err != nil {
		log.Fatalf("쓰기 시작 실패: %v", err)
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	bad := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r row
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.DevID == "" {
			bad++
			continue
		}
		cat := r.CategoryID
		if cat == "" {
			cat = r.ProductKey
		}
		p := &audience.Profile{
			AccountHash: r.DevID,
			Categories:  []string{cat},
			Usage:       map[string]audience.UsageLevel{cat: audience.UsageLevel(r.UsageLevel)},
			Sectors:     r.Sectors,
			Source:      "starrocks",
			FetchedAt:   time.Now(),
		}
		p.AccountHash = keyOf(r.DevID)
		if err := w.Put(ctx, keyOf(r.DevID), p); err != nil {
			log.Fatalf("적재 실패: %v", err)
		}
	}
	if err := sc.Err(); err != nil {
		log.Fatalf("입력 읽기 실패: %v", err)
	}

	n, err := w.Commit(ctx)
	if err != nil {
		// 빈 스냅샷 거부도 여기로 온다 — 전체 타게팅이 조용히 꺼지는 사고를 막는다.
		log.Fatalf("반영 실패: %v", err)
	}
	log.Printf("✔ 프로파일 %d건 반영 (불량 %d건 건너뜀) · TTL %s · prefix %s",
		n, bad, ttl, env("ADS_VALKEY_PREFIX", store.DefaultPrefix))
}
