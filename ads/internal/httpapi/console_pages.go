package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

// nav — 어디에 있는지, 누구로 로그인했는지. 개발 모드면 그 사실을 배지로 드러낸다
// (로그인이 꺼진 줄 모르고 운영에 올리는 사고를 막는다).
func (s *Server) navHTML(r *http.Request, k string) string {
	var right string
	if s.OIDC == nil {
		right = `<span class="warnbadge">개발 모드 — 로그인 꺼짐</span>`
	} else if u := s.sessionUser(r); u != nil {
		right = fmt.Sprintf(`<span class="dim">%s</span> <a class="mini-a" href="/auth/logout">로그아웃</a>`, esc(u.Email))
	}
	return fmt.Sprintf(`<nav class="nav">
<a href="/console%s">캠페인</a><a href="/console/report%s">광고주 리포트</a><a href="/console/audit%s">감사 로그</a>
<span class="spacer"></span>%s</nav>`, qs(k), qs(k), qs(k), right)
}

// ── 광고주 리포트 ───────────────────────────────────────────────────────────

func (s *Server) reportPage(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	k := r.URL.Query().Get("k")
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 365 {
		days = v
	}
	since := time.Now().AddDate(0, 0, -days)

	// 광고주별로 접는다 — 광고주가 보는 단위가 캠페인이 아니라 자기 자신이기 때문.
	type row struct {
		adv   string
		camps int
		m     track.Metrics
	}
	byAdv := map[string]*row{}
	for _, c := range s.Adm.Campaigns() {
		adv := orDash(c.Advertiser)
		e := s.St.Events(store.Filter{CampaignID: c.ID, Since: since})
		m := track.Aggregate(e)
		if byAdv[adv] == nil {
			byAdv[adv] = &row{adv: adv}
		}
		x := byAdv[adv]
		x.camps++
		x.m.Impressions += m.Impressions
		x.m.Clicks += m.Clicks
		x.m.RawClicks += m.RawClicks
		x.m.LandingViews += m.LandingViews
		x.m.Leads += m.Leads
		x.m.Conversions += m.Conversions
		x.m.Revenue += m.Revenue
	}
	rows := make([]*row, 0, len(byAdv))
	for _, v := range byAdv {
		rows = append(rows, v)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].m.Clicks > rows[j].m.Clicks })

	var b strings.Builder
	b.WriteString(consoleHead)
	b.WriteString(s.navHTML(r, k))
	fmt.Fprintf(&b, `<section class="card"><h2>광고주 리포트 <span class="dim">— 최근 %d일</span></h2>
<p class="sub">기간 %s · %s</p>
<div class="rangebar">`, days, since.Format("2006-01-02"), time.Now().Format("2006-01-02"))
	for _, d := range []int{7, 30, 90} {
		cls := ""
		if d == days {
			cls = " on"
		}
		fmt.Fprintf(&b, `<a class="rng%s" href="/console/report?days=%d%s">%d일</a>`,
			cls, d, strings.TrimPrefix(qs(k), "?")+func() string {
				if k != "" {
					return ""
				}
				return ""
			}(), d)
	}
	b.WriteString(`</div>
<table><thead><tr><th>광고주</th><th class="r">캠페인</th><th class="r">클릭(유효)</th>
<th class="r">클릭(전체)</th><th class="r">랜딩 도달</th><th class="r">도달률</th>
<th class="r">리드</th><th class="r">전환</th><th class="r">전환율</th><th class="r">매출</th></tr></thead><tbody>`)
	if len(rows) == 0 {
		b.WriteString(`<tr><td colspan="10" class="empty">이 기간에 데이터가 없습니다.</td></tr>`)
	}
	for _, x := range rows {
		arr, conv := 0.0, 0.0
		if x.m.Clicks > 0 {
			arr = float64(x.m.LandingViews) / float64(x.m.Clicks)
			conv = float64(x.m.Conversions) / float64(x.m.Clicks)
		}
		fmt.Fprintf(&b, `<tr><td><b>%s</b></td><td class="r">%d</td><td class="r">%s</td><td class="r">%s</td>
<td class="r">%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%s</td>
<td class="r">%s</td><td class="r"><b>%s</b></td></tr>`,
			esc(x.adv), x.camps, num(x.m.Clicks), num(x.m.RawClicks), num(x.m.LandingViews),
			pct(arr), num(x.m.Leads), num(x.m.Conversions), pct(conv), won(x.m.Revenue))
	}
	b.WriteString(`</tbody></table>
<p class="note">클릭은 <b>유효분</b>만 과금 대상입니다. 전체와의 차이는 중복·부정 의심으로 걸러진 것이며,
캠페인 상세의 이벤트 표에서 사유를 볼 수 있습니다.</p></section>`)
	fmt.Fprintf(&b, `<p class="foot">프로파일 소스 <code>%s</code></p></body></html>`, audSource(s.Aud))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// ── 감사 로그 ───────────────────────────────────────────────────────────────

