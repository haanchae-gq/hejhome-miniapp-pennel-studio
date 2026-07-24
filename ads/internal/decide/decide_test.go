package decide

import (
	"context"
	"testing"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/model"
)

var now = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

func cand(id, slot string, prio int, t model.Targeting) model.Candidate {
	return model.Candidate{
		Placement: model.Placement{ID: "p-" + id, CampaignID: "c-" + id, CreativeID: id,
			Slot: slot, Priority: prio, Targeting: t},
		Creative: model.Creative{ID: id, CampaignID: "c-" + id, Review: model.ReviewApproved},
		Campaign: model.Campaign{ID: "c-" + id, Status: "active"},
	}
}

func TestSlotAndPriority(t *testing.T) {
	cs := []model.Candidate{
		cand("a", "slot.x", 1, model.Targeting{}),
		cand("b", "slot.x", 9, model.Targeting{}),
		cand("z", "slot.other", 99, model.Targeting{}),
	}
	r := Decide(context.Background(), "slot.x", Ctx{}, cs, Stats{}, audience.Stub{}, now)
	if r.Chosen == nil || r.Chosen.Creative.ID != "b" {
		t.Fatalf("priority 높은 b 가 뽑혀야 한다: %+v", r.Chosen)
	}
}

func TestInactiveAndUnapproved(t *testing.T) {
	a := cand("a", "s", 1, model.Targeting{})
	a.Campaign.Status = "paused"
	b := cand("b", "s", 1, model.Targeting{})
	b.Creative.Review = model.ReviewPending
	r := Decide(context.Background(), "s", Ctx{}, []model.Candidate{a, b}, Stats{}, audience.Stub{}, now)
	if r.Chosen != nil {
		t.Fatalf("비활성·미검수는 나가면 안 된다")
	}
	if len(r.Skipped) != 2 {
		t.Fatalf("사유 2건이 남아야 한다: %+v", r.Skipped)
	}
}

func TestDateWindow(t *testing.T) {
	a := cand("a", "s", 1, model.Targeting{})
	a.Campaign.EndsAt = now.Add(-time.Hour) // 어제 끝남
	r := Decide(context.Background(), "s", Ctx{}, []model.Candidate{a}, Stats{}, audience.Stub{}, now)
	if r.Chosen != nil {
		t.Fatal("기간이 지난 캠페인은 나가면 안 된다")
	}
}

func TestIntentTargetingByDP(t *testing.T) {
	a := cand("a", "s", 1, model.Targeting{DP: map[string]string{"filter_life": "0"}})
	cs := []model.Candidate{a}

	// 필터 수명이 남았으면 안 나간다
	r := Decide(context.Background(), "s", Ctx{DP: map[string]string{"filter_life": "80"}}, cs, Stats{}, audience.Stub{}, now)
	if r.Chosen != nil {
		t.Fatal("DP 불일치인데 나갔다")
	}
	// 필터가 다 됐으면 나간다 — 인텐트 타게팅의 핵심
	r = Decide(context.Background(), "s", Ctx{DP: map[string]string{"filter_life": "0"}}, cs, Stats{}, audience.Stub{}, now)
	if r.Chosen == nil {
		t.Fatal("DP 일치인데 안 나갔다")
	}
}

// 프로파일 소스가 없으면 프로파일 타게팅은 **매칭되지 않아야** 한다(fail closed).
// 조용히 전체 노출로 새면 광고주에게 사기가 된다.
func TestProfileTargetingFailsClosedOnStub(t *testing.T) {
	a := cand("a", "s", 1, model.Targeting{Sector: []string{"pet"}})
	r := Decide(context.Background(), "s", Ctx{AccountHash: "acc1"},
		[]model.Candidate{a}, Stats{}, audience.Stub{}, now)
	if r.Chosen != nil {
		t.Fatal("프로파일 소스가 없는데 프로파일 타게팅이 통과했다")
	}
	if len(r.Skipped) == 0 || r.Skipped[0].Reason == "" {
		t.Fatal("사유가 남아야 한다")
	}
}

func TestProfileTargetingWithSource(t *testing.T) {
	prov := audience.Static{M: map[string]*audience.Profile{
		"acc1": {
			AccountHash: "acc1",
			Categories:  []string{"airpurifier"},
			Usage:       map[string]audience.UsageLevel{"airpurifier": audience.UsageHeavy},
			Sectors:     []audience.Sector{{Name: "pet", Score: 0.8}},
		},
	}}
	cs := []model.Candidate{
		cand("owns", "s", 3, model.Targeting{OwnsCategory: []string{"airpurifier"}}),
		cand("heavy", "s", 2, model.Targeting{UsesHeavily: []string{"airpurifier"}}),
		cand("sector", "s", 1, model.Targeting{Sector: []string{"pet"}, SectorMin: 0.7}),
		cand("miss", "s", 9, model.Targeting{Sector: []string{"baby"}, SectorMin: 0.7}),
	}
	r := Decide(context.Background(), "s", Ctx{AccountHash: "acc1"}, cs, Stats{}, prov, now)
	if r.Chosen == nil || r.Chosen.Creative.ID != "owns" {
		t.Fatalf("프로파일 매칭 중 priority 최고(owns)가 나가야 한다: %+v", r.Chosen)
	}
	// 점수 미달인 'miss' 는 priority 가 제일 높아도 걸러진다
	for _, s := range r.Skipped {
		if s.CreativeID == "miss" {
			return
		}
	}
	t.Fatal("섹터 점수 미달(miss)이 걸러지지 않았다")
}

func TestFreqCapAndBudget(t *testing.T) {
	a := cand("a", "s", 1, model.Targeting{})
	a.Placement.FreqCap = &model.FreqCap{Max: 1, WindowSec: 3600}
	st := Stats{DeviceRecent: []RecentHit{{CampaignID: "c-a", TS: now.Add(-time.Minute)}}}
	r := Decide(context.Background(), "s", Ctx{DeviceHash: "dev1"}, []model.Candidate{a}, st, audience.Stub{}, now)
	if r.Chosen != nil {
		t.Fatal("빈도 제한에 걸려야 한다")
	}

	b := cand("b", "s", 1, model.Targeting{})
	b.Campaign.DailyCap = 10
	st2 := Stats{ClicksToday: map[string]int{"c-b": 10}}
	r = Decide(context.Background(), "s", Ctx{}, []model.Candidate{b}, st2, audience.Stub{}, now)
	if r.Chosen != nil {
		t.Fatal("예산 소진이면 나가면 안 된다")
	}
}

func TestParseDP(t *testing.T) {
	got := ParseDP("filter_life=0, mode=auto ,bad")
	if got["filter_life"] != "0" || got["mode"] != "auto" || len(got) != 2 {
		t.Fatalf("파싱 결과가 다르다: %+v", got)
	}
}
