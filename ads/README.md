# hej-adserver — 헤이홈 광고 서버 (1단계)

패널은 **바뀌지 않는 문(링크)** 하나만 갖고, 무엇을 보여줄지는 여기가 정한다.
설계 정본: [`../add-ads/AD-SERVER-DESIGN.md`](../add-ads/AD-SERVER-DESIGN.md)

> **지금은 `panel-studio` 리포 안에 있다.** 검증 후 떼어낼 것을 전제로 모듈 경로를
> 목적지 이름(`github.com/teamgoqual/hej-adserver`)으로 잡아 뒀다 — 분리할 때 이 디렉터리만
> 옮기면 된다.

## 왜 Go 인가, 그리고 왜 렌더는 Node 인가

| 책임 | 스택 |
|---|---|
| 저작 · **랜딩 HTML 렌더** | **Node** (패널 스튜디오) — 위젯 카탈로그·`emitAdStyles` 가 거기 있다 |
| 결정 · 리다이렉트 · **트래킹** · 서빙 | **Go** (여기) — 핫패스가 전부 데이터 처리 |

랜딩은 **발행 시점에 스튜디오가 구워서** 올린다. 광고 서버는 `Creative.LandingHTML` 을
서빙만 한다. 소재 교체는 여전히 즉시이면서 렌더 규격은 한 곳(SSOT)에 남는다.

## 실행

```bash
go test ./...                                   # 전체 테스트
ADS_PORT=8880 ADS_HASH_SECRET=<고정값> go run ./cmd/adserver
```

| env | 뜻 |
|---|---|
| `ADS_PORT` | 포트 (기본 8880) |
| `ADS_HASH_SECRET` | 식별자 해시 솔트. **꼭 고정** — 안 주면 재시작마다 빈도 제한·중복 판정이 초기화된다 |
| `ADS_BASE_URL` | 랜딩 절대 URL 베이스 (비면 상대경로) |

## 엔드포인트

```
GET  /healthz
GET  /go?slot=&d=&p=&s=&a=     결정 → 클릭 기록 → 302 랜딩 (없으면 204)
GET  /l/{creativeID}?imp=      랜딩 HTML + landing_view 기록
POST /e  {impId,type,amount}   engage | lead | convert
GET  /report?campaign=&since=  집계 지표
```

`d`(deviceId)·`a`(accountId)·`p`(productId)·`s`(DP 힌트)는 **전부 힌트다.** 패널은
클라이언트라 위조 가능하므로 과금·타게팅의 권위 있는 속성은 서버가 따로 확인해야 한다.

### 왕복 예시

```bash
# 필터 수명이 남았으면 → 204 (광고 없음)
curl -i "localhost:8880/go?slot=panel.airpurifier.consumable&d=dev1&s=filter_life=80"

# 필터가 다 됐으면 → 302 (인텐트 타게팅 적중)
curl -i "localhost:8880/go?slot=panel.airpurifier.consumable&d=dev1&s=filter_life=0"

curl "localhost:8880/l/cr-filter?imp=<imp>"
curl -X POST localhost:8880/e -d '{"impId":"<imp>","type":"convert","amount":39900}'
curl "localhost:8880/report?campaign=c-hej-store"
```

## 구조

```
cmd/adserver/      진입점 + 데모 시드(자사 커머스 = 첫 광고주)
internal/model/    도메인 타입 (캠페인·소재·배치·이벤트)
internal/decide/   결정 엔진 — 순수 함수. 슬롯·기간·검수·타게팅·빈도·예산
internal/track/    지표 트래커 — 해시 회전·중복 판정·집계
internal/store/    저장소 인터페이스 + 메모리 구현
internal/audience/ 계정 프로파일 seam — Provider 인터페이스 + Stub
internal/httpapi/  HTTP 표면
```

## 지금 하는 것 / 아직 안 하는 것

| | |
|---|---|
| ✅ 클릭·랜딩도달·리드·전환 집계, CTR·도달률·전환율 | |
| ✅ 인텐트 타게팅 (기기 DP 상태) | 필터 수명 0 → 필터 광고 |
| ✅ 중복 클릭 무효화(30초) · 빈도 제한 · 일일 예산 | |
| ✅ 식별자 해시 회전(일 단위) — 원본 미저장 | |
| ⏳ **계정 프로파일 소스** | `audience.Provider` 자리만 있음. `Stub` 인 동안 프로파일 타게팅은 **fail closed** |
| ⏳ 노출(impression) | 패널이 알려야 들어온다 — 설계서 §6 |
| ⏳ Postgres 저장소 | `store.Store` 구현만 추가하면 된다 (uiot 와 같은 pgx) |
| ⏳ 스튜디오 → 랜딩 발행 배선 | 지금은 시드에 HTML 을 직접 넣어 뒀다 |
| ⏳ 운영 콘솔 | 3단계 |

## 규율 셋 (테스트가 지킨다)

1. **모르면 매칭하지 않는다.** 프로파일 없는데 프로파일 타게팅이 통과하면 광고주 사기다.
   → `TestProfileTargetingFailsClosedOnStub`
2. **무효 클릭을 버리지 않고 표시한다.** 광고주 리포트는 유효분만, 감사는 전량.
   → `TestDedupWithinWindow`
3. **클라이언트를 믿지 않는다.** 사슬에 없는 `impID` 로 전환을 위조할 수 없다.
   → `TestUnknownImpRejected`

광고 서버가 죽어도 **기기 제어에는 영향이 없다.** 패널은 링크만 갖고 있고, 최악의 경우
링크가 안 열리는 것으로 끝난다.
