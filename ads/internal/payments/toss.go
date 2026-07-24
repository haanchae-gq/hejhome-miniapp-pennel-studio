package payments

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

/*
토스페이먼츠 — 가상계좌 발급 + 입금 웹훅.

⚠️ **아직 검증되지 않았다.** 시크릿 키와 계약이 있어야 실제로 부를 수 있고,
아래 엔드포인트·필드명은 문서 기억에 기반한 초안이다. 붙이기 전에 반드시
토스페이먼츠 개발자 문서로 대조하고, 테스트 키로 한 번 왕복시켜라.
그때까지 New() 는 키가 없으면 nil 을 돌려주고 Manual 이 대신 쓰인다.

	POST https://api.tosspayments.com/v1/virtual-accounts   가상계좌 발급
	Authorization: Basic base64(secretKey + ":")

웹훅(입금 통보)은 토스 콘솔에 URL 을 등록해 두면 온다. 우리 쪽 수신 경로는
/api/pg/webhook 이다.

## 검증을 어떻게 하나

토스 웹훅은 본문에 우리가 등록한 시크릿을 실어 보낸다(가상계좌 입금 통보의 `secret`).
그 값이 우리가 아는 값과 같아야 한다. **상수 시간 비교**를 쓴다 — 문자열 비교로
새는 타이밍은 작지만, 결제 검증에서 굳이 감수할 이유가 없다.

TODO(붙일 때): 토스가 서명 헤더 방식으로 바뀌었으면 그쪽을 따른다.
*/

type Toss struct {
	secretKey     string // 결제 API 시크릿
	webhookSecret string // 웹훅 본문 검증용
	base          string
	hc            *http.Client
}

// NewToss — env 가 없으면 nil. 호출자는 nil 이면 Manual 로 떨어진다.
//
//	ADS_TOSS_SECRET_KEY      결제 API 시크릿 (test_sk_… / live_sk_…)
//	ADS_TOSS_WEBHOOK_SECRET  웹훅 본문 검증값
func NewToss() *Toss {
	sk := os.Getenv("ADS_TOSS_SECRET_KEY")
	if sk == "" {
		return nil
	}
	base := os.Getenv("ADS_TOSS_API_BASE")
	if base == "" {
		base = "https://api.tosspayments.com"
	}
	return &Toss{
		secretKey:     sk,
		webhookSecret: os.Getenv("ADS_TOSS_WEBHOOK_SECRET"),
		base:          base,
		hc:            &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *Toss) Name() string { return "tosspayments" }

func (t *Toss) auth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(t.secretKey+":"))
}

// Request — 청구서 전용 가상계좌를 발급한다.
func (t *Toss) Request(ctx context.Context, orderID, orderName, customer string,
	amount int64, due time.Time) (*PaymentRequest, error) {

	body := map[string]any{
		"amount":       amount,
		"orderId":      orderID,
		"orderName":    orderName,
		"customerName": customer,
		"bank":         os.Getenv("ADS_TOSS_VA_BANK"), // 예: "우리"
	}
	if !due.IsZero() {
		body["dueDate"] = due.Format(time.RFC3339)
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", t.base+"/v1/virtual-accounts", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", t.auth())
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("토스 가상계좌 발급 실패(%d): %s", resp.StatusCode, string(raw))
	}
	var out struct {
		VirtualAccount struct {
			BankCode      string `json:"bankCode"`
			AccountNumber string `json:"accountNumber"`
			CustomerName  string `json:"customerName"`
			DueDate       string `json:"dueDate"`
		} `json:"virtualAccount"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	pr := &PaymentRequest{
		Provider: t.Name(), OrderID: orderID, Method: "virtual_account",
		Bank: out.VirtualAccount.BankCode, Account: out.VirtualAccount.AccountNumber,
		Holder: out.VirtualAccount.CustomerName,
	}
	if d, err := time.Parse(time.RFC3339, out.VirtualAccount.DueDate); err == nil {
		pr.DueDate = d
	}
	return pr, nil
}

// ParseWebhook — 입금 통보를 검증하고 해석한다.
func (t *Toss) ParseWebhook(_ context.Context, _ map[string]string, body []byte) (*PaymentResult, error) {
	var ev struct {
		EventType   string `json:"eventType"`
		Secret      string `json:"secret"`
		OrderID     string `json:"orderId"`
		Status      string `json:"status"`
		PaymentKey  string `json:"paymentKey"`
		TotalAmount int64  `json:"totalAmount"`
		ApprovedAt  string `json:"approvedAt"`
		Method      string `json:"method"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	// 검증 — 통과시키면 누구나 "입금됐다"고 주장할 수 있다.
	if t.webhookSecret == "" {
		return nil, errorsNew("웹훅 시크릿이 설정되지 않아 검증할 수 없다 — 처리를 거부한다")
	}
	if subtle.ConstantTimeCompare([]byte(ev.Secret), []byte(t.webhookSecret)) != 1 {
		return nil, errorsNew("웹훅 시크릿 불일치 — 위조 가능성")
	}
	res := &PaymentResult{
		OrderID: ev.OrderID, PaymentKey: ev.PaymentKey, Status: ev.Status,
		Amount: ev.TotalAmount, Method: ev.Method, Raw: string(body),
	}
	if ts, err := time.Parse(time.RFC3339, ev.ApprovedAt); err == nil {
		res.PaidAt = ts
	} else {
		res.PaidAt = time.Now()
	}
	return res, nil
}

func errorsNew(s string) error { return fmt.Errorf("%s", s) }
