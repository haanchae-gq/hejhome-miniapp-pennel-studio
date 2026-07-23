package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamgoqual/hej-adserver/internal/model"
)

/*
Postgres 저장소 — 캠페인·소재·배치·이벤트.

## 왜 메모리로는 안 되나

이벤트가 곧 **과금 근거**다. 프로세스가 죽어 클릭·전환 기록이 사라지면 광고주에게
청구할 수도, 우리가 받은 돈을 설명할 수도 없다. 메모리 구현은 설계 검증용이고,
실제 광고를 태우기 전에 이쪽으로 갈아타야 한다.

## 스키마 원칙

  - 이벤트는 **append-only**. 무효 클릭도 지우지 않고 billable=false 로 남긴다
    (광고주 리포트는 유효분만, 내부 감사는 전량 — track 패키지와 같은 규율).
  - 집계 테이블을 따로 두지 않는다. 지표는 이벤트에서 파생한다 — 두 벌이 되면 어긋난다.
  - 타게팅은 jsonb. 규칙이 늘어나도 마이그레이션이 필요 없다.

pgx 는 사내 uiot 와 같은 스택이다.
*/

const schemaSQL = `
CREATE TABLE IF NOT EXISTS ad_campaign (
  id          text PRIMARY KEY,
  advertiser  text NOT NULL DEFAULT '',
  status      text NOT NULL DEFAULT 'paused',
  starts_at   timestamptz,
  ends_at     timestamptz,
  pricing     text NOT NULL DEFAULT 'cpc',
  daily_cap   int  NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ad_creative (
  id           text PRIMARY KEY,
  campaign_id  text NOT NULL DEFAULT '',
  format       text NOT NULL DEFAULT '',
  title        text NOT NULL DEFAULT '',
  review       text NOT NULL DEFAULT 'pending',
  landing_html text NOT NULL DEFAULT '',
  landing_url  text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ad_placement (
  id          text PRIMARY KEY,
  campaign_id text NOT NULL,
  creative_id text NOT NULL,
  slot        text NOT NULL,
  priority    int  NOT NULL DEFAULT 0,
  targeting   jsonb NOT NULL DEFAULT '{}'::jsonb,
  freq_cap    jsonb
);
CREATE INDEX IF NOT EXISTS ad_placement_slot ON ad_placement(slot);

-- append-only. 무효 클릭도 남긴다(사유와 함께).
CREATE TABLE IF NOT EXISTS ad_event (
  id          bigserial PRIMARY KEY,
  imp_id      text NOT NULL,
  type        text NOT NULL,
  campaign_id text NOT NULL DEFAULT '',
  creative_id text NOT NULL DEFAULT '',
  slot        text NOT NULL DEFAULT '',
  device_hash text NOT NULL DEFAULT '',
  product_id  text NOT NULL DEFAULT '',
  amount      bigint NOT NULL DEFAULT 0,
  billable    bool NOT NULL DEFAULT false,
  reason      text NOT NULL DEFAULT '',
  ts          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ad_event_imp      ON ad_event(imp_id);
CREATE INDEX IF NOT EXISTS ad_event_campaign ON ad_event(campaign_id, ts DESC);
CREATE INDEX IF NOT EXISTS ad_event_device   ON ad_event(device_hash, ts DESC);
`

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, schemaSQL+auditSchemaSQL); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close()       { p.pool.Close() }
func (p *Postgres) Kind() string { return "postgres" }

// ── 서빙 경로 (Store) ───────────────────────────────────────────────────────

