package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

var t0 = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

func newSrv() (*Server, *store.Mem) {
	st := store.NewMem()
	st.AddCampaign(model.Campaign{ID: "c1", Advertiser: "헤이홈 스토어", Status: "active", Pricing: model.CPC})
	st.AddCreative(model.Creative{
		ID: "cr1", CampaignID: "c1", Format: "ad-coupon", Review: model.ReviewApproved,
		LandingHTML: `<!doctype html><h1>첫 구매 15% 할인</h1>`,
	})
	st.AddPlacement(model.Placement{
		ID: "p1", CampaignID: "c1", CreativeID: "cr1",
		Slot: "panel.airpurifier.setting.bottom", Priority: 1,
	})
	s := &Server{St: st, Tr: track.New(track.NewHasher("k"), st), Aud: audience.Stub{},
		Now: func() time.Time { return t0 }}
	return s, st
}

// 전체 사슬: /go(클릭) → /l(랜딩 도달) → /e(전환) → /report(집계)
func TestFullChain(t *testing.T) {
	s, _ := newSrv()
	mux := s.Routes()

	// 1) 문을 지난다 = 클릭
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET",
		"/go?slot=panel.airpurifier.setting.bottom&d=dev-1&p=PID1", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("302 여야 한다: %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/l/cr1") || !strings.Contains(loc, "imp=") {
		t.Fatalf("랜딩 + imp 가 있어야 한다: %s", loc)
	}
	imp := loc[strings.Index(loc, "imp=")+4:]

	// 2) 랜딩 도달
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/l/cr1?imp="+imp, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "첫 구매") {
		t.Fatalf("랜딩이 나와야 한다: %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors") {
		t.Fatal("우리 랜딩이므로 프레임 정책을 우리가 정해야 한다")
	}

	// 3) 전환
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/e",
		strings.NewReader(`{"impId":"`+imp+`","type":"convert","amount":39900}`))
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("전환 기록 실패: %d %s", rec.Code, rec.Body.String())
	}

	// 4) 집계
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/report?campaign=c1", nil))
	var out struct {
		Metrics track.Metrics `json:"metrics"`
		Source  string        `json:"source"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Metrics.Clicks != 1 || out.Metrics.LandingViews != 1 {
		t.Fatalf("클릭·랜딩도달이 1이어야 한다: %+v", out.Metrics)
	}
	if out.Metrics.Conversions != 1 || out.Metrics.Revenue != 39900 {
		t.Fatalf("전환 집계가 다르다: %+v", out.Metrics)
	}
	if out.Metrics.ArrivalRate != 1 {
		t.Fatalf("도달률이 1이어야 한다: %v", out.Metrics.ArrivalRate)
	}
	if out.Source != "stub" {
		t.Fatalf("프로파일 소스가 리포트에 드러나야 한다: %q", out.Source)
	}
}

// 내보낼 광고가 없으면 204 — 패널은 조용히 실패한다(에러 페이지 금지).
func TestNoAdIs204(t *testing.T) {
	s, _ := newSrv()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/go?slot=panel.nothing.here&d=d1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("204 여야 한다: %d", rec.Code)
	}
}

func TestSlotRequired(t *testing.T) {
	s, _ := newSrv()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/go", nil))
	if rec.Code != 400 {
		t.Fatalf("slot 없으면 400: %d", rec.Code)
	}
}

// 사슬에 없는 imp 로 전환을 주장할 수 없어야 한다(클라이언트를 믿지 않는다).
func TestUnknownImpRejected(t *testing.T) {
	s, _ := newSrv()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/e",
		strings.NewReader(`{"impId":"forged","type":"convert","amount":999999}`)))
	if rec.Code != 404 {
		t.Fatalf("모르는 imp 는 404 여야 한다: %d", rec.Code)
	}
}

// 스튜디오 → 콘솔 핸드오프. **발행 직후 그 URL 로 바로 갈 수 있어야 한다.**
// 접근 수단이 안 실리면 마케터는 발행하자마자 401 을 만난다(실제로 그랬다).
func TestPublishHandoffURLIsUsable(t *testing.T) {
	t.Setenv("ADS_ADMIN_SECRET", "sekret")
	st := store.NewMem()
	s := &Server{St: st, Adm: st, Tr: track.New(track.NewHasher("k"), st),
		Aud: audience.Stub{}, Now: func() time.Time { return t0 }}
	mux := s.Routes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/creatives",
		strings.NewReader(`{"format":"ad-lead","title":"t","landingHtml":"<h1>x</h1>"}`))
	req.Header.Set("X-Ads-Secret", "sekret")
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("발행 실패: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ConsoleURL string `json:"consoleUrl"`
		CreativeID string `json:"creativeId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out.ConsoleURL, "creative="+out.CreativeID) {
		t.Fatalf("콘솔 URL 에 소재가 실려야 한다: %s", out.ConsoleURL)
	}

	// 그 URL 로 실제로 가 본다 — 200 이어야 핸드오프가 끊기지 않는다.
	u, err := neturl.Parse(out.ConsoleURL)
	if err != nil {
		t.Fatalf("URL 파싱 실패: %v", err)
	}
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest("GET", u.RequestURI(), nil))
	if rec2.Code != 200 {
		t.Fatalf("발행 직후 콘솔이 열려야 한다(핸드오프): HTTP %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "새 소재가 도착했습니다") {
		t.Fatal("콘솔이 그 소재를 받은 상태로 열려야 한다(캠페인 만들기 폼)")
	}
}
