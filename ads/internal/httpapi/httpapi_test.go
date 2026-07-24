package httpapi

import (
	"encoding/json"
	"fmt"
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

// 이벤트가 있는 캠페인은 **지우지 않고 보관**한다 — 이벤트가 과금 근거이기 때문.
// 서빙은 어느 쪽이든 멈춰야 한다.
func TestDeleteKeepsBillingEvidence(t *testing.T) {
	s, st := newSrv()
	mux := s.Routes()
	s.Adm = st

	// 클릭 하나를 만들어 과금 근거를 남긴다
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET",
		"/go?slot=panel.airpurifier.setting.bottom&d=dev-1", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("클릭이 생겨야 한다: %d", rec.Code)
	}
	before := len(st.Events(store.Filter{CampaignID: "c1"}))
	if before == 0 {
		t.Fatal("이벤트가 있어야 한다")
	}

	if hard := st.DeleteCampaign("c1"); hard {
		t.Fatal("이벤트가 있으면 하드 삭제하면 안 된다")
	}
	if n := len(st.Events(store.Filter{CampaignID: "c1"})); n != before {
		t.Fatalf("이벤트가 보존되어야 한다: %d → %d", before, n)
	}
	// 서빙은 멈춘다
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET",
		"/go?slot=panel.airpurifier.setting.bottom&d=dev-2", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("보관된 캠페인은 더 이상 나가면 안 된다: %d", rec.Code)
	}
}

// 이벤트가 없으면 진짜로 지운다 — 잘못 만든 것을 목록에 남길 이유가 없다.
func TestDeleteRemovesWhenNoEvents(t *testing.T) {
	st := store.NewMem()
	st.AddCampaign(model.Campaign{ID: "c9", Status: "paused"})
	if hard := st.DeleteCampaign("c9"); !hard {
		t.Fatal("이벤트가 없으면 하드 삭제해야 한다")
	}
	for _, c := range st.Campaigns() {
		if c.ID == "c9" {
			t.Fatal("목록에서 사라져야 한다")
		}
	}
}

// 반려한 소재는 더 이상 나가지 않는다.
func TestRejectedCreativeStopsServing(t *testing.T) {
	s, st := newSrv()
	mux := s.Routes()
	st.SetCreativeReview("cr1", model.ReviewRejected)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET",
		"/go?slot=panel.airpurifier.setting.bottom&d=d1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("반려된 소재는 나가면 안 된다: %d", rec.Code)
	}
}

// 청구서는 **발행 시점 숫자를 얼린다.** 이후 집계가 바뀌어도 변하면 안 된다 —
// 광고주가 받은 종이와 우리 화면이 영원히 같아야 한다.
func TestInvoiceFreezesAmount(t *testing.T) {
	st := store.NewMem()
	st.AddCampaign(model.Campaign{ID: "c1", Advertiser: "A사", Status: "active",
		Pricing: model.CPC, UnitPrice: 100})
	st.AddCreative(model.Creative{ID: "cr1", CampaignID: "c1", Review: model.ReviewApproved,
		LandingHTML: "<h1>x</h1>"})
	st.AddPlacement(model.Placement{ID: "p1", CampaignID: "c1", CreativeID: "cr1", Slot: "s"})
	s := &Server{St: st, Adm: st, Bill: st, Tr: track.New(track.NewHasher("k"), st),
		Aud: audience.Stub{}}
	mux := s.Routes()

	// 유효 클릭 2건
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET",
			fmt.Sprintf("/go?slot=s&d=dev-%d", i), nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("클릭 %d 실패: %d", i, rec.Code)
		}
	}

	month := time.Now().Format("2006-01")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/console/invoice/issue",
		strings.NewReader("advertiser=A사&month="+month))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("발행 실패: %d %s", rec.Code, rec.Body.String())
	}
	invs := st.Invoices()
	if len(invs) != 1 {
		t.Fatalf("청구서 1건이어야 한다: %d", len(invs))
	}
	inv := invs[0]
	if inv.Subtotal != 200 { // 2클릭 × 100원
		t.Fatalf("공급가액 200 이어야 한다: %d", inv.Subtotal)
	}
	if inv.VAT != 20 || inv.Total != 220 {
		t.Fatalf("부가세 20 · 합계 220 이어야 한다: %d / %d", inv.VAT, inv.Total)
	}

	// 발행 뒤 단가를 바꾸고 클릭을 더 만들어도 청구서는 그대로여야 한다
	st.AddCampaign(model.Campaign{ID: "c1", Advertiser: "A사", Status: "active",
		Pricing: model.CPC, UnitPrice: 9999})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/go?slot=s&d=dev-9", nil))

	again, _ := st.Invoice(inv.ID)
	if again.Total != 220 {
		t.Fatalf("발행된 청구서 금액이 바뀌었다: %d (동결 실패)", again.Total)
	}
}

// 입금은 멱등해야 한다 — PG 웹훅은 재시도로 여러 번 온다.
func TestPaymentIsIdempotent(t *testing.T) {
	st := store.NewMem()
	s := &Server{St: st, Adm: st, Bill: st, Tr: track.New(track.NewHasher("k"), st), Aud: audience.Stub{}}
	st.PutInvoice(model.Invoice{ID: "INV-1", Advertiser: "A사", Total: 1000, Status: "issued"})

	if err := s.applyPayment("INV-1", 1000, "가상계좌", "pay-key-1", time.Now(), "pg"); err != nil {
		t.Fatal(err)
	}
	// 같은 PaymentKey 재시도 — 두 번 더해지면 장부가 어긋난다
	if err := s.applyPayment("INV-1", 1000, "가상계좌", "pay-key-1", time.Now(), "pg"); err != nil {
		t.Fatal(err)
	}
	inv, _ := st.Invoice("INV-1")
	if inv.PaidAmount != 1000 {
		t.Fatalf("중복 반영됐다: %d", inv.PaidAmount)
	}
	if inv.Status != "paid" {
		t.Fatalf("완납 처리되어야 한다: %s", inv.Status)
	}
}

// 부분 입금은 완납이 아니다.
func TestPartialPaymentStaysOutstanding(t *testing.T) {
	st := store.NewMem()
	s := &Server{St: st, Adm: st, Bill: st, Tr: track.New(track.NewHasher("k"), st), Aud: audience.Stub{}}
	st.PutInvoice(model.Invoice{ID: "INV-2", Total: 1000, Status: "issued"})
	_ = s.applyPayment("INV-2", 400, "계좌이체", "", time.Now(), "dev")
	inv, _ := st.Invoice("INV-2")
	if inv.Status == "paid" {
		t.Fatal("부분 입금은 완납이 아니다")
	}
	if inv.Outstanding() != 600 {
		t.Fatalf("미수 600 이어야 한다: %d", inv.Outstanding())
	}
}
