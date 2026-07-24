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
	"strconv"
	"syscall"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/httpapi"
	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/payments"
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

	// 저장소 — 이벤트가 곧 과금 근거다. 운영은 반드시 Postgres.
	var (
		serving store.Store
		admin   store.Admin
		billing store.Billing
		kind    = "memory"
	)
	if url := os.Getenv("ADS_DATABASE_URL"); url != "" {
		pgs, err := store.NewPostgres(context.Background(), url)
		if err != nil {
			log.Fatalf("Postgres 연결 실패: %v", err) // 과금 데이터를 메모리로 조용히 떨어뜨리지 않는다
		}
		defer pgs.Close()
		serving, admin, billing, kind = pgs, pgs, pgs, pgs.Kind()
	} else {
		mem := store.NewMem()
		seed(mem)
		serving, admin, billing = mem, mem, mem
		log.Printf("  ⚠ ADS_DATABASE_URL 미설정 — 메모리 저장소(프로세스 종료 시 이벤트 소실). 검증용으로만.")
	}

	// 프로파일 소스 — env 로 갈아끼운다(이관 시 코드 변경 없음).
	//   ADS_VALKEY_ADDR 있음 → Valkey 서빙 스토어 (StarRocks 동기화 결과)
	//   없음                 → Stub ("모른다" — 프로파일 타게팅은 fail closed)
	var aud audience.Provider = audience.Stub{}
	if addr := os.Getenv("ADS_VALKEY_ADDR"); addr != "" {
		db, _ := strconv.Atoi(env("ADS_VALKEY_DB", "0"))
		ttl, _ := time.ParseDuration(env("ADS_PROFILE_TTL", "48h"))
		vk := store.NewValkey(addr, os.Getenv("ADS_VALKEY_PASSWORD"), db, ttl, os.Getenv("ADS_VALKEY_PREFIX"))
		if err := vk.Ping(context.Background()); err != nil {
			// 프로파일이 없어도 광고는 나가야 한다(비타게팅). 죽지 않고 Stub 으로 떨어진다.
			log.Printf("  ⚠ Valkey 연결 실패(%s) — 프로파일 타게팅 없이 시작한다: %v", addr, err)
		} else {
			cacheTTL, _ := time.ParseDuration(env("ADS_PROFILE_CACHE_TTL", "5m"))
			aud = audience.NewStoreProvider(vk, cacheTTL)
			if at, n, err := vk.Meta(context.Background()); err == nil && !at.IsZero() {
				log.Printf("  프로파일 스냅샷: %d건 · 적재 %s", n, at.Format(time.RFC3339))
			} else {
				log.Printf("  ⚠ 프로파일 스냅샷이 아직 없다 — adsync 를 먼저 돌려라")
			}
		}
	}

	// 수납 — 토스페이먼츠 키가 있으면 그쪽, 없으면 수동 입금 확인.
	var pg payments.Provider = payments.Manual{}
	if t := payments.NewToss(); t != nil {
		pg = t
		log.Printf("  수납: 토스페이먼츠(가상계좌) — 웹훅 /api/pg/webhook")
	} else {
		log.Printf("  수납: 수동 입금 확인 (ADS_TOSS_SECRET_KEY 설정 시 토스페이먼츠)")
	}

	srv := &httpapi.Server{
		St:   serving,
		Adm:  admin,
		Bill: billing,
		PG:   pg,
		Tr:   track.New(track.NewHasher(os.Getenv("ADS_HASH_SECRET")), serving),
		Aud:  aud,
		Base: base,
	}

	h := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("광고 서버 → http://127.0.0.1:%s  (저장소: %s · 프로파일 소스: %s)", port, kind, aud.Name())
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
	// 프로파일 타게팅 데모 — 공기청정기를 '잘 쓰는' 집에만 나간다.
	// Valkey 스냅샷이 없으면 fail closed 라 아예 안 나간다(조용히 전체 노출로 새지 않는다).
	st.AddCreative(model.Creative{
		ID: "cr-upsell", CampaignID: "c-hej-store", Format: "ad-brandweek",
		Review: model.ReviewApproved, Title: "상위 모델 추천",
		LandingHTML: `<!doctype html><meta charset="utf-8"><title>상위 모델</title><h1>더 넓은 공간을 위한 상위 모델</h1>`,
	})
	st.AddPlacement(model.Placement{
		ID: "pl-upsell", CampaignID: "c-hej-store", CreativeID: "cr-upsell",
		Slot: "panel.airpurifier.setting.bottom", Priority: 5,
		Targeting: model.Targeting{UsesHeavily: []string{"airpurifier"}},
	})

	// 인텐트 타게팅 — 필터 수명이 0일 때만 나간다.
	st.AddPlacement(model.Placement{
		ID: "pl-filter", CampaignID: "c-hej-store", CreativeID: "cr-filter",
		Slot: "panel.airpurifier.consumable", Priority: 10,
		Targeting: model.Targeting{DP: map[string]string{"filter_life": "0"}},
		FreqCap:   &model.FreqCap{Max: 3, WindowSec: 86400},
	})
}
