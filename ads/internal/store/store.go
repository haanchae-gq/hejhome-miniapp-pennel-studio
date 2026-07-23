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
