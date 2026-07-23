package audience

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"
)

// StoreProvider — **서빙 스토어**에서 프로파일을 점조회하는 Provider.
//
// 경로(설계서 §4-3, ads/analytics/README.md):
//
//	StarRocks  mv_ad_device_profile → v_ad_device_profile
//	     │ 동기화 잡 (주기 적재)
//	     ▼
//	서빙 스토어 (Valkey | Postgres)  ← 이 Provider 가 읽는 곳
//	     │ ~1ms 점조회
//	     ▼
//	광고 서버 /go
//
// **StarRocks 를 직접 조회하지 않는 이유**: MPP OLAP 은 대량 스캔·집계용이고,
// 광고는 요청당 1건 점조회 + 수 ms + QPS 라 성격이 정반대다. 파이프라인 내부가
// 바뀌어도 광고 서버가 안 흔들리도록 서빙 뷰 뒤에 한 겹을 둔다.
type StoreProvider struct {
	st  ProfileStore
	ttl time.Duration

	mu    sync.RWMutex
	cache map[string]cached
}

type cached struct {
	p  *Profile
	at time.Time
}

// ProfileStore — 서빙 스토어 뒤에 무엇이 오든 이 인터페이스만 만족하면 된다.
// Valkey 든 Postgres 든 구현체를 갈아끼운다.
type ProfileStore interface {
	Lookup(ctx context.Context, key string) (*Profile, error)
	Name() string
}

func NewStoreProvider(st ProfileStore, ttl time.Duration) *StoreProvider {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &StoreProvider{st: st, ttl: ttl, cache: map[string]cached{}}
}

func (s *StoreProvider) Name() string { return "store:" + s.st.Name() }

func (s *StoreProvider) Profile(ctx context.Context, key string) (*Profile, error) {
	if key == "" {
		return nil, ErrNoProfile
	}
	now := time.Now()
	s.mu.RLock()
	c, ok := s.cache[key]
	s.mu.RUnlock()
	if ok && now.Sub(c.at) < s.ttl {
		if c.p == nil {
			return nil, ErrNoProfile // 부정 결과도 캐시한다(없는 키로 스토어를 두드리지 않게)
		}
		return c.p, nil
	}

	p, err := s.st.Lookup(ctx, key)
	s.mu.Lock()
	s.cache[key] = cached{p: p, at: now}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNoProfile
	}
	return p, nil
}

// ── 스냅샷 스토어 ────────────────────────────────────────────────────────────

// SnapshotStore — 동기화 잡이 떨군 스냅샷을 메모리에 얹어 서빙한다.
//
// 1단계용이다. 동기화 잡이 `v_ad_device_profile` 를 JSONL 로 뽑아 주면 그대로 먹는다 —
// Valkey/Postgres 구현이 붙기 전까지 **같은 인터페이스로 미리 검증**할 수 있다.
type SnapshotStore struct {
	mu   sync.RWMutex
	m    map[string]*Profile
	name string
	at   time.Time
}

func NewSnapshotStore(name string) *SnapshotStore {
	return &SnapshotStore{m: map[string]*Profile{}, name: name}
}

func (s *SnapshotStore) Name() string { return s.name }

func (s *SnapshotStore) Lookup(_ context.Context, key string) (*Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[key], nil // 없으면 (nil, nil) → StoreProvider 가 ErrNoProfile 로 바꾼다
}

// LoadedAt 은 마지막 적재 시각. 리포트에 신선도를 드러내기 위한 것 —
// "언제 적 데이터로 타게팅했나"를 숨기지 않는다.
func (s *SnapshotStore) LoadedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.at
}

// row — v_ad_device_profile 한 줄. 컬럼명은 MV 초안과 맞춘다.
type row struct {
	DevID         string `json:"dev_id"`
	ProductKey    string `json:"product_key"`
	CategoryID    string `json:"category_id"`
	ProductKoName string `json:"product_ko_name"`
	UsageLevel    string `json:"usage_level"`
	EventCount28d int64  `json:"event_count_28d"`
	ActiveDays28d int    `json:"active_days_28d"`
	// 섹터는 아직 소스가 없다(커머스 구매 이력 확인 필요 — analytics/README §질문 3).
	// 컬럼이 생기면 여기에 더한다.
	Sectors []Sector `json:"sectors,omitempty"`
}

// LoadJSONL 은 스냅샷을 통째로 갈아끼운다(부분 갱신이 아니다 — 스냅샷은 원자적이어야 한다).
// 키는 **가명화된 값**이 들어온다고 가정한다: 동기화 잡이 dev_id 를 광고 서버와 같은
// 방식으로 해시해 내보낸다. 원본 dev_id 가 광고 서버로 넘어오지 않게 하려는 것.
func LoadJSONL(s *SnapshotStore, r io.Reader) (int, error) {
	next := map[string]*Profile{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rw row
		if err := json.Unmarshal([]byte(line), &rw); err != nil {
			return n, err
		}
		if rw.DevID == "" {
			continue
		}
		cat := rw.CategoryID
		if rw.ProductKoName != "" {
			cat = rw.CategoryID // 카테고리 표기는 dim 이 정본. 이름은 참고용.
		}
		p := &Profile{
			AccountHash: rw.DevID,
			Categories:  []string{cat},
			Usage:       map[string]UsageLevel{cat: UsageLevel(rw.UsageLevel)},
			Sectors:     rw.Sectors,
			Source:      s.name,
			FetchedAt:   time.Now(),
		}
		next[rw.DevID] = p
		n++
	}
	if err := sc.Err(); err != nil {
		return n, err
	}
	s.mu.Lock()
	s.m = next
	s.at = time.Now()
	s.mu.Unlock()
	return n, nil
}
