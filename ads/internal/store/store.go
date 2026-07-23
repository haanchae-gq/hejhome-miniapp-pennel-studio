// Package store — 캠페인·소재·배치·이벤트 저장소.
//
// 1단계는 **메모리 구현**만 둔다. 인터페이스를 먼저 고정해 두면 Postgres 구현(pgx,
// uiot 와 같은 스택)을 나중에 갈아끼울 때 서버 코드를 안 건드린다.
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/model"
)

type Store interface {
	// Candidates 는 슬롯에 걸린 후보를 전부 준다. 거르는 것은 decide 의 몫이다.
	Candidates(slot string) []model.Candidate
	Creative(id string) (model.Creative, bool)
	Put(model.Event) error
	// Events 는 필터에 맞는 이벤트. 리포트가 쓴다.
	Events(f Filter) []model.Event
	// Stats 는 빈도·예산 판정용 최근 상태.
	Stats(deviceHash string, now time.Time) (map[string]int, []time.Time, []string)
}

type Filter struct {
	CampaignID string
	CreativeID string
	Slot       string
	Since      time.Time
	Until      time.Time
}

// Mem — 메모리 저장소. 프로세스가 죽으면 사라진다(1단계 검증용).
type Mem struct {
	mu         sync.RWMutex
	campaigns  map[string]model.Campaign
	creatives  map[string]model.Creative
	placements []model.Placement
	events     []model.Event
	audits     []model.Audit
}

func NewMem() *Mem {
	return &Mem{campaigns: map[string]model.Campaign{}, creatives: map[string]model.Creative{}}
}

func (m *Mem) AddCampaign(c model.Campaign) { m.mu.Lock(); m.campaigns[c.ID] = c; m.mu.Unlock() }
func (m *Mem) AddCreative(c model.Creative) { m.mu.Lock(); m.creatives[c.ID] = c; m.mu.Unlock() }
func (m *Mem) AddPlacement(p model.Placement) {
	m.mu.Lock()
	m.placements = append(m.placements, p)
	m.mu.Unlock()
}

func (m *Mem) Candidates(slot string) []model.Candidate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Candidate
	for _, p := range m.placements {
		if p.Slot != slot {
			continue
		}
		cr, ok := m.creatives[p.CreativeID]
		if !ok {
			continue
		}
		ca, ok := m.campaigns[p.CampaignID]
		if !ok {
			continue
		}
		out = append(out, model.Candidate{Placement: p, Creative: cr, Campaign: ca})
	}
	return out
}

func (m *Mem) Creative(id string) (model.Creative, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.creatives[id]
	return c, ok
}

func (m *Mem) Put(e model.Event) error {
	m.mu.Lock()
	m.events = append(m.events, e)
	m.mu.Unlock()
	return nil
}

func (m *Mem) Events(f Filter) []model.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Event
	for _, e := range m.events {
		if f.CampaignID != "" && e.CampaignID != f.CampaignID {
			continue
		}
		if f.CreativeID != "" && e.CreativeID != f.CreativeID {
			continue
		}
		if f.Slot != "" && e.Slot != f.Slot {
			continue
		}
		if !f.Since.IsZero() && e.TS.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && e.TS.After(f.Until) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}

// Stats — 오늘 캠페인별 유효 클릭 수, 이 기기의 최근 시각들, 그 캠페인 ID들.
func (m *Mem) Stats(deviceHash string, now time.Time) (map[string]int, []time.Time, []string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	day := now.UTC().Format("2006-01-02")
	clicks := map[string]int{}
	var ts []time.Time
	var camps []string
	for _, e := range m.events {
		if e.Type == model.EvClick && e.Billable && e.TS.UTC().Format("2006-01-02") == day {
			clicks[e.CampaignID]++
		}
		if deviceHash != "" && e.DeviceHash == deviceHash {
			ts = append(ts, e.TS)
			camps = append(camps, e.CampaignID)
		}
	}
	return clicks, ts, camps
}