func (s *Server) auditPage(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	k := r.URL.Query().Get("k")
	var b strings.Builder
	b.WriteString(consoleHead)
	b.WriteString(s.navHTML(r, k))
	b.WriteString(`<section class="card"><h2>감사 로그 <span class="dim">— 누가·언제·무엇을 바꿨나</span></h2>
<p class="sub">광고는 돈이 오가는 일이라 “누가 이 캠페인을 켰나”에 답할 수 있어야 합니다. 이 기록은 지워지지 않습니다.</p>
<table><thead><tr><th>시각</th><th>사람</th><th>동작</th><th>대상</th><th>내용</th></tr></thead><tbody>`)
	as := s.Adm.Audits(200)
	if len(as) == 0 {
		b.WriteString(`<tr><td colspan="5" class="empty">아직 기록이 없습니다.</td></tr>`)
	}
	for _, a := range as {
		fmt.Fprintf(&b, `<tr><td class="mono">%s</td><td>%s</td><td><span class="pill">%s</span></td>
<td class="mono dim">%s</td><td>%s</td></tr>`,
			a.TS.In(time.Local).Format("01-02 15:04:05"), esc(orDash(a.Actor)),
			esc(a.Action), esc(a.Target), esc(a.Detail))
	}
	b.WriteString(`</tbody></table></section>`)
	fmt.Fprintf(&b, `<p class="foot">프로파일 소스 <code>%s</code></p></body></html>`, audSource(s.Aud))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// ── 기간 편집 · 검수 · 소재 추가 ────────────────────────────────────────────

func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s *Server) setSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.FormValue("campaign")
	for _, c := range s.Adm.Campaigns() {
		if c.ID != id {
			continue
		}
		c.StartsAt = parseDate(r.FormValue("startsAt"))
		e := parseDate(r.FormValue("endsAt"))
		if !e.IsZero() {
			e = e.Add(24*time.Hour - time.Second) // 종료일은 그날 끝까지
		}
		c.EndsAt = e
		if v, err := strconv.Atoi(r.FormValue("dailyCap")); err == nil {
			c.DailyCap = v
		}
		s.Adm.UpdateCampaign(c)
		s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "campaign.schedule", Target: id,
			Detail: fmt.Sprintf("기간 %s ~ %s · 일일한도 %s",
				orDash(r.FormValue("startsAt")), orDash(r.FormValue("endsAt")), orDash(r.FormValue("dailyCap")))})
		break
	}
	http.Redirect(w, r, "/console/campaign/"+id+qs(r.FormValue("k")), http.StatusSeeOther)
}

func (s *Server) setReview(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	id, rev := r.FormValue("creative"), model.Review(r.FormValue("review"))
	s.Adm.SetCreativeReview(id, rev)
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "creative.review", Target: id,
		Detail: "→ " + string(rev)})
	back := r.FormValue("back")
	if back == "" {
		back = "/console"
	}
	http.Redirect(w, r, back+qs(r.FormValue("k")), http.StatusSeeOther)
}

// attachCreative — 한 캠페인에 소재를 더 붙인다(A/B, 소재 교체).
func (s *Server) attachCreative(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	campID, crID, slot := r.FormValue("campaign"), r.FormValue("creative"), r.FormValue("slot")
	cr, ok := s.Adm.Creative(crID)
	if !ok {
		http.Error(w, "unknown creative", 404)
		return
	}
	cr.CampaignID = campID
	s.Adm.AddCreative(cr)
	prio, _ := strconv.Atoi(r.FormValue("priority"))
	s.Adm.AddPlacement(model.Placement{
		ID: "pl-" + crID, CampaignID: campID, CreativeID: crID, Slot: slot, Priority: prio,
	})
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "creative.attach", Target: crID,
		Detail: "캠페인 " + campID + " · 슬롯 " + slot})
	http.Redirect(w, r, "/console/campaign/"+campID+qs(r.FormValue("k")), http.StatusSeeOther)
}

// ── 캠페인 상세 (확장판) ────────────────────────────────────────────────────

