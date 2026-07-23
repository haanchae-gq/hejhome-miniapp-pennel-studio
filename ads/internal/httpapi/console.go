package httpapi

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
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
	return r.URL.Query().Get("k") == s
}

func (s *Server) RegisterConsole(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/creatives", s.createCreative)
	mux.HandleFunc("GET /console", s.console)
	mux.HandleFunc("POST /console/campaign", s.createCampaign)
	mux.HandleFunc("POST /console/status", s.setStatus)
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
	writeJSON(w, 200, map[string]any{
		"ok": true, "creativeId": id,
		"consoleUrl": fmt.Sprintf("%s/console?creative=%s", s.Base, id),
		"previewUrl": fmt.Sprintf("%s/l/%s", s.Base, id),
	})
}

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthorized", 401)
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
	http.Redirect(w, r, s.consoleURL(r, ""), http.StatusSeeOther)
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	_ = r.ParseForm()
	s.Adm.SetCampaignStatus(r.FormValue("campaign"), r.FormValue("status"))
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
	if !authed(r) {
		http.Error(w, "unauthorized — ?k=<ADS_ADMIN_SECRET>", 401)
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

	// 캠페인 목록
	b.WriteString(`<section class="card"><h2>캠페인</h2><table><tr>
<th>캠페인</th><th>광고주</th><th>상태</th><th>과금</th><th>슬롯</th><th>타게팅</th><th>검수</th><th>지표</th><th></th></tr>`)
	camps := s.Adm.Campaigns()
	sort.Slice(camps, func(i, j int) bool { return camps[i].ID > camps[j].ID })
	if len(camps) == 0 {
		b.WriteString(`<tr><td colspan="9" class="empty">아직 캠페인이 없습니다. 스튜디오에서 “광고로 발행”을 눌러 보세요.</td></tr>`)
	}
	for _, c := range camps {
		pl := s.Adm.PlacementsOf(c.ID)
		slot, tgt, crev := "—", "전체(비타게팅)", "—"
		if len(pl) > 0 {
			slot = pl[0].Slot
			if d := describeTargeting(pl[0].Targeting); d != "" {
				tgt = d
			}
			if cr, ok := s.Adm.Creative(pl[0].CreativeID); ok {
				crev = string(cr.Review)
			}
		}
		m := metricsFor(s, c.ID)
		next, label := "active", "켜기"
		if c.Status == "active" {
			next, label = "paused", "끄기"
		}
		fmt.Fprintf(&b, `<tr><td><code>%s</code></td><td>%s</td><td><span class="st %s">%s</span></td><td>%s</td>
<td><code>%s</code></td><td>%s</td><td>%s</td><td>클릭 %d · 전환 %d</td>
<td><form method="post" action="/console/status%s"><input type="hidden" name="campaign" value="%s">
<input type="hidden" name="status" value="%s"><button class="mini">%s</button></form></td></tr>`,
			html.EscapeString(c.ID), html.EscapeString(c.Advertiser),
			html.EscapeString(c.Status), html.EscapeString(c.Status), html.EscapeString(string(c.Pricing)),
			html.EscapeString(slot), html.EscapeString(tgt), html.EscapeString(crev),
			m.Clicks, m.Conversions, qs(k), html.EscapeString(c.ID), next, label)
	}
	b.WriteString(`</table></section>`)
	b.WriteString(fmt.Sprintf(`<p class="foot">프로파일 소스: <code>%s</code></p></body></html>`, audSource(s.Aud)))
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

const consoleHead = `<!doctype html><html lang="ko"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>광고 콘솔</title><style>
*{box-sizing:border-box}body{margin:0;padding:24px;background:#F7F8FA;color:#0F1114;
font-family:-apple-system,BlinkMacSystemFont,"Apple SD Gothic Neo","Noto Sans KR",system-ui,sans-serif}
h1{font-size:20px;margin:0 0 16px}h2{font-size:15px;margin:0 0 12px}
.card{background:#fff;border:1px solid #E5E8EB;border-radius:14px;padding:18px;margin-bottom:16px;max-width:1080px}
.card.hi{border-color:#00A872;box-shadow:0 4px 16px rgba(0,168,114,.12)}
.sub{color:#8B95A1;font-size:13px;margin:0 0 14px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:10px}
label{display:block;font-size:12px;color:#4E5968}
input,select{width:100%;padding:8px 10px;margin-top:4px;border:1px solid #E5E8EB;border-radius:8px;font-size:13px}
.chk{margin:12px 0 4px;font-size:13px}.chk input{width:auto;margin-right:6px}
.note{font-size:12px;color:#8B95A1;margin:8px 0 12px}
button{background:#00A872;color:#fff;border:0;border-radius:10px;padding:10px 16px;font-size:14px;font-weight:700;cursor:pointer}
button.mini{padding:5px 10px;font-size:12px;background:#4E5968}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;color:#8B95A1;font-weight:600;font-size:12px;border-bottom:1px solid #E5E8EB;padding:8px 6px}
td{padding:9px 6px;border-bottom:1px solid #F2F4F6;vertical-align:middle}
td.empty{color:#8B95A1;text-align:center;padding:28px}
code{background:#F2F4F6;border-radius:5px;padding:1px 5px;font-size:12px}
.st{padding:2px 8px;border-radius:99px;font-size:11px;font-weight:700}
.st.active{background:#E6F7F1;color:#007559}.st.paused{background:#F2F4F6;color:#8B95A1}
.foot{color:#8B95A1;font-size:12px;max-width:1080px}
</style></head><body><h1>광고 콘솔</h1>`
