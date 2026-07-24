package httpapi

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

/*
콘솔 화면 조각.

지금은 **눈으로 보고 판단하기 위한** 수준까지만 키운다 — 개요 지표, 캠페인 표,
소재 목록, 캠페인 상세(지표·스킵 사유). 화면을 보고 무엇이 더 필요한지 정한 뒤
3단계에서 운영 콘솔로 정식화한다.

의도적으로 안 넣은 것: 로그인(개발용 시크릿), 기간 편집 UI, 정산, 감사 로그.
*/

func esc(s string) string { return html.EscapeString(s) }

func num(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func pct(f float64) string {
	if f == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", f*100)
}

func won(n int64) string {
	if n == 0 {
		return "—"
	}
	return num(int(n)) + "원"
}

// 개요 — 전체 지표 한 줄.
func overviewHTML(s *Server) string {
	m := track.Aggregate(s.St.Events(store.Filter{}))
	live, total := 0, 0
	for _, c := range s.Adm.Campaigns() {
		total++
		if c.Status == "active" {
			live++
		}
	}
	tiles := []struct{ label, value, sub string }{
		{"진행 중 캠페인", fmt.Sprintf("%d", live), fmt.Sprintf("전체 %d", total)},
		{"클릭", num(m.Clicks), fmt.Sprintf("무효 포함 %s", num(m.RawClicks))},
		{"랜딩 도달", num(m.LandingViews), "도달률 " + pct(m.ArrivalRate)},
		{"전환", num(m.Conversions), "전환율 " + pct(m.ConvRate)},
		{"매출", won(m.Revenue), ""},
	}
	var b strings.Builder
	b.WriteString(`<section class="tiles">`)
	for _, t := range tiles {
		fmt.Fprintf(&b, `<div class="tile"><span class="t-label">%s</span>
<span class="t-value">%s</span><span class="t-sub">%s</span></div>`,
			esc(t.label), esc(t.value), esc(t.sub))
	}
	b.WriteString(`</section>`)
	return b.String()
}

// 노출이 없으면 CTR 은 의미가 없다 — 숨기지 말고 왜인지 알려준다.
func impressionNote(s *Server) string {
	m := track.Aggregate(s.St.Events(store.Filter{}))
	if m.Impressions > 0 {
		return ""
	}
	return `<div class="banner">
<b>노출(impression) 집계가 없습니다.</b> 패널이 “광고를 그렸다”를 알려주지 않기 때문입니다 —
지금 구조에서는 <b>클릭부터</b> 세어집니다. 그래서 CTR 은 계산되지 않고,
<b>클릭당·성과당 과금(CPC·CPA)만</b> 유효합니다. 노출당 과금(CPM)은 패널이 신호를 보내야 가능합니다.
</div>`
}

func campaignsHTML(s *Server, k string) string {
	camps := s.Adm.Campaigns()
	sort.Slice(camps, func(i, j int) bool { return camps[i].ID > camps[j].ID })

	var b strings.Builder
	b.WriteString(`<section class="card"><h2>캠페인</h2>
<table><thead><tr><th>광고주</th><th>상태</th><th>과금</th><th>슬롯</th><th>타게팅</th>
<th>검수</th><th class="r">클릭</th><th class="r">전환</th><th class="r">매출</th><th></th></tr></thead><tbody>`)
	if len(camps) == 0 {
		b.WriteString(`<tr><td colspan="10" class="empty">아직 캠페인이 없습니다.<br>
<span class="dim">패널 스튜디오에서 광고를 만들고 “광고로 발행”을 누르면 여기로 옵니다.</span></td></tr>`)
	}
	for _, c := range camps {
		pl := s.Adm.PlacementsOf(c.ID)
		slot, tgt, rev := "—", `<span class="dim">전체(비타게팅)</span>`, "—"
		if len(pl) > 0 {
			slot = pl[0].Slot
			if d := describeTargeting(pl[0].Targeting); d != "" {
				tgt = esc(d)
			}
			if cr, ok := s.Adm.Creative(pl[0].CreativeID); ok {
				rev = reviewBadge(cr.Review)
			}
		}
		m := track.Aggregate(s.St.Events(store.Filter{CampaignID: c.ID}))
		next, label, cls := "active", "켜기", "on"
		switch c.Status {
		case "active":
			next, label, cls = "paused", "끄기", "off"
		case "archived":
			// 보관에서 곧바로 라이브로 되돌리지 않는다 — 일시정지까지만 복구하고
			// 켜는 것은 사람이 한 번 더 판단한다.
			next, label, cls = "paused", "복구", "off"
		}
		fmt.Fprintf(&b, `<tr>
<td><a href="/console/campaign/%s%s"><b>%s</b></a><div class="dim mono">%s</div></td>
<td>%s</td><td><span class="pill">%s</span></td><td class="mono">%s</td><td>%s</td><td>%s</td>
<td class="r">%s</td><td class="r">%s</td><td class="r">%s</td>
<td><div class="acts">
<form method="post" action="/console/status"><input type="hidden" name="campaign" value="%s">
<input type="hidden" name="status" value="%s"><input type="hidden" name="k" value="%s">
<button class="mini %s">%s</button></form>
<form method="post" action="/console/campaign/delete"
 onsubmit="return confirm('캠페인 「%s」 을(를) 삭제할까요?\n\n집행 기록(클릭·전환)이 있으면 과금 근거이므로 지우지 않고 보관 처리되며, 서빙만 멈춥니다.')">
<input type="hidden" name="campaign" value="%s"><input type="hidden" name="k" value="%s">
<button class="mini danger">삭제</button></form></div></td></tr>`,
			esc(c.ID), qs(k), esc(orDash(c.Advertiser)), esc(c.ID),
			statusBadge(c.Status), esc(string(c.Pricing)), esc(slot), tgt, rev,
			num(m.Clicks), num(m.Conversions), won(m.Revenue),
			esc(c.ID), next, esc(k), cls, label,
			esc(orDash(c.Advertiser)), esc(c.ID), esc(k))
	}
	b.WriteString(`</tbody></table></section>`)
	return b.String()
}

func creativesHTML(s *Server, k string) string {
	crs := s.Adm.Creatives()
	var b strings.Builder
	b.WriteString(`<section class="card"><h2>소재</h2>
<table><thead><tr><th>제목</th><th>포맷</th><th>검수</th><th>캠페인</th><th></th></tr></thead><tbody>`)
	if len(crs) == 0 {
		b.WriteString(`<tr><td colspan="5" class="empty">발행된 소재가 없습니다.</td></tr>`)
	}
	for _, c := range crs {
		camp := `<span class="dim">미연결</span>`
		if c.CampaignID != "" {
			camp = `<span class="mono">` + esc(c.CampaignID) + `</span>`
		}
		fmt.Fprintf(&b, `<tr><td>%s<div class="dim mono">%s</div></td><td><span class="pill">%s</span></td>
<td>%s</td><td>%s</td><td><div class="acts">
<a class="mini-a" href="/l/%s" target="_blank">랜딩 ↗</a>
<form method="post" action="/console/review"><input type="hidden" name="creative" value="%s">
<input type="hidden" name="review" value="%s"><input type="hidden" name="k" value="%s">
<button class="mini off">%s</button></form>
<form method="post" action="/console/creative/delete"
 onsubmit="return confirm('소재 「%s」 을(를) 삭제할까요?\n\n노출 기록이 있으면 반려 처리되어 서빙만 멈추고 기록은 남습니다.')">
<input type="hidden" name="creative" value="%s"><input type="hidden" name="k" value="%s">
<button class="mini danger">삭제</button></form></div></td></tr>`,
			esc(orDash(c.Title)), esc(c.ID), esc(orDash(c.Format)),
			reviewBadge(c.Review), camp, esc(c.ID),
			esc(c.ID), string(nextReview(c.Review)), esc(k), reviewActionLabel(c.Review),
			esc(orDash(c.Title)), esc(c.ID), esc(k))
	}
	b.WriteString(`</tbody></table></section>`)
	return b.String()
}

// 캠페인 상세 — 지표 + 최근 이벤트. "왜 이 광고가 안 나갔나"를 볼 수 있어야 한다.
func campaignDetailHTML(s *Server, id, k string) string {
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
	evs := s.St.Events(store.Filter{CampaignID: id})
	m := track.Aggregate(evs)

	var b strings.Builder
	fmt.Fprintf(&b, `<p class="crumb"><a href="/console%s">← 캠페인 목록</a></p>
<section class="card"><h2>%s <span class="dim mono">%s</span></h2>
<div class="kv">
<div><span>상태</span>%s</div><div><span>과금</span>%s</div>
<div><span>일일 클릭 한도</span>%s</div></div>`,
		qs(k), esc(orDash(camp.Advertiser)), esc(camp.ID),
		statusBadge(camp.Status), esc(string(camp.Pricing)),
		esc(map[bool]string{true: "무제한", false: num(camp.DailyCap)}[camp.DailyCap == 0]))

	for _, pl := range s.Adm.PlacementsOf(id) {
		tgt := describeTargeting(pl.Targeting)
		if tgt == "" {
			tgt = "전체(비타게팅)"
		}
		fmt.Fprintf(&b, `<div class="kv"><div><span>슬롯</span><code>%s</code></div>
<div><span>우선순위</span>%d</div><div><span>타게팅</span>%s</div></div>`,
			esc(pl.Slot), pl.Priority, esc(tgt))
	}
	b.WriteString(`</section>`)

	fmt.Fprintf(&b, `<section class="card"><h2>지표</h2><div class="kv wide">
<div><span>클릭(유효)</span>%s</div><div><span>클릭(무효 포함)</span>%s</div>
<div><span>랜딩 도달</span>%s</div><div><span>도달률</span>%s</div>
<div><span>리드</span>%s</div><div><span>전환</span>%s</div>
<div><span>전환율</span>%s</div><div><span>매출</span>%s</div></div></section>`,
		num(m.Clicks), num(m.RawClicks), num(m.LandingViews), pct(m.ArrivalRate),
		num(m.Leads), num(m.Conversions), pct(m.ConvRate), won(m.Revenue))

	// 최근 이벤트 — 무효 사유가 그대로 보인다(감사).
	b.WriteString(`<section class="card"><h2>최근 이벤트 <span class="dim">— 무효도 숨기지 않습니다</span></h2>
<table><thead><tr><th>시각</th><th>유형</th><th>과금</th><th>사유</th><th>금액</th></tr></thead><tbody>`)
	n := 0
	for i := len(evs) - 1; i >= 0 && n < 30; i-- {
		e := evs[i]
		n++
		bill := `<span class="no">무효</span>`
		if e.Billable {
			bill = `<span class="yes">유효</span>`
		}
		fmt.Fprintf(&b, `<tr><td class="mono">%s</td><td>%s</td><td>%s</td><td class="dim">%s</td><td class="r">%s</td></tr>`,
			e.TS.In(time.Local).Format("01-02 15:04:05"), esc(string(e.Type)), bill,
			esc(orDash(e.Reason)), won(e.Amount))
	}
	if n == 0 {
		b.WriteString(`<tr><td colspan="5" class="empty">아직 이벤트가 없습니다.</td></tr>`)
	}
	b.WriteString(`</tbody></table></section>`)
	return b.String()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// 보관(archived)은 일시정지와 다르다. 섞어 보이면 "잠깐 멈춘 것"으로 오해해
// 다시 켜려 하게 된다 — 보관은 삭제 요청의 결과이고 기록만 남긴 상태다.
func statusBadge(s string) string {
	switch s {
	case "active":
		return `<span class="st active">진행 중</span>`
	case "archived":
		return `<span class="st rej">보관됨</span>`
	default:
		return `<span class="st paused">일시정지</span>`
	}
}

func reviewBadge(r model.Review) string {
	switch r {
	case model.ReviewApproved:
		return `<span class="st active">통과</span>`
	case model.ReviewRejected:
		return `<span class="st rej">반려</span>`
	default:
		return `<span class="st paused">검수 대기</span>`
	}
}

// 다음 검수 상태와 버튼 문구. 통과된 것은 "반려", 그 밖은 "통과"로 넘긴다.
func nextReview(r model.Review) model.Review {
	if r == model.ReviewApproved {
		return model.ReviewRejected
	}
	return model.ReviewApproved
}

func reviewActionLabel(r model.Review) string {
	if r == model.ReviewApproved {
		return "반려"
	}
	return "통과"
}
