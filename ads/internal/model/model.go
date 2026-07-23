// Package model — 광고 서버의 도메인 타입.
//
// 설계 근거: add-ads/AD-SERVER-DESIGN.md
// 소재(Creative)의 본문은 여기 없다. 랜딩 HTML 은 **패널 스튜디오가 발행 시점에 구워서**
// 올린 것을 서빙만 한다 — 위젯 렌더 규격을 Go 로 옮기면 SSOT 가 두 벌이 되기 때문.
package model

import "time"

// Pricing 은 과금 방식. 1단계는 CPC·CPA·CPT 만 판다(노출 집계가 아직 없으므로 CPM 은 뒤).
type Pricing string

const (
	CPC Pricing = "cpc" // 클릭당
	CPA Pricing = "cpa" // 성과(전환)당
	CPT Pricing = "cpt" // 기간 정액
	CPM Pricing = "cpm" // 노출당 — 노출 신호가 생긴 뒤에만 유효
)

type Campaign struct {
	ID         string    `json:"id"`
	Advertiser string    `json:"advertiser"`
	Status     string    `json:"status"` // draft | active | paused | done
	StartsAt   time.Time `json:"startsAt"`
	EndsAt     time.Time `json:"endsAt"`
	Pricing    Pricing   `json:"pricing"`
	DailyCap   int       `json:"dailyCap"` // 0 = 무제한. 유효 클릭 기준.
}

// Review 는 소재 검수 상태. 제한 업종·소재 가이드 위반은 여기서 막는다.
type Review string

const (
	ReviewPending  Review = "pending"
	ReviewApproved Review = "approved"
	ReviewRejected Review = "rejected"
)

type Creative struct {
	ID         string `json:"id"`
	CampaignID string `json:"campaignId"`
	Format     string `json:"format"` // ad-lead | ad-trial | … (스튜디오 템플릿 id)
	Review     Review `json:"review"`
	// LandingHTML 은 스튜디오가 발행한 랜딩 본문. 비어 있으면 LandingURL 로 넘긴다.
	LandingHTML string `json:"landingHtml,omitempty"`
	LandingURL  string `json:"landingUrl,omitempty"`
	Title       string `json:"title,omitempty"`
}

// Targeting — 규칙이 비면 무조건 통과(비타게팅). 개인정보 동의 전까지는 비타게팅으로도
// CPC 운영이 가능하다는 것이 1단계 전제다.
type Targeting struct {
	ProductID []string          `json:"productId,omitempty"` // 기기 제품(PID) 중 하나
	DP        map[string]string `json:"dp,omitempty"`        // 지금 기기 상태 — 인텐트 타게팅

	// ── 계정 프로파일 기반 (IoT 플랫폼의 진짜 무기) ─────────────────────────
	// 이 계정이 무엇을 **잘 쓰고** 어떤 **섹터에 관심**이 있는지. 소스는 아직 미정이라
	// audience.Provider 로 분리해 두었다(§audience). 소스가 stub 인 동안 아래 규칙이 걸린
	// placement 는 **매칭되지 않는다(fail closed)** — 조용히 전체 노출로 새지 않게.
	OwnsCategory []string `json:"ownsCategory,omitempty"` // 보유 제품군
	UsesHeavily  []string `json:"usesHeavily,omitempty"`  // 사용 강도 상위 제품군
	Sector       []string `json:"sector,omitempty"`       // 관심 섹터
	SectorMin    float64  `json:"sectorMin,omitempty"`    // 섹터 점수 하한 (0~1, 기본 0.5)
}

// NeedsProfile 은 이 타게팅이 계정 프로파일을 요구하는지. 요구하는데 프로파일이 없으면
// 매칭 실패로 처리한다(선언된 실패 — 리포트에 사유가 남는다).
func (t Targeting) NeedsProfile() bool {
	return len(t.OwnsCategory) > 0 || len(t.UsesHeavily) > 0 || len(t.Sector) > 0
}

type FreqCap struct {
	Max       int `json:"max"`       // 기간 내 최대 노출/클릭 횟수
	WindowSec int `json:"windowSec"` // 기간(초). 기본 86400
}

type Placement struct {
	ID         string    `json:"id"`
	CampaignID string    `json:"campaignId"`
	CreativeID string    `json:"creativeId"`
	Slot       string    `json:"slot"`     // panel.<product>.<위치>
	Priority   int       `json:"priority"` // 높을수록 먼저
	Targeting  Targeting `json:"targeting"`
	FreqCap    *FreqCap  `json:"freqCap,omitempty"`
}

// Candidate 는 결정 엔진에 넘기는 한 벌.
type Candidate struct {
	Placement Placement
	Creative  Creative
	Campaign  Campaign
}

// ── 지표 ────────────────────────────────────────────────────────────────────

// EventType — impressionId 하나가 아래 사슬 전체를 묶는다.
//
//	impression(나중) → click → landing_view → engage → lead / convert
type EventType string

const (
	EvImpression  EventType = "impression"   // 패널에 떴다 — §6 의 B·C 를 붙여야 들어온다
	EvClick       EventType = "click"        // /go 통과 = 클릭
	EvLandingView EventType = "landing_view" // 랜딩이 실제로 열렸다 (클릭→도달 이탈 측정)
	EvEngage      EventType = "engage"       // 스크롤·체류 등 관여
	EvLead        EventType = "lead"         // 폼 제출
	EvConvert     EventType = "convert"      // 구매 등 전환 (Amount 동반)
)

type Event struct {
	ID         string    `json:"id"`
	ImpID      string    `json:"impId"` // 사슬을 묶는 열쇠
	Type       EventType `json:"type"`
	CampaignID string    `json:"campaignId"`
	CreativeID string    `json:"creativeId"`
	Slot       string    `json:"slot"`
	DeviceHash string    `json:"deviceHash"` // 원본 deviceId 는 저장하지 않는다
	ProductID  string    `json:"productId"`
	TS         time.Time `json:"ts"`
	Amount     int64     `json:"amount,omitempty"` // 전환 금액(원)
	// Billable=false 는 중복·부정 의심으로 과금에서 제외된 것. 버리지 않고 표시만 한다
	// (광고주 리포트는 유효분만, 내부 감사는 전량을 본다).
	Billable bool   `json:"billable"`
	Reason   string `json:"reason,omitempty"` // 무효 사유
}

// ── 감사 로그 ───────────────────────────────────────────────────────────────

// Audit — 누가·언제·무엇을 바꿨나.
//
// 광고는 돈이 오가는 일이라 "누가 이 캠페인을 켰나", "누가 이 소재를 검수 통과시켰나"에
// 답할 수 있어야 한다. 사고가 났을 때 이 기록이 없으면 아무것도 재구성할 수 없다.
// append-only — 지우거나 고치지 않는다.
type Audit struct {
	ID     int64     `json:"id"`
	Actor  string    `json:"actor"`  // 조작한 사람(이메일). 개발 모드면 'dev'
	Action string    `json:"action"` // creative.publish | campaign.create | campaign.status | creative.review
	Target string    `json:"target"` // 대상 ID
	Detail string    `json:"detail"` // 사람이 읽을 요약
	TS     time.Time `json:"ts"`
}
