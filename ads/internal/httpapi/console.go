package httpapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

/*
콘솔 — 캠페인 관리.

## 책임 분리

	패널 스튜디오  = 소재 공장 (랜딩을 만든다)
	광고 콘솔      = 캠페인 소유 (언제·어디에·누구에게 내보낼지 정한다)

스튜디오에서 "광고로 발행"을 누르면 랜딩 HTML 이 여기로 올라오고(소재 생성),
브라우저는 **그 소재가 선택된 채로** 캠페인 만들기 화면으로 넘어온다. 인계가 끊기지 않되
책임은 갈린다 — 스튜디오가 캠페인까지 떠안으면 저작도구의 성격이 바뀐다.

## 인증

발행 API(`/api/creatives`)는 스튜디오 백엔드가 서버 대 서버로 부른다 → 공유 시크릿.
콘솔 UI 는 사람이 보는 것 → 지금은 같은 시크릿을 쿼리로 받는 개발용 게이트다.
운영 이관 시 스튜디오와 같은 구글 OIDC(@goqual.com)로 바꾼다.
*/

func adminSecret() string { return os.Getenv("ADS_ADMIN_SECRET") }

// authed — 개발용 게이트. 시크릿이 설정되지 않았으면 열어 둔다(로컬 개발).
func authed(r *http.Request) bool {
	s := adminSecret()
	if s == "" {
		return true
	}
	if h := r.Header.Get("X-Ads-Secret"); h == s {
		return true
	}
	if r.URL.Query().Get("k") == s {
		return true
	}
	// 폼 POST 는 k 를 본문으로 보낸다. JSON 본문(발행 API)은 건드리지 않는다 —
	// ParseForm 이 본문을 소비해 뒤에서 디코드가 실패한다.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err == nil && r.PostFormValue("k") == s {
			return true
		}
	}
	return false
}

func (s *Server) RegisterConsole(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/creatives", s.createCreative)
	mux.HandleFunc("GET /console", s.console)
	mux.HandleFunc("POST /console/campaign", s.createCampaign)
	mux.HandleFunc("POST /console/status", s.setStatus)
	mux.HandleFunc("GET /console/campaign/{id}", s.campaignDetail)
	mux.HandleFunc("GET /console/tokens.css", s.tokens)
	mux.HandleFunc("GET /console/report", s.reportPage)
	mux.HandleFunc("GET /console/audit", s.auditPage)
	mux.HandleFunc("POST /console/schedule", s.setSchedule)
	mux.HandleFunc("POST /console/review", s.setReview)
	mux.HandleFunc("POST /console/attach", s.attachCreative)
}

type createCreativeReq struct {
	Format      string `json:"format"`
	Title       string `json:"title"`
	LandingHTML string `json:"landingHtml"`
	CampaignID  string `json:"campaignId,omitempty"`
}