func (s *Server) campaignDetailFull(id, k string) string {
	var camp *model.Campaign
	for _, c := range s.Adm.Campaigns() {
		if c.ID == id {
			cc := c
			camp = &cc
			break
		}
	}
	if camp == nil {
		return `<section class="card"><p class="empty">캠페인을 찾을 수 없습니다.</p></section>`
	}
	var b strings.Builder
	b.WriteString(campaignDetailHTML(s, id, k))

	// 기간·예산 편집
	df := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.In(time.Local).Format("2006-01-02")
	}
	fmt.Fprintf(&b, `<section class="card"><h2>기간 · 예산</h2>
<form method="post" action="/console/schedule"><input type="hidden" name="campaign" value="%s">
<input type="hidden" name="k" value="%s">
<div class="grid">
<label>시작일<input type="date" name="startsAt" value="%s"></label>
<label>종료일<input type="date" name="endsAt" value="%s"></label>
<label>일일 클릭 한도<input type="number" name="dailyCap" value="%d" title="0 = 무제한"></label>
</div><p class="note">비워 두면 제한 없음. 종료일은 그날 끝까지 집행됩니다.</p>
<button type="submit">저장</button></form></section>`,
		esc(id), esc(k), df(camp.StartsAt), df(camp.EndsAt), camp.DailyCap)

	// 이 캠페인의 소재들 + 검수 조작
	b.WriteString(`<section class="card"><h2>소재 <span class="dim">— 여러 개를 붙여 A/B 하거나 교체합니다</span></h2>
<table><thead><tr><th>제목</th><th>포맷</th><th>검수</th><th class="r">클릭</th><th class="r">전환</th><th></th></tr></thead><tbody>`)
	crs := s.Adm.CreativesOf(id)
	if len(crs) == 0 {
		b.WriteString(`<tr><td colspan="6" class="empty">붙은 소재가 없습니다.</td></tr>`)
	}
	for _, c := range crs {
		m := track.Aggregate(s.St.Events(store.Filter{CreativeID: c.ID}))
		next, label := model.ReviewApproved, "검수 통과"
		if c.Review == model.ReviewApproved {
			next, label = model.ReviewRejected, "반려"
		}
		fmt.Fprintf(&b, `<tr><td>%s<div class="dim mono">%s</div></td><td><span class="pill">%s</span></td>
<td>%s</td><td class="r">%s</td><td class="r">%s</td>
<td><div class="acts"><a class="mini-a" href="/l/%s" target="_blank">랜딩 ↗</a>
<form method="post" action="/console/review">
<input type="hidden" name="creative" value="%s"><input type="hidden" name="review" value="%s">
<input type="hidden" name="k" value="%s"><input type="hidden" name="back" value="/console/campaign/%s">
<button class="mini off">%s</button></form>
<form method="post" action="/console/creative/delete"
 onsubmit="return confirm('이 소재를 삭제할까요?\n\n노출 기록이 있으면 반려 처리되어 서빙만 멈추고 기록은 남습니다.')">
<input type="hidden" name="creative" value="%s"><input type="hidden" name="k" value="%s">
<input type="hidden" name="back" value="/console/campaign/%s">
<button class="mini danger">삭제</button></form></div></td></tr>`,
			esc(orDash(c.Title)), esc(c.ID), esc(orDash(c.Format)), reviewBadge(c.Review),
			num(m.Clicks), num(m.Conversions), esc(c.ID),
			esc(c.ID), string(next), esc(k), esc(id), label,
			esc(c.ID), esc(k), esc(id))
	}
	b.WriteString(`</tbody></table>`)

	// 미연결 소재를 이 캠페인에 붙이기
	var free []model.Creative
	for _, c := range s.Adm.Creatives() {
		if c.CampaignID == "" {
			free = append(free, c)
		}
	}
	if len(free) > 0 {
		slot := ""
		if pl := s.Adm.PlacementsOf(id); len(pl) > 0 {
			slot = pl[0].Slot
		}
		b.WriteString(`<form method="post" action="/console/attach" class="attach">
<input type="hidden" name="campaign" value="` + esc(id) + `"><input type="hidden" name="k" value="` + esc(k) + `">
<div class="grid"><label>소재 추가<select name="creative">`)
		for _, c := range free {
			fmt.Fprintf(&b, `<option value="%s">%s (%s)</option>`, esc(c.ID), esc(orDash(c.Title)), esc(c.ID))
		}
		fmt.Fprintf(&b, `</select></label><label>슬롯<input name="slot" value="%s" required></label>
<label>우선순위<input type="number" name="priority" value="5"></label></div>
<button type="submit">이 캠페인에 붙이기</button></form>`, esc(slot))
	}
	b.WriteString(`</section>`)
	return b.String()
}

// ── 반려 · 삭제 ─────────────────────────────────────────────────────────────
//
// 되돌릴 수 없는 동작이라 두 가지를 지킨다:
//  1. 브라우저 확인(confirm)을 거친다.
//  2. **이벤트가 있으면 진짜로 지우지 않는다.** 이벤트는 과금 근거다 — 지우면
//     광고주에게 청구한 근거가 사라진다. 대신 보관(archived)·반려(rejected)로
//     떨어뜨려 **서빙만 멈춘다.** 어느 쪽이 일어났는지 감사 로그에 남긴다.

func (s *Server) deleteCampaign(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.FormValue("campaign")
	hard := s.Adm.DeleteCampaign(id)
	what := "보관(이벤트가 있어 기록은 남김) — 서빙 중단"
	if hard {
		what = "삭제(이벤트 없음)"
	}
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "campaign.delete", Target: id, Detail: what})
	http.Redirect(w, r, "/console"+qs(r.FormValue("k")), http.StatusSeeOther)
}

func (s *Server) deleteCreative(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.FormValue("creative")
	hard := s.Adm.DeleteCreative(id)
	what := "반려 처리(이벤트가 있어 기록은 남김) — 서빙 중단"
	if hard {
		what = "삭제(이벤트 없음)"
	}
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "creative.delete", Target: id, Detail: what})
	back := r.FormValue("back")
	if back == "" {
		back = "/console"
	}
	http.Redirect(w, r, back+qs(r.FormValue("k")), http.StatusSeeOther)
}
