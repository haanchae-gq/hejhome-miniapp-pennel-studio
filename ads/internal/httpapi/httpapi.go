// Package httpapi — 광고 서버의 HTTP 표면.
//
//	GET  /healthz                     → ok
//	GET  /go?slot=&d=&p=&s=&a=        → 결정 → 클릭 기록 → 302 랜딩 (없으면 204)
//	GET  /l/{creativeID}?imp=         → 랜딩 HTML (스튜디오가 발행한 것) + landing_view 기록
//	POST /e                           → 이벤트 수집 (engage·lead·convert)
//	GET  /report?campaign=&since=     → 집계 지표
//
// 원칙 하나: **광고 서버가 죽어도 기기 제어에는 영향이 없다.** 패널은 링크만 갖고 있고,
// 최악의 경우 링크가 안 열리는 것으로 끝나야 한다.
package httpapi

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/decide"
	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

type Server struct {
	St   store.Store
	Tr   *track.Tracker
	Aud  audience.Provider
	Now  func() time.Time
	Base string // 랜딩 절대 URL 을 만들 때 쓰는 베이스 (비면 상대경로)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /go", s.goRedirect)
	mux.HandleFunc("GET /l/{creativeID}", s.landing)
	mux.HandleFunc("POST /e", s.event)
	mux.HandleFunc("GET /report", s.report)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "hej-adserver"})
}

// goRedirect — 패널이 여는 문. 여기를 지나는 순간이 곧 클릭이다.
func (s *Server) goRedirect(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	q := r.URL.Query()
	slot := q.Get("slot")
	if slot == "" {
		http.Error(w, "slot required", 400)
		return
	}

	h := s.Tr.Hasher()
	ctx := decide.Ctx{
		ProductID:   q.Get("p"),
		DeviceHash:  h.Hash(q.Get("d"), now),
		AccountHash: h.HashStable(q.Get("a")), // 프로파일 조회 키는 회전하지 않는다
		DP:          decide.ParseDP(q.Get("s")),
	}

	clicks, ts, camps := s.St.Stats(ctx.DeviceHash, now)
	recent := make([]decide.RecentHit, 0, len(ts))
	for i := range ts {
		recent = append(recent, decide.RecentHit{CampaignID: camps[i], TS: ts[i]})
	}

	res := decide.Decide(r.Context(), slot, ctx, s.St.Candidates(slot),
		decide.Stats{ClicksToday: clicks, DeviceRecent: recent}, s.Aud, now)

	if res.Chosen == nil {
		// 내보낼 광고가 없다. 패널은 조용히 실패한다 — 에러 페이지를 보여주지 않는다.
		w.Header().Set("X-Ad-Skipped", strings.Join(reasons(res.Skipped), "; "))
		w.WriteHeader(http.StatusNoContent)
		return
	}

	c := *res.Chosen
	impID := track.NewImpID(now)
	if _, err := s.Tr.Record(model.Event{
		ImpID: impID, Type: model.EvClick, CampaignID: c.Campaign.ID,
		CreativeID: c.Creative.ID, Slot: slot, DeviceHash: ctx.DeviceHash,
		ProductID: ctx.ProductID,
	}, now); err != nil {
		http.Error(w, "record failed", 500)
		return
	}

	dest := c.Creative.LandingURL
	if dest == "" {
		dest = s.Base + "/l/" + c.Creative.ID
	}
	sep := "?"
	if strings.Contains(dest, "?") {
		sep = "&"
	}
	http.Redirect(w, r, dest+sep+"imp="+impID, http.StatusFound)
}

// landing — 스튜디오가 발행한 랜딩 HTML 을 그대로 서빙한다.
// 렌더 규격(위젯→HTML)은 스튜디오(Node)에 남는다 — 여기서 다시 만들지 않는다.
func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("creativeID")
	cr, ok := s.St.Creative(id)
	if !ok || cr.LandingHTML == "" {
		http.NotFound(w, r)
		return
	}
	if imp := r.URL.Query().Get("imp"); imp != "" {
		_, _ = s.Tr.Record(model.Event{
			ImpID: imp, Type: model.EvLandingView,
			CampaignID: cr.CampaignID, CreativeID: cr.ID,
		}, s.now())
	}
	// 우리 랜딩이라 프레임 정책을 우리가 정한다 — 앱 안에서 열 수 있다(설계서 §4).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self' https://*.hej.life https://*.goqual.com")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write([]byte(cr.LandingHTML))
}

type eventReq struct {
	ImpID  string `json:"impId"`
	Type   string `json:"type"`
	Amount int64  `json:"amount"`
}

// event — 랜딩에서 올라오는 관여·리드·전환.
func (s *Server) event(w http.ResponseWriter, r *http.Request) {
	var req eventReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	t := model.EventType(req.Type)
	switch t {
	case model.EvEngage, model.EvLead, model.EvConvert:
	default:
		http.Error(w, "bad type", 400)
		return
	}
	if req.ImpID == "" {
		http.Error(w, "impId required", 400)
		return
	}
	// 사슬을 거슬러 캠페인·소재·기기를 채운다 — 클라이언트가 주장하게 두지 않는다.
	base := s.St.Events(store.Filter{})
	var seed *model.Event
	for i := range base {
		if base[i].ImpID == req.ImpID && base[i].Type == model.EvClick {
			seed = &base[i]
			break
		}
	}
	if seed == nil {
		http.Error(w, "unknown imp", 404)
		return
	}
	ev, err := s.Tr.Record(model.Event{
		ImpID: req.ImpID, Type: t, CampaignID: seed.CampaignID, CreativeID: seed.CreativeID,
		Slot: seed.Slot, DeviceHash: seed.DeviceHash, ProductID: seed.ProductID, Amount: req.Amount,
	}, s.now())
	if err != nil {
		http.Error(w, "record failed", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "billable": ev.Billable})
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.Filter{CampaignID: q.Get("campaign"), CreativeID: q.Get("creative"), Slot: q.Get("slot")}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = t
		}
	}
	evs := s.St.Events(f)
	writeJSON(w, 200, map[string]any{
		"filter":  f,
		"metrics": track.Aggregate(evs),
		"source":  audSource(s.Aud),
	})
}

func audSource(p audience.Provider) string {
	if p == nil {
		return "none"
	}
	return p.Name()
}

func reasons(sk []decide.Skip) []string {
	out := make([]string, 0, len(sk))
	for _, s := range sk {
		out = append(out, html.EscapeString(s.CreativeID+":"+s.Reason))
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