// createCreative — 스튜디오의 "광고로 발행" 이 도착하는 곳.
// 소재만 만든다. 캠페인·배치는 사람이 콘솔에서 정한다(자동으로 라이브가 되지 않는다).
func (s *Server) createCreative(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var req createCreativeReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if strings.TrimSpace(req.LandingHTML) == "" {
		http.Error(w, "landingHtml required", 400)
		return
	}
	id := "cr-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	cr := model.Creative{
		ID: id, CampaignID: req.CampaignID, Format: req.Format, Title: req.Title,
		// 발행했다고 바로 나가지 않는다 — 검수를 거쳐야 한다.
		Review:      model.ReviewPending,
		LandingHTML: req.LandingHTML,
	}
	s.Adm.AddCreative(cr)
	// 콘솔로 넘어가는 링크에 **접근 수단을 함께 실어야** 핸드오프가 끊기지 않는다.
	// OIDC 가 켜져 있으면 콘솔이 로그인으로 유도하므로 키가 필요 없고,
	// 개발 모드(시크릿 게이트)면 키를 붙여 준다 — 안 붙이면 발행 직후 401 을 만난다.
	consoleURL := s.Base + "/console?creative=" + id
	if s.OIDC == nil && adminSecret() != "" {
		consoleURL += "&k=" + url.QueryEscape(adminSecret())
	}
	s.Adm.Audit(model.Audit{Actor: "studio", Action: "creative.publish", Target: id,
		Detail: "스튜디오에서 발행 · 포맷 " + req.Format + " · " + req.Title})
	writeJSON(w, 200, map[string]any{
		"ok": true, "creativeId": id,
		"consoleUrl": consoleURL,
		"previewUrl": fmt.Sprintf("%s/l/%s", s.Base, id),
	})
}

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	f := r.Form
	crID := f.Get("creative")
	cr, ok := s.Adm.Creative(crID)
	if !ok {
		http.Error(w, "unknown creative", 404)
		return
	}
	campID := "c-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	prio, _ := strconv.Atoi(f.Get("priority"))
	cap_, _ := strconv.Atoi(f.Get("dailyCap"))

	s.Adm.AddCampaign(model.Campaign{
		ID: campID, Advertiser: f.Get("advertiser"),
		Status:  "paused", // 만들자마자 나가지 않는다. 검수 후 사람이 켠다.
		Pricing: model.Pricing(f.Get("pricing")), DailyCap: cap_,
	})
	cr.CampaignID = campID
	if f.Get("approve") == "on" {
		cr.Review = model.ReviewApproved
	}
	s.Adm.AddCreative(cr)

	t := model.Targeting{}
	if v := strings.TrimSpace(f.Get("dpKey")); v != "" {
		t.DP = map[string]string{v: f.Get("dpVal")}
	}
	if v := strings.TrimSpace(f.Get("usesHeavily")); v != "" {
		t.UsesHeavily = strings.Split(v, ",")
	}
	s.Adm.AddPlacement(model.Placement{
		ID: "pl-" + campID, CampaignID: campID, CreativeID: crID,
		Slot: f.Get("slot"), Priority: prio, Targeting: t,
	})
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "campaign.create", Target: campID,
		Detail: fmt.Sprintf("광고주 %s · 슬롯 %s · %s", f.Get("advertiser"), f.Get("slot"), f.Get("pricing"))})
	http.Redirect(w, r, s.consoleURL(r, ""), http.StatusSeeOther)
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	s.Adm.SetCampaignStatus(r.FormValue("campaign"), r.FormValue("status"))
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "campaign.status",
		Target: r.FormValue("campaign"), Detail: "→ " + r.FormValue("status")})
	http.Redirect(w, r, s.consoleURL(r, ""), http.StatusSeeOther)
}

func (s *Server) consoleURL(r *http.Request, extra string) string {
	u := "/console"
	if k := r.URL.Query().Get("k"); k != "" {
		u += "?k=" + k
		if extra != "" {
			u += "&" + extra
		}
	} else if extra != "" {
		u += "?" + extra
	}
	return u
}

