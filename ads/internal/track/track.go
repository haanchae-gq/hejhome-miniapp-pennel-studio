// Package track — 지표 트래커.
//
// 광고 상품에서 리포트는 부가기능이 아니라 **상품의 절반**이다. 키즈노트도 D-Day 에
// "노출 수·클릭 수·클릭률을 볼 수 있는 어드민 계정"을 광고주에게 준다.
//
// 사슬 하나(impID)가 전부를 묶는다:
//
//	impression(나중) → click → landing_view → engage → lead / convert
//
// 세 가지를 지킨다:
//
//  1. **원본 식별자를 저장하지 않는다.** deviceID·accountID 는 솔트 HMAC 으로만 남고,
//     솔트는 날마다 회전해 장기 추적이 불가능하다.
//  2. **무효 클릭을 버리지 않고 표시한다.** 중복·부정 의심은 Billable=false + 사유.
//     광고주 리포트는 유효분만, 내부 감사는 전량을 본다.
//  3. **집계는 이벤트에서 파생한다.** 별도 카운터를 두지 않는다(어긋날 여지를 만들지 않는다).
package track

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
)

// Hasher — 식별자 가명화. 솔트를 날마다 회전한다.
//
// 회전의 뜻: 어제의 해시와 오늘의 해시가 달라 **날짜를 넘는 개인 추적이 안 된다.**
// 대신 빈도 제한·중복 제거도 하루 범위로 제한된다 — 의도한 맞교환이다.
type Hasher struct {
	base []byte
}

func NewHasher(secret string) *Hasher {
	if secret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		secret = hex.EncodeToString(b)
	}
	return &Hasher{base: []byte(secret)}
}

// Hash 는 (id, 날짜) → 12자 해시. id 가 비면 빈 문자열(익명은 익명으로 둔다).
func (h *Hasher) Hash(id string, now time.Time) string {
	if id == "" {
		return ""
	}
	m := hmac.New(sha256.New, h.base)
	m.Write([]byte(now.UTC().Format("2006-01-02")))
	m.Write([]byte{0})
	m.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))[:12]
}

// HashStable — **회전하지 않는** 가명화. 프로파일 조회 키 전용이다.
//
// 왜 Hash 와 나누나: 둘은 목적이 다르다.
//
//   - Hash(회전)      — 이벤트 로그용. 날이 바뀌면 값이 달라져 **광고 로그만으로는**
//     장기 행동 추적이 불가능하다. 이게 회전의 값어치다.
//   - HashStable(고정) — 프로파일 조회용. StarRocks 쪽 스냅샷의 키와 맞아야 조회가 된다.
//
// 회전 해시를 조회 키로 쓰면 **매일 자정에 전 계정의 프로파일이 미스**가 되고,
// 그날 동기화가 돌기 전까지 타게팅이 통째로 꺼진다. 그렇다고 얻는 프라이버시도 없다 —
// dev_id 는 StarRocks 가 이미 갖고 있어 광고 서버의 회전이 아무것도 가리지 못한다.
// 그래서 조회 키는 고정하고, **행동 로그 쪽만 회전**시킨다.
func (h *Hasher) HashStable(id string) string {
	if id == "" {
		return ""
	}
	m := hmac.New(sha256.New, h.base)
	m.Write([]byte("profile\x00"))
	m.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))[:16]
}

// NewImpID 는 사슬 키. 시간 접두어를 둬서 정렬·만료 판단이 쉽다.
func NewImpID(now time.Time) string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return now.UTC().Format("20060102") + "-" + base64.RawURLEncoding.EncodeToString(b)
}

// DedupWindow — 같은 (기기, 슬롯, 캠페인) 클릭이 이 시간 안에 다시 오면 무효로 본다.
// 사람이 같은 배너를 3초 만에 두 번 누르는 것은 클릭 2회가 아니다.
const DedupWindow = 30 * time.Second

// Tracker 는 이벤트를 받아 유효성 판정 후 저장소에 넘긴다.
type Tracker struct {
	h    *Hasher
	sink Sink

	mu   sync.Mutex
	seen map[string]time.Time // dedup 키 → 마지막 시각
}