// ── 콘솔(관리)용 인터페이스 ─────────────────────────────────────────────────
//
// 읽기 전용 서빙 경로(Store)와 나눠 둔다. 광고를 내보내는 코드가 실수로 캠페인을
// 고치는 일이 없도록, 관리 기능은 별도 인터페이스로만 닿는다.
type Admin interface {
	AddCampaign(model.Campaign)
	AddCreative(model.Creative)
	AddPlacement(model.Placement)
	Creative(id string) (model.Creative, bool)
	Campaigns() []model.Campaign
	PlacementsOf(campaignID string) []model.Placement
	SetCampaignStatus(campaignID, status string)
	Creatives() []model.Creative
	CreativesOf(campaignID string) []model.Creative
	Audit(a model.Audit)
	Audits(limit int) []model.Audit
	UpdateCampaign(c model.Campaign)
	SetCreativeReview(creativeID string, r model.Review)
	// DeleteCampaign/DeleteCreative 는 **이벤트가 없을 때만 진짜로 지운다.**
	// 이벤트는 과금 근거라, 있는 것을 지우면 광고주에게 청구한 근거가 사라진다.
	// 있으면 보관(archive)으로 떨어뜨리고 hard=false 를 돌려준다 — 서빙은 어느 쪽이든 멈춘다.
	DeleteCampaign(id string) (hard bool)
	DeleteCreative(id string) (hard bool)
}

func (m *Mem) Campaigns() []model.Campaign {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Campaign, 0, len(m.campaigns))
	for _, c := range m.campaigns {
		out = append(out, c)
	}
	return out
}

func (m *Mem) PlacementsOf(campaignID string) []model.Placement {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Placement
	for _, p := range m.placements {
		if p.CampaignID == campaignID {
			out = append(out, p)
		}
	}
	return out
}

func (m *Mem) SetCampaignStatus(campaignID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.campaigns[campaignID]; ok {
		c.Status = status
		m.campaigns[campaignID] = c
	}
}

func (m *Mem) Creatives() []model.Creative {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Creative, 0, len(m.creatives))
	for _, c := range m.creatives {
		c.LandingHTML = "" // 목록엔 본문을 싣지 않는다
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (m *Mem) CreativesOf(campaignID string) []model.Creative {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Creative
	for _, c := range m.creatives {
		if c.CampaignID == campaignID {
			c.LandingHTML = ""
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Mem) Audit(a model.Audit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = int64(len(m.audits) + 1)
	if a.TS.IsZero() {
		a.TS = time.Now()
	}
	m.audits = append(m.audits, a)
}

func (m *Mem) Audits(limit int) []model.Audit {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := append([]model.Audit(nil), m.audits...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *Mem) UpdateCampaign(c model.Campaign) { m.AddCampaign(c) }

func (m *Mem) SetCreativeReview(id string, r model.Review) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.creatives[id]; ok {
		c.Review = r
		m.creatives[id] = c
	}
}

func (m *Mem) hasEventsLocked(campaignID, creativeID string) bool {
	for _, e := range m.events {
		if (campaignID != "" && e.CampaignID == campaignID) || (creativeID != "" && e.CreativeID == creativeID) {
			return true
		}
	}
	return false
}

func (m *Mem) DeleteCampaign(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasEventsLocked(id, "") {
		if c, ok := m.campaigns[id]; ok {
			c.Status = "archived" // 서빙은 멈추고 기록은 남는다
			m.campaigns[id] = c
		}
		return false
	}
	delete(m.campaigns, id)
	kept := m.placements[:0]
	for _, p := range m.placements {
		if p.CampaignID != id {
			kept = append(kept, p)
		}
	}
	m.placements = kept
	return true
}

func (m *Mem) DeleteCreative(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasEventsLocked("", id) {
		if c, ok := m.creatives[id]; ok {
			c.Review = model.ReviewRejected // 반려 = 더 이상 안 나간다
			m.creatives[id] = c
		}
		return false
	}
	delete(m.creatives, id)
	kept := m.placements[:0]
	for _, p := range m.placements {
		if p.CreativeID != id {
			kept = append(kept, p)
		}
	}
	m.placements = kept
	return true
}