func (s *Server) console(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	k := r.URL.Query().Get("k")
	newCreative := r.URL.Query().Get("creative")

	var b strings.Builder
	b.WriteString(consoleHead)

	// 스튜디오에서 방금 발행하고 넘어온 경우 — 캠페인 만들기 폼을 먼저 보여준다.
	if cr, ok := s.Adm.Creative(newCreative); ok {
		fmt.Fprintf(&b, `<section class="card hi">
<h2>새 소재가 도착했습니다 — 캠페인을 만들어 주세요</h2>
<p class="sub">소재 <code>%s</code> · 포맷 <code>%s</code> · <a href="/l/%s" target="_blank">랜딩 미리보기 ↗</a></p>
<form method="post" action="/console/campaign%s">
<input type="hidden" name="creative" value="%s">
<div class="grid">
  <label>광고주<input name="advertiser" value="%s" required></label>
  <label>과금<select name="pricing"><option value="cpc">CPC (클릭당)</option><option value="cpa">CPA (성과당)</option><option value="cpt">CPT (기간정액)</option></select></label>
  <label>슬롯<input name="slot" placeholder="panel.airpurifier.setting.bottom" required></label>
  <label>우선순위<input name="priority" type="number" value="10"></label>
  <label>일일 클릭 한도<input name="dailyCap" type="number" value="0" title="0 = 무제한"></label>
  <label>기기 상태 조건 (선택)<input name="dpKey" placeholder="filter_life"></label>
  <label>그 값<input name="dpVal" placeholder="0"></label>
  <label>잘 쓰는 제품군 (선택)<input name="usesHeavily" placeholder="airpurifier"></label>
</div>
<label class="chk"><input type="checkbox" name="approve"> 소재 검수 통과로 표시</label>
<p class="note">캠페인은 <b>일시정지</b> 상태로 만들어집니다. 검수 후 아래 목록에서 켜세요.</p>
<button type="submit">캠페인 만들기</button>
</form></section>`,
			html.EscapeString(cr.ID), html.EscapeString(cr.Format), html.EscapeString(cr.ID),
			qs(k), html.EscapeString(cr.ID), html.EscapeString(cr.Title))
	}

	b.WriteString(s.navHTML(r, k))
	b.WriteString(overviewHTML(s))
	b.WriteString(impressionNote(s))
	b.WriteString(campaignsHTML(s, k))
	b.WriteString(creativesHTML(s, k))
	fmt.Fprintf(&b, `<p class="foot">프로파일 소스 <code>%s</code></p></body></html>`, audSource(s.Aud))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func metricsFor(s *Server, campaignID string) track.Metrics {
	return track.Aggregate(s.St.Events(store.Filter{CampaignID: campaignID}))
}

func qs(k string) string {
	if k == "" {
		return ""
	}
	return "?k=" + k
}

func describeTargeting(t model.Targeting) string {
	var p []string
	for k, v := range t.DP {
		p = append(p, "기기 "+k+"="+v)
	}
	if len(t.UsesHeavily) > 0 {
		p = append(p, "잘 쓰는 "+strings.Join(t.UsesHeavily, ","))
	}
	if len(t.OwnsCategory) > 0 {
		p = append(p, "보유 "+strings.Join(t.OwnsCategory, ","))
	}
	if len(t.Sector) > 0 {
		p = append(p, "관심 "+strings.Join(t.Sector, ","))
	}
	sort.Strings(p)
	return strings.Join(p, " · ")
}

//go:embed assets/tokens.css
var tokensCSS []byte

// 디자인 시스템 토큰을 서빙한다. 콘솔 CSS 는 --color-* 만 쓰고 hex 를 쓰지 않는다.
func (s *Server) tokens(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(tokensCSS)
}

// 콘솔 셸. **자체 팔레트를 만들지 않는다** — 색은 전부 헤이홈 디자인 시스템의
// 시맨틱 토큰(--color-*)에서 온다. hex 를 여기 쓰면 다크 모드가 깨지고
// 디자인 시스템 변경을 따라가지 못한다. (assets/README.md)
const consoleHead = `<!doctype html><html lang="ko" data-theme="light"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>광고 콘솔</title>
<link rel="stylesheet" href="/console/tokens.css"><style>
*{box-sizing:border-box}
body{margin:0;padding:24px 28px 40px;background:var(--color-background-elevation-2);
  color:var(--color-contents-contents);font-family:var(--font-family-korean),var(--font-family-sans),system-ui,sans-serif}
h1{font-size:var(--font-size-heading3);margin:0 0 16px}
h2{font-size:var(--font-size-body1);margin:0 0 12px}
.card{background:var(--color-background-elevation-1);border:1px solid var(--color-divider-divider);
  border-radius:var(--rd-16);padding:18px;margin-bottom:16px}
.card.hi{border-color:var(--color-primary-primary)}
.sub{color:var(--color-contents-contents-sub);font-size:var(--font-size-caption1);margin:0 0 14px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:10px}
label{display:block;font-size:var(--font-size-caption2);color:var(--color-contents-contents-sub)}
input,select{width:100%;padding:8px 10px;margin-top:4px;border:1px solid var(--color-divider-divider);
  border-radius:var(--rd-8);font-size:var(--font-size-caption1);
  background:var(--color-background-elevation-1);color:var(--color-contents-contents)}
.chk{margin:12px 0 4px;font-size:var(--font-size-caption1)}.chk input{width:auto;margin-right:6px}
.note{font-size:var(--font-size-caption2);color:var(--color-contents-contents-sub);margin:8px 0 12px}
button{background:var(--color-primary-primary);color:var(--color-contents-contents-on);
  border:0;border-radius:var(--rd-12);padding:10px 16px;
  font-size:var(--font-size-body2);font-weight:700;cursor:pointer}
button.mini{padding:5px 10px;font-size:var(--font-size-caption2)}
button.mini.on{background:var(--color-primary-primary)}
button.mini.off{background:var(--color-button-secondary);color:var(--color-contents-contents)}
table{width:100%;border-collapse:collapse;table-layout:auto;font-size:var(--font-size-caption1)}
th{text-align:left;color:var(--color-contents-contents-sub);font-weight:600;
  font-size:var(--font-size-caption2);border-bottom:1px solid var(--color-divider-divider);padding:8px 6px}
td{padding:9px 6px;border-bottom:1px solid var(--color-divider-divider);vertical-align:middle}
td.empty{color:var(--color-contents-contents-sub);text-align:center;padding:28px;line-height:1.7}
code{background:var(--color-background-elevation-2);border-radius:var(--rd-4);padding:1px 5px;
  font-family:var(--font-family-mono);font-size:var(--font-size-caption2)}
.st{padding:2px 8px;border-radius:var(--rd-circular);font-size:var(--font-size-caption2);font-weight:700;line-height:1.6}
.st.active{background:var(--color-primary-primary-ghost);color:var(--color-primary-primary-text)}
.st.paused{background:var(--color-background-elevation-2);color:var(--color-contents-contents-sub)}
.st.rej{background:var(--color-background-danger-elevation-1);color:var(--color-individuals-danger)}
.foot{color:var(--color-contents-contents-sub);font-size:var(--font-size-caption2)}
.nav{display:flex;gap:16px;align-items:center;margin:0 0 16px;
  padding-bottom:10px;border-bottom:1px solid var(--color-divider-divider);font-size:var(--font-size-caption1)}
.nav a{color:var(--color-contents-contents-sub);text-decoration:none}
.nav a:hover{color:var(--color-contents-contents)}
.nav .spacer{flex:1}
.warnbadge{background:var(--color-background-warning-elevation-1);color:var(--color-contents-contents);
  border-radius:var(--rd-circular);padding:3px 10px;font-size:var(--font-size-caption2);font-weight:700}
.rangebar{display:flex;gap:6px;margin-bottom:12px}
.rng{font-size:var(--font-size-caption2);padding:4px 10px;border-radius:var(--rd-circular);
  text-decoration:none;background:var(--color-background-elevation-2);color:var(--color-contents-contents-sub)}
.rng.on{background:var(--color-primary-primary-ghost);color:var(--color-primary-primary-text);font-weight:700}
.attach{margin-top:14px;padding-top:14px;border-top:1px solid var(--color-divider-divider)}
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin-bottom:16px}
.tile{background:var(--color-background-elevation-1);border:1px solid var(--color-divider-divider);
  border-radius:var(--rd-16);padding:14px 16px;display:flex;flex-direction:column;gap:3px}
.t-label{font-size:var(--font-size-caption2);color:var(--color-contents-contents-sub)}
.t-value{font-size:var(--font-size-heading3);font-weight:800}
.t-sub{font-size:var(--font-size-caption2);color:var(--color-contents-contents-sub)}
.banner{background:var(--color-background-warning-elevation-1);
  border:1px solid var(--color-divider-divider);border-radius:var(--rd-12);
  padding:12px 14px;font-size:var(--font-size-caption1);line-height:1.6;margin-bottom:16px}
.dim{color:var(--color-contents-contents-sub);font-size:var(--font-size-caption2)}
.mono{font-family:var(--font-family-mono)}
th.r,td.r{text-align:right}
.pill{background:var(--color-background-elevation-2);border-radius:var(--rd-4);padding:2px 7px;
  font-size:var(--font-size-caption2)}
.mini-a{font-size:var(--font-size-caption2);color:var(--color-primary-primary);text-decoration:none}
a{color:var(--color-contents-contents)}
.crumb{font-size:var(--font-size-caption1);margin:0 0 12px}
.crumb a{color:var(--color-contents-contents-sub);text-decoration:none}
.kv{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:10px;margin-top:10px}
.kv.wide{grid-template-columns:repeat(auto-fit,minmax(120px,1fr))}
.kv>div{background:var(--color-background-elevation-2);border-radius:var(--rd-12);
  padding:10px 12px;font-size:var(--font-size-body2);font-weight:600}
.kv span{display:block;font-size:var(--font-size-caption2);color:var(--color-contents-contents-sub);
  font-weight:400;margin-bottom:3px}
.yes{color:var(--color-primary-primary);font-size:var(--font-size-caption1);font-weight:700}
.no{color:var(--color-individuals-danger);font-size:var(--font-size-caption1);font-weight:700}
</style></head><body><h1>광고 콘솔</h1>`

func (s *Server) campaignDetail(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	k := r.URL.Query().Get("k")
	var b strings.Builder
	b.WriteString(consoleHead)
	b.WriteString(s.navHTML(r, k))
	b.WriteString(s.campaignDetailFull(r.PathValue("id"), k))
	fmt.Fprintf(&b, `<p class="foot">프로파일 소스 <code>%s</code></p></body></html>`, audSource(s.Aud))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
