package track

import (
	"testing"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
)

type memSink struct{ evs []model.Event }

func (m *memSink) Put(e model.Event) error { m.evs = append(m.evs, e); return nil }

var t0 = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

// 원본 식별자를 저장하지 않고, 솔트가 날마다 회전해 날짜를 넘는 추적이 안 된다.
func TestHashRotatesDaily(t *testing.T) {
	h := NewHasher("secret")
	a := h.Hash("device-123", t0)
	b := h.Hash("device-123", t0.Add(24*time.Hour))
	if a == "" || a == "device-123" {
		t.Fatal("원본이 그대로 나오면 안 된다")
	}
	if a == b {
		t.Fatal("날이 바뀌면 해시도 바뀌어야 한다(장기 추적 방지)")
	}
	if a != h.Hash("device-123", t0.Add(time.Hour)) {
		t.Fatal("같은 날 안에서는 같아야 한다(중복 판정에 필요)")
	}
	if h.Hash("", t0) != "" {
		t.Fatal("빈 id 는 빈 해시여야 한다(익명은 익명으로)")
	}
}

func TestDedupWithinWindow(t *testing.T) {
	s := &memSink{}
	tr := New(NewHasher("k"), s)
	ev := model.Event{Type: model.EvClick, DeviceHash: "d1", Slot: "s", CampaignID: "c1"}

	e1, _ := tr.Record(ev, t0)
	if !e1.Billable {
		t.Fatal("첫 클릭은 유효해야 한다")
	}
	e2, _ := tr.Record(ev, t0.Add(3*time.Second))
	if e2.Billable {
		t.Fatal("3초 만의 반복은 무효여야 한다")
	}
	if e2.Reason == "" {
		t.Fatal("무효 사유가 남아야 한다")
	}
	e3, _ := tr.Record(ev, t0.Add(DedupWindow+time.Second))
	if !e3.Billable {
		t.Fatal("창을 벗어나면 다시 유효해야 한다")
	}
	if len(s.evs) != 3 {
		t.Fatal("무효도 버리지 않고 저장해야 한다(내부 감사)")
	}
}

func TestUnknownDeviceIsNotBillable(t *testing.T) {
	tr := New(NewHasher("k"), &memSink{})
	e, _ := tr.Record(model.Event{Type: model.EvClick, Slot: "s", CampaignID: "c1"}, t0)
	if e.Billable {
		t.Fatal("기기를 모르면 중복 판정이 불가하므로 과금하면 안 된다")
	}
}

func TestNonBillableTypes(t *testing.T) {
	tr := New(NewHasher("k"), &memSink{})
	e, _ := tr.Record(model.Event{Type: model.EvLandingView, DeviceHash: "d", CampaignID: "c"}, t0)
	if e.Billable {
		t.Fatal("랜딩 조회는 과금 대상이 아니다")
	}
}

func TestAggregate(t *testing.T) {
	evs := []model.Event{
		{Type: model.EvImpression},
		{Type: model.EvImpression},
		{Type: model.EvImpression},
		{Type: model.EvImpression},
		{Type: model.EvClick, Billable: true},
		{Type: model.EvClick, Billable: false}, // 중복 — 유효 클릭에서 제외
		{Type: model.EvLandingView},
		{Type: model.EvConvert, Billable: true, Amount: 39900},
	}
	m := Aggregate(evs)
	if m.Impressions != 4 || m.Clicks != 1 || m.RawClicks != 2 {
		t.Fatalf("집계가 다르다: %+v", m)
	}
	if m.Revenue != 39900 || m.Conversions != 1 {
		t.Fatalf("전환 집계가 다르다: %+v", m)
	}
	if m.CTR != 0.25 {
		t.Fatalf("CTR 은 유효 클릭/노출 = 0.25 여야 한다: %v", m.CTR)
	}
	if m.ArrivalRate != 1 || m.ConvRate != 1 {
		t.Fatalf("도달률·전환율이 다르다: %+v", m)
	}
}
