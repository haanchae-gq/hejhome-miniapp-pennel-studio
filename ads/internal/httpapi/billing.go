package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
	"github.com/teamgoqual/hej-adserver/internal/payments"
	"github.com/teamgoqual/hej-adserver/internal/store"
	"github.com/teamgoqual/hej-adserver/internal/track"
)

/*
정산 — 청구서 발행부터 입금 확인까지.

## 규율 셋

 1. **발행하면 숫자를 얼린다.** 청구서는 이벤트에서 매번 다시 계산하지 않는다.
    나중에 캠페인이 보관되거나 단가가 바뀌면 이미 보낸 청구서 금액이 조용히
    달라진다 — 광고주가 받은 종이와 우리 화면이 영원히 같아야 한다.
 2. **입금은 멱등하게 기록한다.** PG 웹훅은 재시도로 여러 번 온다.
    (청구서, PaymentKey) 조합으로 한 번만 반영한다.
 3. **금액이 다르면 자동 완납하지 않는다.** 부분·초과 입금은 사람이 판단한다.

## 하지 않는 것

세금계산서 발행(홈택스·벤더)은 여기 없다. 실제 수납도 PG 몫이고, 이 콘솔은
"입금됐다"를 **기록**할 뿐이다.
*/

const vatRate = 10 // %

// billableQty — 과금 방식별 수량. 이벤트에서 뽑는다(발행 시점 1회).
func billableQty(evs []model.Event, pricing model.Pricing, from, to time.Time) int {
	m := track.Aggregate(evs)
	switch pricing {
	case model.CPC:
		return m.Clicks // 유효 클릭만
	case model.CPA:
		return m.Conversions + m.Leads
	case model.CPT:
		d := int(to.Sub(from).Hours()/24) + 1
		if d < 0 {
			d = 0
		}
		return d
	}
	return 0
}

// buildLines — 광고주의 기간 내 청구 줄을 만든다. **미리보기와 발행이 같은 함수를 쓴다**
// (다르면 보고 승인한 금액과 실제 청구가 갈린다).
func (s *Server) buildLines(adv string, from, to time.Time) ([]model.InvoiceLine, int64) {
	var lines []model.InvoiceLine
	var subtotal int64
	for _, c := range s.Adm.Campaigns() {
		if c.Advertiser != adv {
			continue
		}
		evs := s.St.Events(store.Filter{CampaignID: c.ID, Since: from, Until: to})
		qty := billableQty(evs, c.Pricing, from, to)
		if qty == 0 || c.UnitPrice == 0 {
			continue
		}
		amt := int64(qty) * c.UnitPrice
		lines = append(lines, model.InvoiceLine{
			CampaignID: c.ID, Advertiser: adv, Pricing: c.Pricing,
			Qty: qty, UnitPrice: c.UnitPrice, Amount: amt,
		})
		subtotal += amt
	}
	return lines, subtotal
}

func monthRange(v string) (time.Time, time.Time) {
	t, err := time.ParseInLocation("2006-01", v, time.Local)
	if err != nil {
		n := time.Now()
		t = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.Local)
	}
	return t, t.AddDate(0, 1, 0).Add(-time.Second)
}

// ── 화면 ────────────────────────────────────────────────────────────────────