func (p *Postgres) Candidates(slot string) []model.Candidate {
	ctx := context.Background()
	rows, err := p.pool.Query(ctx, `
	  SELECT pl.id, pl.campaign_id, pl.creative_id, pl.slot, pl.priority, pl.targeting, pl.freq_cap,
	         cr.format, cr.title, cr.review, cr.landing_html, cr.landing_url,
	         ca.advertiser, ca.status, ca.starts_at, ca.ends_at, ca.pricing, ca.daily_cap
	  FROM ad_placement pl
	  JOIN ad_creative cr ON cr.id = pl.creative_id
	  JOIN ad_campaign ca ON ca.id = pl.campaign_id
	  WHERE pl.slot = $1`, slot)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []model.Candidate
	for rows.Next() {
		var c model.Candidate
		var tgt []byte
		var fc *[]byte
		var starts, ends *time.Time
		if err := rows.Scan(&c.Placement.ID, &c.Placement.CampaignID, &c.Placement.CreativeID,
			&c.Placement.Slot, &c.Placement.Priority, &tgt, &fc,
			&c.Creative.Format, &c.Creative.Title, &c.Creative.Review,
			&c.Creative.LandingHTML, &c.Creative.LandingURL,
			&c.Campaign.Advertiser, &c.Campaign.Status, &starts, &ends,
			&c.Campaign.Pricing, &c.Campaign.DailyCap); err != nil {
			continue
		}
		_ = json.Unmarshal(tgt, &c.Placement.Targeting)
		if fc != nil && len(*fc) > 0 {
			var f model.FreqCap
			if json.Unmarshal(*fc, &f) == nil {
				c.Placement.FreqCap = &f
			}
		}
		if starts != nil {
			c.Campaign.StartsAt = *starts
		}
		if ends != nil {
			c.Campaign.EndsAt = *ends
		}
		c.Creative.ID = c.Placement.CreativeID
		c.Creative.CampaignID = c.Placement.CampaignID
		c.Campaign.ID = c.Placement.CampaignID
		out = append(out, c)
	}
	return out
}

func (p *Postgres) Creative(id string) (model.Creative, bool) {
	var c model.Creative
	err := p.pool.QueryRow(context.Background(),
		`SELECT id, campaign_id, format, title, review, landing_html, landing_url
		 FROM ad_creative WHERE id=$1`, id).
		Scan(&c.ID, &c.CampaignID, &c.Format, &c.Title, &c.Review, &c.LandingHTML, &c.LandingURL)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return c, false
	}
	return c, true
}

