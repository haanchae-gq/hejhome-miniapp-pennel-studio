// Package payments — 결제 수납 seam.
//
// **실제 PG 는 토스페이먼츠를 붙일 예정이다.** 지금은 사람이 입금을 확인해 기록하는
// Manual 구현만 있고, 토스는 자리(TossPayments)만 잡아 둔다 — 시크릿과 계약이 있어야
// 실제로 동작하므로, 없는 것을 있는 척하지 않는다.
//
// # 왜 가상계좌인가
//
// 광고 정산은 B2B 다. 카드 결제창을 띄워 광고주 담당자가 결제하는 흐름은 맞지 않고,
// **청구서마다 전용 가상계좌를 발급 → 광고주가 이체 → 웹훅으로 자동 대사(對査)** 가
// 한국 B2B 의 표준 경로다. 그래서 인터페이스를 그 모양으로 잡는다.
//
// # 나중에 토스를 붙일 때 반드시 지켜야 할 것 셋
//
//  1. **멱등성.** 웹훅은 중복·재시도로 여러 번 온다. 같은 결제를 두 번 기록하면
//     장부가 어긋난다. (invoiceID, PaymentKey) 로 한 번만 반영한다.
//  2. **금액 대사.** 웹훅이 말하는 입금액과 청구 금액이 다르면 **자동으로 완납
//     처리하지 않는다.** 부분입금·초과입금은 사람이 판단할 일이다.
//  3. **웹훅 검증.** 보낸 쪽이 토스가 맞는지 확인하지 않으면 누구나 "입금됐다"고
//     주장할 수 있다. 검증 실패는 조용히 무시하지 말고 남긴다.
package payments

import (
	"context"
	"errors"
	"time"
)

// ErrNotConfigured — PG 가 아직 연결되지 않았다. 오류가 아니라 상태다.
var ErrNotConfigured = errors.New("payments: PG 가 연결되지 않았다(수동 입금 확인으로 운영 중)")

// ErrAmountMismatch — 입금액이 청구액과 다르다. 자동 완납 처리하면 안 된다.
var ErrAmountMismatch = errors.New("payments: 입금액이 청구액과 다르다 — 사람이 확인해야 한다")

// PaymentRequest — 청구서에 붙일 결제 수단. 가상계좌면 계좌번호가 들어온다.
type PaymentRequest struct {
	Provider  string    `json:"provider"`
	OrderID   string    `json:"orderId"`           // 우리 쪽 주문번호 = 청구서 ID
	Method    string    `json:"method"`            // virtual_account | transfer | card
	Bank      string    `json:"bank,omitempty"`    // 가상계좌 은행
	Account   string    `json:"account,omitempty"` // 가상계좌 번호
	Holder    string    `json:"holder,omitempty"`  // 예금주
	DueDate   time.Time `json:"dueDate,omitempty"` // 입금 기한
	CheckoutU string    `json:"checkoutUrl,omitempty"`
}

// PaymentResult — 웹훅이 알려 온 결제 결과.
type PaymentResult struct {
	OrderID    string // 청구서 ID
	PaymentKey string // PG 의 결제 식별자 — **멱등성 키**
	Status     string // DONE | CANCELED | EXPIRED | PARTIAL
	Amount     int64  // 실제 입금액
	Method     string
	PaidAt     time.Time
	Raw        string // 원문(감사용)
}

// Provider — PG 구현체를 꽂는 자리.
type Provider interface {
	Name() string
	// Request 는 청구서에 결제 수단을 붙인다(가상계좌 발급 등).
	Request(ctx context.Context, orderID, orderName, customer string, amount int64, due time.Time) (*PaymentRequest, error)
	// ParseWebhook 은 웹훅을 **검증하고** 결제 결과로 해석한다.
	// 검증 실패는 반드시 오류다 — 통과시키면 누구나 입금을 주장할 수 있다.
	ParseWebhook(ctx context.Context, header map[string]string, body []byte) (*PaymentResult, error)
}

// Manual — 지금 쓰는 것. PG 없이 사람이 입금을 확인해 콘솔에 기록한다.
type Manual struct{}

func (Manual) Name() string { return "manual" }

func (Manual) Request(context.Context, string, string, string, int64, time.Time) (*PaymentRequest, error) {
	return nil, ErrNotConfigured
}

func (Manual) ParseWebhook(context.Context, map[string]string, []byte) (*PaymentResult, error) {
	return nil, ErrNotConfigured
}
