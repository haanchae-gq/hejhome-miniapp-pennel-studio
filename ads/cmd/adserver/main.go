// Command adserver — 헤이홈 광고 서버 (1단계).
//
//	go run ./cmd/adserver          # ADS_PORT=8880 기본
//
// 1단계 범위: 결정 → 클릭 기록 → 랜딩 서빙 → 이벤트 수집 → 집계.
// 저장소는 메모리다(프로세스가 죽으면 사라진다) — Postgres 는 store.Store 를 구현해 갈아끼운다.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/httpapi"
	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	port := env("ADS_PORT", "8880")
	base := env("ADS_BASE_URL", "")

	st := store.NewMem()
	seed(st)

	srv := &httpapi.Server{
		St:   st,
		Tr:   track.New(track.NewHasher(os.Getenv("ADS_HASH_SECRET")), st),
		Aud:  audience.Stub{}, // ← 계정 프로파일 소스가 정해지면 여기만 바꾼다
		Base: base,
	}

	h := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("광고 서버 → http://127.0.0.1:%s  (저장소: memory · 프로파일 소스: %s)", port, audience.Stub{}.Name())
		if os.Getenv("ADS_HASH_SECRET") == "" {
			log.Printf("  ⚠ ADS_HASH_SECRET 미설정 — 재시작마다 해시가 바뀐다(빈도 제한·중복 판정이 초기화됨)")
		}
		if err := h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.Shutdown(ctx)
	log.Println("종료")
}

// seed — 데모용 캠페인 하나. 첫 광고주는 우리 자신(자사 커머스)이라는 설계 그대로다.
func seed(st *store.Mem) {
	st.AddCampaign(model.Campaign{
		ID: "c-hej-store", Advertiser: "헤이홈 스토어(자사)", Status: "active",
		Pricing: model.CPC, DailyCap: 0,
	})
	st.AddCreative(model.Creative{
		ID: "cr-filter", CampaignID: "c-hej-store", Format: "ad-coupon",
		Review: model.ReviewApproved, Title: "필터 교체 안내",
		LandingHTML: `<!doctype html><meta charset="utf-8">` +
			`<meta name="viewport" content="width=device-width,initial-scale=1">` +
			`<title>필터 교체할 때가 됐어요</title>` +
			`<body style="font-family:-apple-system,system-ui,sans-serif;max-width:520px;margin:0 auto;padding:24px">` +
			`<h1 style="font-size:22px">필터 교체할 때가 됐어요</h1>` +
			`<p style="color:#8B95A1">정품 필터로 성능을 유지하세요.</p>` +
			`<a href="https://m.hej.life/" style="display:block;text-align:center;background:#00A872;color:#fff;` +
			`padding:15px;border-radius:14px;text-decoration:none;font-weight:700">지금 주문하기</a>` +
			`</body>`,
	})
	// 인텐트 타게팅 — 필터 수명이 0일 때만 나간다.
	st.AddPlacement(model.Placement{
		ID: "pl-filter", CampaignID: "c-hej-store", CreativeID: "cr-filter",
		Slot: "panel.airpurifier.consumable", Priority: 10,
		Targeting: model.Targeting{DP: map[string]string{"filter_life": "0"}},
		FreqCap:   &model.FreqCap{Max: 3, WindowSec: 86400},
	})
}