// Sink — 이벤트를 어디에 쌓을지. 메모리·Postgres 구현을 갈아끼운다.
type Sink interface {
	Put(model.Event) error
}

func New(h *Hasher, sink Sink) *Tracker {
	return &Tracker{h: h, sink: sink, seen: map[string]time.Time{}}
}

func (t *Tracker) Hasher() *Hasher { return t.h }

// Record 는 이벤트 하나를 기록한다. Billable 판정은 여기서만 한다.
func (t *Tracker) Record(e model.Event, now time.Time) (model.Event, error) {
	if e.TS.IsZero() {
		e.TS = now
	}
	if e.ID == "" {
		e.ID = NewImpID(now)
	}
	e.Billable, e.Reason = t.judge(e, now)
	if err := t.sink.Put(e); err != nil {
		return e, err
	}
	return e, nil
}

// judge — 과금 대상인가. 판정을 한곳에 모아 둔다(흩어지면 정산이 어긋난다).
func (t *Tracker) judge(e model.Event, now time.Time) (bool, string) {
	// 과금은 클릭·전환에만 붙는다. 나머지는 관측용이라 애초에 과금 대상이 아니다.
	switch e.Type {
	case model.EvClick, model.EvConvert, model.EvLead:
	default:
		return false, "과금 대상 이벤트 아님"
	}
	// 기기를 모르면 중복 판정을 못 한다 → 보수적으로 무효.
	if e.DeviceHash == "" {
		return false, "기기 식별 불가 — 중복 판정 불가"
	}
	key := strings.Join([]string{string(e.Type), e.DeviceHash, e.Slot, e.CampaignID}, "|")
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.seen[key]; ok && now.Sub(last) < DedupWindow {
		return false, "중복(짧은 시간 내 반복)"
	}
	t.seen[key] = now
	t.gcLocked(now)
	return true, ""
}

func (t *Tracker) gcLocked(now time.Time) {
	if len(t.seen) < 10000 {
		return
	}
	for k, v := range t.seen {
		if now.Sub(v) > DedupWindow*4 {
			delete(t.seen, k)
		}
	}
}

// ── 집계 ────────────────────────────────────────────────────────────────────

// Metrics 는 광고주 리포트의 한 줄. 전부 이벤트에서 파생한다.
type Metrics struct {
	Impressions  int     `json:"impressions"`
	Clicks       int     `json:"clicks"`       // 유효 클릭만
	RawClicks    int     `json:"rawClicks"`    // 무효 포함 (내부 감사용)
	LandingViews int     `json:"landingViews"` // 클릭 → 랜딩 도달
	Leads        int     `json:"leads"`
	Conversions  int     `json:"conversions"`
	Revenue      int64   `json:"revenue"`
	CTR          float64 `json:"ctr"`      // 클릭/노출 — 노출이 0이면 0
	ArrivalRate  float64 `json:"arrival"`  // 랜딩도달/클릭 — 이탈 측정
	ConvRate     float64 `json:"convRate"` // 전환/클릭
}

// Aggregate 는 이벤트 목록을 지표로 접는다.
func Aggregate(evs []model.Event) Metrics {
	var m Metrics
	for _, e := range evs {
		switch e.Type {
		case model.EvImpression:
			m.Impressions++
		case model.EvClick:
			m.RawClicks++
			if e.Billable {
				m.Clicks++
			}
		case model.EvLandingView:
			m.LandingViews++
		case model.EvLead:
			if e.Billable {
				m.Leads++
			}
		case model.EvConvert:
			if e.Billable {
				m.Conversions++
				m.Revenue += e.Amount
			}
		}
	}
	if m.Impressions > 0 {
		m.CTR = float64(m.Clicks) / float64(m.Impressions)
	}
	if m.Clicks > 0 {
		m.ArrivalRate = float64(m.LandingViews) / float64(m.Clicks)
		m.ConvRate = float64(m.Conversions) / float64(m.Clicks)
	}
	return m
}
