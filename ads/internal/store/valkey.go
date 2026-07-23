package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/teamgoqual/hej-adserver/internal/audience"
)

/*
Valkey 서빙 스토어 — StarRocks 에서 동기화된 프로파일을 점조회한다.

## 왜 버전 키를 쓰나 (원자 교체)

스냅샷은 **통째로 갈려야** 한다. 키를 하나씩 덮어쓰면 교체 중간에 옛 행과 새 행이
섞여 보이고, 사라진 기기의 옛 프로파일이 영영 남는다. 그래서 세대(generation)를 둔다:

	adprof:gen            → 현재 세대 번호 N       (읽기는 항상 이걸 먼저 본다)
	adprof:N:<key>        → 프로파일 JSON
	adprof:N:_meta        → { loadedAt, count }   (신선도 노출용)

동기화 잡은 N+1 에 전부 쓴 뒤 **마지막에 gen 을 N+1 로 바꾼다.** 그 한 번의 SET 이
교체 시점이다. 이전 세대는 TTL 로 알아서 사라진다.

## 왜 TTL 을 거나

동기화 잡이 조용히 죽어도 광고는 계속 나간다 — 그때 **일주일 묵은 프로파일로
타게팅하는 것**이 가장 나쁜 실패다. TTL 이 있으면 신선도가 끊길 때 프로파일이
사라지고, 결정 엔진이 fail closed 로 넘어가 비타게팅으로 안전하게 떨어진다.
*/

// DefaultPrefix — 키스페이스 접두어.
//
// **이관을 염두에 둔 설정이다.** 개발은 전용 Valkey 를 띄워 쓰지만, 운영 Valkey 는
// APISIX 쿼터(quota-check)와 **공유**한다. 남의 키와 섞이지 않도록 접두어를 반드시
// 두고, 환경별로 갈아끼울 수 있게 env 로 뺀다(`ADS_VALKEY_PREFIX`).
const DefaultPrefix = "adprof"

const keyMeta = "_meta"

type Valkey struct {
	rdb    *redis.Client
	ttl    time.Duration
	prefix string
}

func (v *Valkey) keyGen() string               { return v.prefix + ":gen" }
func (v *Valkey) key(g int64, k string) string { return fmt.Sprintf("%s:%d:%s", v.prefix, g, k) }

type valkeyMeta struct {
	LoadedAt time.Time `json:"loadedAt"`
	Count    int       `json:"count"`
	Source   string    `json:"source"`
}

// NewValkey — addr 은 "host:port". ttl 은 프로파일 수명(권장: 동기화 주기의 3~4배).
func NewValkey(addr, password string, db int, ttl time.Duration, prefix string) *Valkey {
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	if prefix == "" {
		prefix = DefaultPrefix
	}
	return &Valkey{
		rdb:    redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}),
		ttl:    ttl,
		prefix: prefix,
	}
}

func (v *Valkey) Name() string { return "valkey" }
func (v *Valkey) Close() error { return v.rdb.Close() }

func (v *Valkey) Ping(ctx context.Context) error { return v.rdb.Ping(ctx).Err() }

func (v *Valkey) gen(ctx context.Context) (int64, error) {
	s, err := v.rdb.Get(ctx, v.keyGen()).Result()
	if err == redis.Nil {
		return 0, nil // 아직 아무것도 안 실렸다
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// Lookup 은 audience.ProfileStore 구현. 없으면 (nil, nil) → 호출자가 ErrNoProfile 로 바꾼다.
func (v *Valkey) Lookup(ctx context.Context, key string) (*audience.Profile, error) {
	g, err := v.gen(ctx)
	if err != nil {
		return nil, err
	}
	if g == 0 {
		return nil, nil
	}
	raw, err := v.rdb.Get(ctx, v.key(g, key)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p audience.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.Source = "valkey"
	return &p, nil
}

// Meta 는 지금 서빙 중인 스냅샷의 신선도. 리포트에 드러내기 위한 것 —
// "언제 적 데이터로 타게팅했나"를 숨기지 않는다.
func (v *Valkey) Meta(ctx context.Context) (time.Time, int, error) {
	g, err := v.gen(ctx)
	if err != nil || g == 0 {
		return time.Time{}, 0, err
	}
	raw, err := v.rdb.Get(ctx, v.key(g, keyMeta)).Bytes()
	if err != nil {
		return time.Time{}, 0, err
	}
	var m valkeyMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return time.Time{}, 0, err
	}
	return m.LoadedAt, m.Count, nil
}

// ── 동기화(쓰기) ────────────────────────────────────────────────────────────

// Writer 는 새 세대에 프로파일을 쌓고, Commit 에서 한 번에 교체한다.
type Writer struct {
	v      *Valkey
	nextG  int64
	pipe   redis.Pipeliner
	n      int
	source string
}

func (v *Valkey) BeginWrite(ctx context.Context, source string) (*Writer, error) {
	g, err := v.gen(ctx)
	if err != nil {
		return nil, err
	}
	return &Writer{v: v, nextG: g + 1, pipe: v.rdb.Pipeline(), source: source}, nil
}

func (w *Writer) Put(ctx context.Context, key string, p *audience.Profile) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	w.pipe.Set(ctx, w.v.key(w.nextG, key), b, w.v.ttl)
	w.n++
	if w.n%1000 == 0 {
		if _, err := w.pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Commit — 메타를 쓰고 **마지막에 gen 을 올린다.** 이 SET 하나가 교체 시점이다.
func (w *Writer) Commit(ctx context.Context) (int, error) {
	if w.n == 0 {
		// 빈 스냅샷으로 교체하면 전 계정의 타게팅이 조용히 꺼진다. 사고다 — 거부한다.
		return 0, fmt.Errorf("빈 스냅샷은 반영하지 않는다(전체 타게팅이 꺼지는 사고 방지)")
	}
	m, _ := json.Marshal(valkeyMeta{LoadedAt: time.Now(), Count: w.n, Source: w.source})
	w.pipe.Set(ctx, w.v.key(w.nextG, keyMeta), m, w.v.ttl)
	if _, err := w.pipe.Exec(ctx); err != nil {
		return 0, err
	}
	// 교체 — 이 한 줄 전까지 소비자는 옛 세대를 본다.
	if err := w.v.rdb.Set(ctx, w.v.keyGen(), w.nextG, 0).Err(); err != nil {
		return 0, err
	}
	return w.n, nil
}
