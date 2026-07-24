# 광고 서버 배포 — 개발 단계 (hejdev6)

> **결정(2026-07-23)**: 개발 단계는 **A + V + D1(추후)** —
> hejdev6 컨테이너 · Valkey 서빙 스토어 · `ads.hej.life` 는 나중에.

## ⚠️ 지금은 개발 단계다 — 실사용자 트래픽을 붙이지 않는다

hejdev6 은 **공용 개발 워크스테이션**이다. SLA 가 없고, 재부팅·다른 사람 작업·디스크에
영향받는다. 여기 올라간 광고 서버는 **개발·검증용**이며 실제 패널이 가리키게 하지 않는다.

운영 이관 대상은 **EKS(stg → prod) + APISIX** 다. 그때 얻는 것:

- APISIX 의 `limit-req`/`limit-count` 가 **부정 클릭 방어에 한 겹 더** 붙는다
- 사내 표준 관측·시크릿·배포(Terraform · `goqual-k8s-manifest` · ECR)

## 이관을 쉽게 하려고 지금 해둔 것

| 항목 | 지금 | 이관 시 |
|---|---|---|
| 모듈 경로 | `github.com/teamgoqual/hej-adserver` | 디렉터리만 옮기면 됨 |
| 설정 | **전부 env** | 값만 교체, 코드 변경 0 |
| Valkey 키스페이스 | `ADS_VALKEY_PREFIX` (기본 `adprof`) | **운영 Valkey 는 APISIX 쿼터와 공유** — 접두어로 격리 |
| 프로파일 소스 | `ADS_VALKEY_ADDR` 없으면 Stub 자동 폴백 | 주소만 바꾸면 됨 |
| 저장소 | 메모리 | `store.Store` 구현 추가 (pgx, uiot 와 같은 스택) |

## 띄우기

### 1) Valkey (이미 떠 있음)

```bash
docker run -d --name ads-valkey --network proxy --restart unless-stopped \
  -v /disk-A/docker-data/ads-valkey:/data \
  valkey/valkey:8-alpine \
  valkey-server --save 60 1 --appendonly no --maxmemory 256mb --maxmemory-policy allkeys-lru
```

호스트 포트를 열지 않는다 — `proxy` 네트워크 안에서만 닿는다(panel-studio 와 같은 관례).

### 2) 프로파일 동기화

```bash
export ADS_VALKEY_ADDR=ads-valkey:6379      # 컨테이너 밖에서는 IP
export ADS_HASH_SECRET=<고정값>

go run ./cmd/adsync -in profiles.jsonl -hash-input
```

`-hash-input` 은 입력 `dev_id` 가 **원본**일 때. 이 잡이 가명화 경계라, 원본 식별자가
Valkey 로 넘어가지 않는다. 이미 가명화된 입력이면 플래그를 빼면 된다.

> **StarRocks 직결은 아직 안 붙였다.** MV·서빙 뷰가 데이터팀 리뷰 중이고
> (`ads/analytics/README.md`) 결합키·계정 매핑이 미확정이라, 지금 붙이면 틀린 쿼리를
> 박게 된다. 확정되면 `adsync` 에 StarRocks 소스를 더한다.

### 3) 서버

```bash
export ADS_PORT=8880 ADS_HASH_SECRET=<고정값> ADS_VALKEY_ADDR=ads-valkey:6379
go run ./cmd/adserver
```

## env

| 키 | 뜻 |
|---|---|
| `ADS_PORT` | 포트 (기본 8880) |
| `ADS_HASH_SECRET` | 식별자 해시 솔트. **꼭 고정** — 바뀌면 빈도 제한·프로파일 조회가 전부 어긋난다 |
| `ADS_VALKEY_ADDR` | 서빙 스토어. **없으면 Stub 폴백** (프로파일 타게팅만 꺼지고 광고는 계속 나간다) |
| `ADS_VALKEY_PASSWORD` · `ADS_VALKEY_DB` | 접속 |
| `ADS_VALKEY_PREFIX` | 키스페이스 접두어 (기본 `adprof`). **운영 공유 Valkey 에서 필수** |
| `ADS_PROFILE_TTL` | 프로파일 수명 (기본 48h). 동기화 주기의 3~4배 권장 |
| `ADS_PROFILE_CACHE_TTL` | 프로세스 내 캐시 (기본 5m) |
| `ADS_BASE_URL` | 랜딩 절대 URL 베이스 |

## 두 가지 안전장치

**빈 스냅샷은 반영되지 않는다.** 동기화 입력이 비면 `adsync` 가 거부한다 —
빈 스냅샷으로 갈아치우면 전 계정의 타게팅이 조용히 꺼지는 사고가 난다.

**프로파일에 TTL 이 있다.** 동기화 잡이 조용히 죽어도 광고는 계속 나가는데, 그때
**일주일 묵은 프로파일로 타게팅하는 것**이 가장 나쁜 실패다. TTL 이 지나면 프로파일이
사라지고 결정 엔진이 fail closed 로 넘어가 비타게팅으로 안전하게 떨어진다.

## 도메인 (D1 — 아직 아님)

최종은 `ads.hej.life`(소비자 도메인)다. 개발 단계에서는 도메인을 붙이지 않고
`proxy` 네트워크 안에서만 접근한다. 외부 확인이 필요해지면 panel-studio 와 같은
Cloudflare 터널로 **내부 도메인**을 먼저 붙인다 — 소비자 도메인은 운영 이관 때.
