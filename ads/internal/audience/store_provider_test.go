package audience

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const sample = `
{"dev_id":"h1","product_key":"pid-air","category_id":"airpurifier","usage_level":"heavy","event_count_28d":420,"active_days_28d":26}
{"dev_id":"h2","product_key":"pid-light","category_id":"lighting","usage_level":"light","event_count_28d":30,"active_days_28d":6}
`

func TestSnapshotLoadAndLookup(t *testing.T) {
	st := NewSnapshotStore("starrocks-snapshot")
	n, err := LoadJSONL(st, strings.NewReader(sample))
	if err != nil || n != 2 {
		t.Fatalf("적재 실패: n=%d err=%v", n, err)
	}
	p := NewStoreProvider(st, time.Minute)

	got, err := p.Profile(context.Background(), "h1")
	if err != nil {
		t.Fatalf("h1 을 찾아야 한다: %v", err)
	}
	if !got.Owns("airpurifier") || !got.UsesHeavily("airpurifier") {
		t.Fatalf("보유·사용강도가 반영되어야 한다: %+v", got)
	}

	got2, _ := p.Profile(context.Background(), "h2")
	if got2.UsesHeavily("lighting") {
		t.Fatal("light 인데 heavy 로 잡혔다")
	}
}

// 모르는 키는 ErrNoProfile — 이걸 받으면 결정 엔진이 fail closed 한다.
func TestUnknownKeyIsNoProfile(t *testing.T) {
	st := NewSnapshotStore("s")
	_, _ = LoadJSONL(st, strings.NewReader(sample))
	p := NewStoreProvider(st, time.Minute)
	if _, err := p.Profile(context.Background(), "없는키"); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("ErrNoProfile 이어야 한다: %v", err)
	}
	if _, err := p.Profile(context.Background(), ""); !errors.Is(err, ErrNoProfile) {
		t.Fatal("빈 키도 ErrNoProfile")
	}
}

// 스냅샷은 원자적으로 갈린다 — 부분 갱신으로 옛 행이 남으면 안 된다.
func TestSnapshotIsAtomicReplace(t *testing.T) {
	st := NewSnapshotStore("s")
	_, _ = LoadJSONL(st, strings.NewReader(sample))
	_, _ = LoadJSONL(st, strings.NewReader(`{"dev_id":"h3","category_id":"plug","usage_level":"heavy"}`))

	p := NewStoreProvider(st, 0) // ttl 0 → 기본 5분이지만 캐시가 비어 있으므로 스토어를 본다
	if _, err := p.Profile(context.Background(), "h3"); err != nil {
		t.Fatal("새 스냅샷의 h3 이 보여야 한다")
	}
	fresh := NewStoreProvider(st, time.Minute)
	if _, err := fresh.Profile(context.Background(), "h1"); !errors.Is(err, ErrNoProfile) {
		t.Fatal("옛 스냅샷의 h1 은 사라져야 한다")
	}
}

func TestLoadedAtIsExposed(t *testing.T) {
	st := NewSnapshotStore("s")
	if !st.LoadedAt().IsZero() {
		t.Fatal("적재 전에는 zero")
	}
	_, _ = LoadJSONL(st, strings.NewReader(sample))
	if st.LoadedAt().IsZero() {
		t.Fatal("적재 후 신선도가 드러나야 한다 — 언제 적 데이터로 타게팅했는지 숨기지 않는다")
	}
}

func TestProviderNameShowsSource(t *testing.T) {
	p := NewStoreProvider(NewSnapshotStore("starrocks-snapshot"), time.Minute)
	if p.Name() != "store:starrocks-snapshot" {
		t.Fatalf("리포트에 소스가 드러나야 한다: %s", p.Name())
	}
}