func (s *Server) billingPage(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	k := r.URL.Query().Get("k")
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	from, to := monthRange(month)

	var b strings.Builder
	b.WriteString(consoleHead)
	b.WriteString(s.navHTML(r, k))

	// 미수금 요약
	var billed, paid, out int64
	for _, i := range s.Bill.Invoices() {
		if i.Status == "void" {
			continue
		}
		billed += i.Total
		paid += i.PaidAmount
		out += i.Outstanding()
	}
	fmt.Fprintf(&b, `<section class="tiles">
<div class="tile"><span class="t-label">청구 합계</span><span class="t-value">%s</span><span class="t-sub">부가세 포함</span></div>
<div class="tile"><span class="t-label">입금</span><span class="t-value">%s</span><span class="t-sub"></span></div>
<div class="tile"><span class="t-label">미수금</span><span class="t-value">%s</span><span class="t-sub">%s</span></div>
</section>`, won(billed), won(paid), won(out),
		map[bool]string{true: "없음", false: "확인 필요"}[out == 0])

	// 이번 달 발행 대상 미리보기
	fmt.Fprintf(&b, `<section class="card"><h2>청구서 만들기</h2>
<form method="get" action="/console/billing" class="rangebar">
<input type="hidden" name="k" value="%s">
<input type="month" name="month" value="%s" style="width:auto">
<button class="mini off" type="submit">기간 보기</button></form>
<p class="sub">%s ~ %s · 유효 클릭·전환만 셉니다. 단가가 0인 캠페인은 빠집니다.</p>
<table><thead><tr><th>광고주</th><th class="r">청구 줄</th><th class="r">공급가액</th>
<th class="r">부가세</th><th class="r">합계</th><th></th></tr></thead><tbody>`,
		esc(k), esc(month), from.Format("2006-01-02"), to.Format("2006-01-02"))

	advs := map[string]bool{}
	for _, c := range s.Adm.Campaigns() {
		if c.Advertiser != "" {
			advs[c.Advertiser] = true
		}
	}
	any := false
	for adv := range advs {
		lines, sub := s.buildLines(adv, from, to)
		if len(lines) == 0 {
			continue
		}
		any = true
		vat := sub * vatRate / 100
		// 이미 발행한 기간이면 버튼 대신 그 청구서로 보낸다 — 눌러 봐야 409 다.
		action := fmt.Sprintf(`<form method="post" action="/console/invoice/issue"
 onsubmit="return confirm('%s · %s 청구서를 발행할까요?\n\n발행하면 금액이 고정되어 이후 집계가 바뀌어도 변하지 않습니다.')">
<input type="hidden" name="advertiser" value="%s"><input type="hidden" name="month" value="%s">
<input type="hidden" name="k" value="%s"><button class="mini">청구서 발행</button></form>`,
			esc(adv), esc(month), esc(adv), esc(month), esc(k))
		if prev := s.existingInvoice(adv, from); prev != "" {
			action = fmt.Sprintf(`<span class="dim">발행됨</span> <a class="mini-a" href="/console/invoice/%s%s">%s →</a>`,
				esc(prev), qs(k), esc(prev))
		}
		fmt.Fprintf(&b, `<tr><td><b>%s</b></td><td class="r">%d</td><td class="r">%s</td>
<td class="r">%s</td><td class="r"><b>%s</b></td><td>%s</td></tr>`,
			esc(adv), len(lines), won(sub), won(vat), won(sub+vat), action)
	}
	if !any {
		b.WriteString(`<tr><td colspan="6" class="empty">이 기간에 청구할 것이 없습니다.<br>
<span class="dim">캠페인 상세에서 <b>단가</b>를 넣어야 청구 대상이 됩니다.</span></td></tr>`)
	}
	b.WriteString(`</tbody></table></section>`)

	// 발행된 청구서
	b.WriteString(`<section class="card"><h2>청구서</h2>
<table><thead><tr><th>번호</th><th>광고주</th><th>기간</th><th>상태</th>
<th class="r">합계</th><th class="r">입금</th><th class="r">미수</th><th></th></tr></thead><tbody>`)
	invs := s.Bill.Invoices()
	if len(invs) == 0 {
		b.WriteString(`<tr><td colspan="8" class="empty">발행된 청구서가 없습니다.</td></tr>`)
	}
	for _, i := range invs {
		fmt.Fprintf(&b, `<tr><td class="mono"><a href="/console/invoice/%s%s"><b>%s</b></a></td>
<td>%s</td><td class="dim">%s ~ %s</td><td>%s</td>
<td class="r">%s</td><td class="r">%s</td><td class="r">%s</td>
<td><a class="mini-a" href="/console/invoice/%s%s">열기 →</a></td></tr>`,
			esc(i.ID), qs(k), esc(i.ID), esc(i.Advertiser),
			i.PeriodStart.Format("01-02"), i.PeriodEnd.Format("01-02"),
			invoiceBadge(i), won(i.Total), won(i.PaidAmount), won(i.Outstanding()),
			esc(i.ID), qs(k))
	}
	b.WriteString(`</tbody></table></section>`)
	fmt.Fprintf(&b, `<p class="foot">수납 경로 <code>%s</code></p></body></html>`, s.pgName())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func invoiceBadge(i model.Invoice) string {
	switch {
	case i.Status == "void":
		return `<span class="st rej">취소</span>`
	case i.Outstanding() <= 0 && i.PaidAmount > 0:
		return `<span class="st active">입금 완료</span>`
	case i.PaidAmount > 0:
		return `<span class="st paused">부분 입금</span>`
	default:
		return `<span class="st paused">미수</span>`
	}
}

func (s *Server) pgName() string {
	if s.PG == nil {
		return "manual"
	}
	return s.PG.Name()
}

// issueInvoice — 발행. 이 순간의 숫자를 얼린다.
func (s *Server) issueInvoice(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	adv, month := r.FormValue("advertiser"), r.FormValue("month")
	from, to := monthRange(month)

	// 같은 광고주·같은 기간을 두 번 청구하지 않는다.
	if prev := s.existingInvoice(adv, from); prev != "" {
		http.Error(w, "이미 이 기간의 청구서가 있습니다: "+prev, 409)
		return
	}

	lines, sub := s.buildLines(adv, from, to)
	if len(lines) == 0 {
		http.Error(w, "청구할 내역이 없습니다", 400)
		return
	}
	vat := sub * vatRate / 100
	inv := model.Invoice{
		ID:         "INV-" + from.Format("200601") + "-" + strconv.FormatInt(time.Now().Unix()%100000, 36),
		Advertiser: adv, PeriodStart: from, PeriodEnd: to,
		IssuedAt: time.Now(), Status: "issued",
		Lines: lines, Subtotal: sub, VAT: vat, Total: sub + vat,
		IssuedBy: s.actor(r),
	}

	// PG 가 붙어 있으면 결제 수단(가상계좌)을 함께 발급한다.
	if s.PG != nil {
		pr, err := s.PG.Request(r.Context(), inv.ID,
			fmt.Sprintf("%s 광고비 %s", adv, from.Format("2006-01")), adv, inv.Total,
			time.Now().AddDate(0, 0, 14))
		if err == nil && pr != nil {
			inv.PGProvider, inv.PGBank, inv.PGAccount = pr.Provider, pr.Bank, pr.Account
		} else if err != nil && !errors.Is(err, payments.ErrNotConfigured) {
			// 발급 실패로 청구서 자체를 막지는 않는다 — 수동 입금으로 받을 수 있다.
			inv.Note = "결제 수단 발급 실패: " + err.Error()
		}
	}

	s.Bill.PutInvoice(inv)
	s.Adm.Audit(model.Audit{Actor: s.actor(r), Action: "invoice.issue", Target: inv.ID,
		Detail: fmt.Sprintf("%s · %s · 합계 %s (동결)", adv, month, won(inv.Total))})
	http.Redirect(w, r, "/console/invoice/"+inv.ID+qs(r.FormValue("k")), http.StatusSeeOther)
}

func (s *Server) invoicePage(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	k := r.URL.Query().Get("k")
	inv, ok := s.Bill.Invoice(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var b strings.Builder
	b.WriteString(consoleHead)
	b.WriteString(s.navHTML(r, k))
	fmt.Fprintf(&b, `<p class="crumb"><a href="/console/billing%s">← 정산</a></p>
<section class="card"><h2>%s <span class="dim mono">%s</span></h2>
<div class="kv">
<div><span>기간</span>%s ~ %s</div>
<div><span>발행</span>%s</div>
<div><span>상태</span>%s</div>
<div><span>발행자</span>%s</div></div>`,
		qs(k), esc(inv.Advertiser), esc(inv.ID),
		inv.PeriodStart.Format("2006-01-02"), inv.PeriodEnd.Format("2006-01-02"),
		inv.IssuedAt.In(time.Local).Format("2006-01-02 15:04"), invoiceBadge(inv), esc(orDash(inv.IssuedBy)))

	b.WriteString(`<table style="margin-top:16px"><thead><tr><th>캠페인</th><th>과금</th>
<th class="r">수량</th><th class="r">단가</th><th class="r">금액</th></tr></thead><tbody>`)
	for _, l := range inv.Lines {
		unit := map[model.Pricing]string{model.CPC: "클릭", model.CPA: "전환", model.CPT: "일"}[l.Pricing]
		fmt.Fprintf(&b, `<tr><td class="mono">%s</td><td><span class="pill">%s</span></td>
<td class="r">%s %s</td><td class="r">%s</td><td class="r"><b>%s</b></td></tr>`,
			esc(l.CampaignID), esc(string(l.Pricing)), num(l.Qty), esc(unit),
			won(l.UnitPrice), won(l.Amount))
	}
	fmt.Fprintf(&b, `</tbody></table>
<div class="kv wide" style="margin-top:16px">
<div><span>공급가액</span>%s</div><div><span>부가세(%d%%)</span>%s</div>
<div><span>합계</span>%s</div><div><span>입금</span>%s</div>
<div><span>미수금</span>%s</div></div>
<p class="note">이 금액은 <b>발행 시점에 고정</b>되었습니다. 이후 집계가 바뀌어도 청구서는 변하지 않습니다.</p>
</section>`, won(inv.Subtotal), vatRate, won(inv.VAT), won(inv.Total),
		won(inv.PaidAmount), won(inv.Outstanding()))

	// 결제 수단
	if inv.PGAccount != "" {
		fmt.Fprintf(&b, `<section class="card"><h2>입금 계좌 <span class="dim">— %s 발급</span></h2>
<div class="kv"><div><span>은행</span>%s</div><div><span>계좌번호</span><span class="mono">%s</span></div></div>
<p class="note">입금되면 PG 웹훅으로 자동 반영됩니다.</p></section>`,
			esc(inv.PGProvider), esc(inv.PGBank), esc(inv.PGAccount))
	} else {
		fmt.Fprintf(&b, `<section class="card"><h2>입금 확인</h2>
<p class="sub">PG(<code>%s</code>)가 연결되지 않아 <b>수동 확인</b>으로 운영 중입니다.
토스페이먼츠를 붙이면 청구서마다 가상계좌가 발급되고 입금이 자동 반영됩니다.</p>`, s.pgName())
	}

	if inv.Outstanding() > 0 && inv.Status != "void" {
		fmt.Fprintf(&b, `<form method="post" action="/console/invoice/pay">
<input type="hidden" name="invoice" value="%s"><input type="hidden" name="k" value="%s">
<div class="grid">
<label>입금액<input type="number" name="amount" value="%d" required></label>
<label>수단<select name="method"><option value="계좌이체">계좌이체</option>
<option value="가상계좌">가상계좌</option><option value="카드">카드</option></select></label>
<label>입금일<input type="date" name="paidAt" value="%s"></label>
</div><button type="submit">입금 기록</button></form>`,
			esc(inv.ID), esc(k), inv.Outstanding(), time.Now().Format("2006-01-02"))
	}
	b.WriteString(`</section>`)
	fmt.Fprintf(&b, `<p class="foot">수납 경로 <code>%s</code></p></body></html>`, s.pgName())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// recordPayment — 수동 입금 기록. 웹훅 경로와 같은 함수를 거쳐 규칙이 갈리지 않게 한다.
func (s *Server) recordPayment(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.FormValue("invoice")
	amt, _ := strconv.ParseInt(r.FormValue("amount"), 10, 64)
	at := parseDate(r.FormValue("paidAt"))
	if at.IsZero() {
		at = time.Now()
	}
	if err := s.applyPayment(id, amt, r.FormValue("method"), "", at, s.actor(r)); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	http.Redirect(w, r, "/console/invoice/"+id+qs(r.FormValue("k")), http.StatusSeeOther)
}

// applyPayment — 입금 반영의 **유일한 통로**. 수동·웹훅이 같은 규칙을 탄다.
//
// 멱등성: paymentKey 가 이미 기록된 것과 같으면 아무것도 하지 않는다.
func (s *Server) applyPayment(invoiceID string, amount int64, method, paymentKey string,
	at time.Time, actor string) error {

	inv, ok := s.Bill.Invoice(invoiceID)
	if !ok {
		return fmt.Errorf("청구서를 찾을 수 없습니다: %s", invoiceID)
	}
	if paymentKey != "" && inv.PGPaymentKey == paymentKey {
		return nil // 이미 반영됨(웹훅 재시도)
	}
	if amount <= 0 {
		return fmt.Errorf("입금액이 0 이하입니다")
	}
	inv.PaidAmount += amount
	inv.PaidAt = at
	inv.PaidMethod = method
	if paymentKey != "" {
		inv.PGPaymentKey = paymentKey
	}
	if inv.Outstanding() <= 0 {
		inv.Status = "paid"
	}
	s.Bill.PutInvoice(inv)

	detail := fmt.Sprintf("%s 입금(%s) · 미수 %s", won(amount), method, won(inv.Outstanding()))
	if inv.Outstanding() > 0 {
		detail += " — 부분 입금"
	} else if inv.PaidAmount > inv.Total {
		detail += " — **초과 입금** 확인 필요"
	}
	s.Adm.Audit(model.Audit{Actor: actor, Action: "invoice.payment", Target: invoiceID, Detail: detail})
	return nil
}

// pgWebhook — PG 입금 통보. 검증은 Provider 가 한다.
func (s *Server) pgWebhook(w http.ResponseWriter, r *http.Request) {
	if s.PG == nil {
		http.Error(w, "PG 미연결", 404)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	hdr := map[string]string{}
	for k := range r.Header {
		hdr[k] = r.Header.Get(k)
	}
	res, err := s.PG.ParseWebhook(context.Background(), hdr, body)
	if err != nil {
		// 검증 실패를 조용히 넘기지 않는다 — 위조 시도일 수 있다.
		s.Adm.Audit(model.Audit{Actor: "pg", Action: "invoice.webhook.reject", Target: "",
			Detail: err.Error()})
		http.Error(w, "rejected", 400)
		return
	}
	if res.Status != "DONE" {
		writeJSON(w, 200, map[string]any{"ok": true, "ignored": res.Status})
		return
	}
	inv, ok := s.Bill.Invoice(res.OrderID)
	if !ok {
		http.Error(w, "unknown invoice", 404)
		return
	}
	// 금액이 다르면 자동 완납하지 않는다 — 기록만 하고 사람이 판단한다.
	if res.Amount != inv.Total {
		s.Adm.Audit(model.Audit{Actor: "pg", Action: "invoice.payment.mismatch", Target: inv.ID,
			Detail: fmt.Sprintf("입금 %s · 청구 %s — 자동 완납하지 않음", won(res.Amount), won(inv.Total))})
	}
	if err := s.applyPayment(res.OrderID, res.Amount, res.Method, res.PaymentKey, res.PaidAt, "pg"); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// existingInvoice — 같은 광고주·같은 달에 이미 발행된 청구서 번호. 없으면 빈 문자열.
func (s *Server) existingInvoice(adv string, from time.Time) string {
	for _, i := range s.Bill.Invoices() {
		if i.Advertiser == adv && i.Status != "void" &&
			i.PeriodStart.Format("2006-01") == from.Format("2006-01") {
			return i.ID
		}
	}
	return ""
}