func (p *Postgres) Put(e model.Event) error {
	_, err := p.pool.Exec(context.Background(), `
	  INSERT INTO ad_event(imp_id,type,campaign_id,creative_id,slot,device_hash,product_id,amount,billable,reason,ts)
	  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ImpID, string(e.Type), e.CampaignID, e.CreativeID, e.Slot,
		e.DeviceHash, e.ProductID, e.Amount, e.Billable, e.Reason, e.TS)
	return err
}

func (p *Postgres) Events(f Filter) []model.Event {
	q := `SELECT imp_id,type,campaign_id,creative_id,slot,device_hash,product_id,amount,billable,reason,ts
	      FROM ad_event WHERE ($1='' OR campaign_id=$1) AND ($2='' OR creative_id=$2)
	        AND ($3='' OR slot=$3)
	        AND ($4::timestamptz IS NULL OR ts >= $4)
	        AND ($5::timestamptz IS NULL OR ts <= $5)
	      ORDER BY ts`
	var since, until *time.Time
	if !f.Since.IsZero() {
		since = &f.Since
	}
	if !f.Until.IsZero() {
		until = &f.Until
	}
	rows, err := p.pool.Query(context.Background(), q, f.CampaignID, f.CreativeID, f.Slot, since, until)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ImpID, &e.Type, &e.CampaignID, &e.CreativeID, &e.Slot,
			&e.DeviceHash, &e.ProductID, &e.Amount, &e.Billable, &e.Reason, &e.TS); err == nil {
			out = append(out, e)
		}
	}
	return out
}

func (p *Postgres) Stats(deviceHash string, now time.Time) (map[string]int, []time.Time, []string) {
	ctx := context.Background()
	clicks := map[string]int{}
	// 오늘 유효 클릭 — 예산 판정용.
	if rows, err := p.pool.Query(ctx, `
	  SELECT campaign_id, count(*) FROM ad_event
	  WHERE type='click' AND billable AND ts >= date_trunc('day', $1::timestamptz)
	  GROUP BY campaign_id`, now.UTC()); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int
			if rows.Scan(&id, &n) == nil {
				clicks[id] = n
			}
		}
	}
	var ts []time.Time
	var camps []string
	if deviceHash != "" {
		// 빈도 제한 판정용 — 최근 것만 본다(창이 하루라 넉넉히 이틀).
		if rows, err := p.pool.Query(ctx, `
		  SELECT campaign_id, ts FROM ad_event
		  WHERE device_hash=$1 AND ts >= $2 ORDER BY ts DESC LIMIT 500`,
			deviceHash, now.Add(-48*time.Hour)); err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				var t time.Time
				if rows.Scan(&id, &t) == nil {
					camps = append(camps, id)
					ts = append(ts, t)
				}
			}
		}
	}
	return clicks, ts, camps
}

// ── 관리 경로 (Admin) ───────────────────────────────────────────────────────

func (p *Postgres) AddCampaign(c model.Campaign) {
	var starts, ends *time.Time
	if !c.StartsAt.IsZero() {
		starts = &c.StartsAt
	}
	if !c.EndsAt.IsZero() {
		ends = &c.EndsAt
	}
	_, _ = p.pool.Exec(context.Background(), `
	  INSERT INTO ad_campaign(id,advertiser,status,starts_at,ends_at,pricing,daily_cap)
	  VALUES($1,$2,$3,$4,$5,$6,$7)
	  ON CONFLICT (id) DO UPDATE SET advertiser=$2,status=$3,starts_at=$4,ends_at=$5,pricing=$6,daily_cap=$7`,
		c.ID, c.Advertiser, c.Status, starts, ends, string(c.Pricing), c.DailyCap)
}

func (p *Postgres) AddCreative(c model.Creative) {
	_, _ = p.pool.Exec(context.Background(), `
	  INSERT INTO ad_creative(id,campaign_id,format,title,review,landing_html,landing_url)
	  VALUES($1,$2,$3,$4,$5,$6,$7)
	  ON CONFLICT (id) DO UPDATE SET campaign_id=$2,format=$3,title=$4,review=$5,landing_html=$6,landing_url=$7`,
		c.ID, c.CampaignID, c.Format, c.Title, string(c.Review), c.LandingHTML, c.LandingURL)
}

func (p *Postgres) AddPlacement(pl model.Placement) {
	tgt, _ := json.Marshal(pl.Targeting)
	var fc []byte
	if pl.FreqCap != nil {
		fc, _ = json.Marshal(pl.FreqCap)
	}
	_, _ = p.pool.Exec(context.Background(), `
	  INSERT INTO ad_placement(id,campaign_id,creative_id,slot,priority,targeting,freq_cap)
	  VALUES($1,$2,$3,$4,$5,$6,$7)
	  ON CONFLICT (id) DO UPDATE SET campaign_id=$2,creative_id=$3,slot=$4,priority=$5,targeting=$6,freq_cap=$7`,
		pl.ID, pl.CampaignID, pl.CreativeID, pl.Slot, pl.Priority, tgt, fc)
}

func (p *Postgres) Campaigns() []model.Campaign {
	rows, err := p.pool.Query(context.Background(), `
	  SELECT id,advertiser,status,starts_at,ends_at,pricing,daily_cap FROM ad_campaign ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Campaign
	for rows.Next() {
		var c model.Campaign
		var starts, ends *time.Time
		if rows.Scan(&c.ID, &c.Advertiser, &c.Status, &starts, &ends, &c.Pricing, &c.DailyCap) == nil {
			if starts != nil {
				c.StartsAt = *starts
			}
			if ends != nil {
				c.EndsAt = *ends
			}
			out = append(out, c)
		}
	}
	return out
}

func (p *Postgres) PlacementsOf(campaignID string) []model.Placement {
	rows, err := p.pool.Query(context.Background(), `
	  SELECT id,campaign_id,creative_id,slot,priority,targeting FROM ad_placement WHERE campaign_id=$1`, campaignID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Placement
	for rows.Next() {
		var pl model.Placement
		var tgt []byte
		if rows.Scan(&pl.ID, &pl.CampaignID, &pl.CreativeID, &pl.Slot, &pl.Priority, &tgt) == nil {
			_ = json.Unmarshal(tgt, &pl.Targeting)
			out = append(out, pl)
		}
	}
	return out
}

func (p *Postgres) SetCampaignStatus(campaignID, status string) {
	_, _ = p.pool.Exec(context.Background(), `UPDATE ad_campaign SET status=$2 WHERE id=$1`, campaignID, status)
}

// Creatives 는 콘솔의 소재 목록용. 랜딩 본문은 무거우니 뺀다.
func (p *Postgres) Creatives() []model.Creative {
	rows, err := p.pool.Query(context.Background(), `
	  SELECT id,campaign_id,format,title,review FROM ad_creative ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Creative
	for rows.Next() {
		var c model.Creative
		if rows.Scan(&c.ID, &c.CampaignID, &c.Format, &c.Title, &c.Review) == nil {
			out = append(out, c)
		}
	}
	return out
}

// ── 감사 로그 · 다중 소재 (Postgres) ────────────────────────────────────────

const auditSchemaSQL = `
CREATE TABLE IF NOT EXISTS ad_audit (
  id     bigserial PRIMARY KEY,
  actor  text NOT NULL DEFAULT '',
  action text NOT NULL,
  target text NOT NULL DEFAULT '',
  detail text NOT NULL DEFAULT '',
  ts     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ad_audit_ts ON ad_audit(ts DESC);
`

func (p *Postgres) CreativesOf(campaignID string) []model.Creative {
	rows, err := p.pool.Query(context.Background(), `
	  SELECT id,campaign_id,format,title,review FROM ad_creative WHERE campaign_id=$1 ORDER BY created_at`, campaignID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Creative
	for rows.Next() {
		var c model.Creative
		if rows.Scan(&c.ID, &c.CampaignID, &c.Format, &c.Title, &c.Review) == nil {
			out = append(out, c)
		}
	}
	return out
}

func (p *Postgres) Audit(a model.Audit) {
	if a.TS.IsZero() {
		a.TS = time.Now()
	}
	_, _ = p.pool.Exec(context.Background(),
		`INSERT INTO ad_audit(actor,action,target,detail,ts) VALUES($1,$2,$3,$4,$5)`,
		a.Actor, a.Action, a.Target, a.Detail, a.TS)
}

func (p *Postgres) Audits(limit int) []model.Audit {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.pool.Query(context.Background(),
		`SELECT id,actor,action,target,detail,ts FROM ad_audit ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Audit
	for rows.Next() {
		var a model.Audit
		if rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Target, &a.Detail, &a.TS) == nil {
			out = append(out, a)
		}
	}
	return out
}

func (p *Postgres) UpdateCampaign(c model.Campaign) { p.AddCampaign(c) }

func (p *Postgres) SetCreativeReview(id string, r model.Review) {
	_, _ = p.pool.Exec(context.Background(), `UPDATE ad_creative SET review=$2 WHERE id=$1`, id, string(r))
}

func (p *Postgres) countEvents(campaignID, creativeID string) int {
	var n int
	_ = p.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ad_event WHERE ($1<>'' AND campaign_id=$1) OR ($2<>'' AND creative_id=$2)`,
		campaignID, creativeID).Scan(&n)
	return n
}

// DeleteCampaign — 이벤트가 있으면 보관(archived)으로. 과금 근거를 지우지 않는다.
func (p *Postgres) DeleteCampaign(id string) bool {
	ctx := context.Background()
	if p.countEvents(id, "") > 0 {
		_, _ = p.pool.Exec(ctx, `UPDATE ad_campaign SET status='archived' WHERE id=$1`, id)
		return false
	}
	_, _ = p.pool.Exec(ctx, `DELETE FROM ad_placement WHERE campaign_id=$1`, id)
	_, _ = p.pool.Exec(ctx, `UPDATE ad_creative SET campaign_id='' WHERE campaign_id=$1`, id)
	_, _ = p.pool.Exec(ctx, `DELETE FROM ad_campaign WHERE id=$1`, id)
	return true
}

func (p *Postgres) DeleteCreative(id string) bool {
	ctx := context.Background()
	if p.countEvents("", id) > 0 {
		_, _ = p.pool.Exec(ctx, `UPDATE ad_creative SET review='rejected' WHERE id=$1`, id)
		return false
	}
	_, _ = p.pool.Exec(ctx, `DELETE FROM ad_placement WHERE creative_id=$1`, id)
	_, _ = p.pool.Exec(ctx, `DELETE FROM ad_creative WHERE id=$1`, id)
	return true
}
