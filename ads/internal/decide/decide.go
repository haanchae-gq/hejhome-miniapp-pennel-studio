// Package decide — 광고 결정 엔진.
//
// 슬롯 + 컨텍스트를 받아 **어떤 소재를 내보낼지** 고른다. 부수효과가 없다 —
// 시간·후보·통계·프로파일을 전부 주입받는다(테스트·재현 가능).
//
// 규칙 (순서대로 거른다):
//  1. 슬롯 일치
//  2. 캠페인 활성 (status + 기간)
//  3. 소재 검수 통과
//  4. 타게팅 일치 — 제품·DP(인텐트) + 계정 프로파일
//  5. 빈도 제한
//  6. 예산 소진 (유효 클릭 기준)
//
// 남은 것 중 priority 내림차순, 동점이면 creativeID 오름차순(안정적 = 재현 가능).
// 없으면 nil → 서버는 204, 패널은 조용히 실패한다.
package decide

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/teamgoqual/hej-adserver/internal/audience"
	"github.com/teamgoqual/hej-adserver/internal/model"
)

// Ctx — 요청 컨텍스트. **패널이 준 값은 전부 힌트다**(클라이언트라 위조 가능).
// 과금·타게팅의 권위 있는 속성은 서버가 따로 확인해야 한다 — 설계서 §3.2.
type Ctx struct {
	ProductID   string
	DeviceHash  string
	AccountHash string
	DP          map[string]string
}

// Stats — 빈도·예산 판정에 필요한 최근 상태.
type Stats struct {
	ClicksToday  map[string]int // campaignID → 오늘 유효 클릭
	DeviceRecent []RecentHit    // 이 기기의 최근 노출/클릭
}

type RecentHit struct {
	CampaignID string
	TS         time.Time
}

// Result — 고른 것과, 고르는 과정에서 걸러진 사유.
// Skipped 를 버리지 않는 이유: "왜 광고가 안 나갔나"를 나중에 설명할 수 있어야 한다.
type Result struct {
	Chosen  *model.Candidate
	Skipped []Skip
}

type Skip struct {
	CreativeID string
	Reason     string
}

// ParseDP 는 "filter_life=0,mode=auto" 를 맵으로. 패널이 URL 로 실어 보내는 형식.
func ParseDP(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
		}
	}
	return out
}

func active(c model.Campaign, now time.Time) bool {
	if c.Status != "active" {
		return false
	}
	if !c.StartsAt.IsZero() && now.Before(c.StartsAt) {
		return false
	}
	if !c.EndsAt.IsZero() && now.After(c.EndsAt) {
		return false
	}
	return true
}

// MatchTargeting 은 규칙 하나를 컨텍스트·프로파일에 맞춰 본다.
//
// 프로파일이 필요한 규칙인데 프로파일을 못 얻으면 **false** 다(fail closed).
// 이유 문자열을 함께 돌려주므로 "타게팅했다고 하고 아무나에게 나가는" 사고가 안 난다.
func MatchTargeting(t model.Targeting, c Ctx, prof *audience.Profile) (bool, string) {
	if len(t.ProductID) > 0 {
		ok := false
		for _, p := range t.ProductID {
			if p == c.ProductID && p != "" {
				ok = true
				break
			}
		}
		if !ok {
			return false, "productId 불일치"
		}
	}
	for k, v := range t.DP {
		if c.DP[k] != v {
			return false, "DP 상태 불일치(" + k + ")"
		}
	}

	if !t.NeedsProfile() {
		return true, ""
	}
	if prof == nil {
		// 소스 미연결이거나 이 계정을 모른다 — 선언된 실패.
		return false, "계정 프로파일 없음(소스 미연결) — 프로파일 타게팅 불가"
	}
	for _, cat := range t.OwnsCategory {
		if !prof.Owns(cat) {
			return false, "보유 제품군 아님(" + cat + ")"
		}
	}
	for _, cat := range t.UsesHeavily {
		if !prof.UsesHeavily(cat) {
			return false, "사용 강도 미달(" + cat + ")"
		}
	}
	if len(t.Sector) > 0 {
		min := t.SectorMin
		if min <= 0 {
			min = 0.5
		}
		ok := false
		for _, s := range t.Sector {
			if prof.SectorScore(s) >= min {
				ok = true
				break
			}
		}
		if !ok {
			return false, "관심 섹터 점수 미달"
		}
	}
	return true, ""
}

// Decide 는 후보 중 하나를 고른다.
func Decide(ctx context.Context, slot string, c Ctx, cands []model.Candidate,
	st Stats, prov audience.Provider, now time.Time) Result {

	var res Result

	// 프로파일은 필요한 후보가 있을 때만 한 번 조회한다(불필요한 조회·추적을 만들지 않는다).
	var prof *audience.Profile
	needProf := false
	for _, cd := range cands {
		if cd.Placement.Slot == slot && cd.Placement.Targeting.NeedsProfile() {
			needProf = true
			break
		}
	}
	if needProf && prov != nil && c.AccountHash != "" {
		p, err := prov.Profile(ctx, c.AccountHash)
		if err == nil {
			prof = p
		} else if !errors.Is(err, audience.ErrNoProfile) {
			// 소스 장애도 "모름"과 같게 취급한다 — 광고가 안 나갈 뿐 기기 제어엔 영향 없다.
			prof = nil
		}
	}

	var ok []model.Candidate
	for _, cd := range cands {
		skip := func(r string) { res.Skipped = append(res.Skipped, Skip{cd.Creative.ID, r}) }

		if cd.Placement.Slot != slot {
			continue // 다른 슬롯은 사유를 남기지 않는다(후보 자체가 아님)
		}
		if !active(cd.Campaign, now) {
			skip("캠페인 비활성/기간 밖")
			continue
		}
		if cd.Creative.Review != model.ReviewApproved {
			skip("소재 검수 미통과")
			continue
		}
		if pass, why := MatchTargeting(cd.Placement.Targeting, c, prof); !pass {
			skip(why)
			continue
		}
		if fc := cd.Placement.FreqCap; fc != nil && c.DeviceHash != "" {
			w := fc.WindowSec
			if w <= 0 {
				w = 86400
			}
			since := now.Add(-time.Duration(w) * time.Second)
			n := 0
			for _, h := range st.DeviceRecent {
				if h.CampaignID == cd.Campaign.ID && h.TS.After(since) {
					n++
				}
			}
			max := fc.Max
			if max <= 0 {
				max = 1
			}
			if n >= max {
				skip("빈도 제한")
				continue
			}
		}
		if cap := cd.Campaign.DailyCap; cap > 0 && st.ClicksToday[cd.Campaign.ID] >= cap {
			skip("일일 예산 소진")
			continue
		}
		ok = append(ok, cd)
	}

	if len(ok) == 0 {
		return res
	}
	sort.SliceStable(ok, func(i, j int) bool {
		if ok[i].Placement.Priority != ok[j].Placement.Priority {
			return ok[i].Placement.Priority > ok[j].Placement.Priority
		}
		return ok[i].Creative.ID < ok[j].Creative.ID
	})
	res.Chosen = &ok[0]
	return res
}
