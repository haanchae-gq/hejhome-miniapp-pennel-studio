// Package audience — 계정 프로파일 신호.
//
// **IoT 플랫폼의 진짜 무기가 여기다.** 이 계정이 어떤 제품을 갖고 있고, 그중 무엇을
// 실제로 **잘 쓰며**, 어떤 **섹터에 관심**이 있는지. 키즈노트가 "자녀 월령"으로 추정하는
// 자리를, 우리는 실사용 데이터로 **확정**할 수 있다.
//
// 다만 **소스가 아직 정해지지 않았다.** Cube 런타임 API 인지, 데이터웨어하우스인지,
// 별도 집계 배치인지 미정이다. 그래서 이 패키지는 **자리(seam)만** 만든다:
//
//	Provider 인터페이스 — 나중에 진짜 소스를 꽂는다
//	Stub          — 지금. "모른다"를 정직하게 돌려준다
//
// 규율 하나: **모르면 매칭하지 않는다(fail closed).** 프로파일을 요구하는 타게팅인데
// 프로파일이 없으면 광고를 내보내지 않고 사유를 남긴다. 조용히 전체 노출로 새면
// 광고주에게는 "타게팅했다"고 말하면서 실제로는 아무나에게 나가는 사기가 된다.
package audience

import (
	"context"
	"errors"
	"strings"
	"time"
)

// UsageLevel — 제품군을 얼마나 쓰는가. 소스가 정해지면 임계값도 그때 정한다.
type UsageLevel string

const (
	UsageNone  UsageLevel = "none"
	UsageLight UsageLevel = "light"
	UsageHeavy UsageLevel = "heavy"
)

// Sector — 관심 섹터와 점수(0~1). 어떻게 산출할지는 소스 확정 시 정의한다.
type Sector struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// Profile 은 한 계정의 광고 관련 신호. AccountHash 만 들고 다닌다 — 원본 계정 식별자는
// 이 패키지 밖으로 나가지 않는다.
type Profile struct {
	AccountHash string                `json:"accountHash"`
	Categories  []string              `json:"categories"` // 보유 제품군
	Usage       map[string]UsageLevel `json:"usage"`      // 제품군 → 사용 강도
	Sectors     []Sector              `json:"sectors"`    // 관심 섹터
	Source      string                `json:"source"`     // stub | cube | dw | …
	FetchedAt   time.Time             `json:"fetchedAt"`
}

// ErrNoProfile — 프로파일을 모른다. 오류가 아니라 **상태**다. 호출자는 이걸 받으면
// 프로파일 기반 타게팅을 포기해야 한다(비타게팅 광고는 그대로 나간다).
var ErrNoProfile = errors.New("audience: 계정 프로파일 소스가 아직 연결되지 않았다")

// Provider — 진짜 소스를 꽂는 자리. 구현체는 나중에 추가한다.
//
//	CubeProvider  — Cube OpenAPI 로 기기 목록·사용 이력
//	DWProvider    — 데이터웨어하우스 집계 결과
//	CachedProvider— 위를 감싸는 캐시 (프로파일은 자주 안 바뀐다)
type Provider interface {
	// Profile 은 계정 해시로 프로파일을 찾는다. 모르면 (nil, ErrNoProfile).
	Profile(ctx context.Context, accountHash string) (*Profile, error)
	// Name 은 리포트·로그에 남길 소스 이름.
	Name() string
}

// Stub — 지금 쓰는 것. 항상 "모른다"를 돌려준다.
// 이걸 쓰는 동안 프로파일 타게팅은 전부 실패하며, 그 사실이 리포트에 드러난다.
type Stub struct{}

func (Stub) Name() string { return "stub" }
func (Stub) Profile(context.Context, string) (*Profile, error) {
	return nil, ErrNoProfile
}

// Static — 테스트·데모용. 미리 넣어 둔 프로파일을 돌려준다.
// 소스가 붙기 전에 타게팅 규칙을 검증하는 용도이지 운영용이 아니다.
type Static struct{ M map[string]*Profile }

func (Static) Name() string { return "static" }
func (s Static) Profile(_ context.Context, hash string) (*Profile, error) {
	if p, ok := s.M[hash]; ok {
		return p, nil
	}
	return nil, ErrNoProfile
}

// ── 판정 헬퍼 (타게팅 규칙이 쓰는 것) ────────────────────────────────────────

func (p *Profile) Owns(category string) bool {
	for _, c := range p.Categories {
		if strings.EqualFold(c, category) {
			return true
		}
	}
	return false
}

func (p *Profile) UsesHeavily(category string) bool {
	for k, v := range p.Usage {
		if strings.EqualFold(k, category) && v == UsageHeavy {
			return true
		}
	}
	return false
}

// SectorScore 는 해당 섹터 점수. 없으면 0.
func (p *Profile) SectorScore(name string) float64 {
	for _, s := range p.Sectors {
		if strings.EqualFold(s.Name, name) {
			return s.Score
		}
	}
	return 0
}
