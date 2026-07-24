# 광고 프로파일 MV — 데이터파트 리뷰 반영 (v2)

> **초안 v2.** 데이터파트 리뷰(윤재훈·남홍식, 2026-07-24)를 반영했습니다. 재리뷰 요청.
> 확정 시 SQL 은 `goqual-data-transform/models/{gold,serving}/` 로 옮깁니다.
> 요청: 임베디드-엣지AI파트 · SSOT 는 repo `ads/analytics/`

## 리뷰 반영 요약

데이터파트 6개 항목 답과 남홍식 님의 보강 3건을 모두 반영해 **MV 구조를 다시 짰습니다.**
핵심은 사용강도 소스 교체입니다.

### 🔴 사용강도 소스 교체 — 남홍식 님 지적 수용

초안 v1 은 `mv_device_status_hourly` 를 재사용했는데, 그 MV 는 `dp_value_num IS NOT NULL`
로 **number/bool 만** 담고 enum/string 을 버립니다(silver DDL 확정). 광고 사용강도로는
정확히 반대로 편향된다는 지적이 맞습니다:

- 버려짐 = 모드변경·씬선택·팬속도 enum **제어** DP = 진짜 사용신호 → 과소집계
- 남음 = 온습도·pm25 센서 **주기보고** number = Q4 노이즈

→ numeric-only MV 를 버리고, silver `device_status` 를 직접 읽는 **전용 활동 MV** 를 신설했습니다.

### 파일 구조 (v2)

| 파일 | 위치(확정 시) | 역할 |
|---|---|---|
| `mv_ad_device_activity.sql` | `models/gold/` | 사용강도 **원자료** — device_status 전 DP 집계, product_key 1급 |
| `dim_device_account.sql` | `models/` | dev_id → 최신 uid/owner_id (bizevent) |
| `lookup_ad_usage_baseline.sql` | `models/gold/` | 카테고리별 사용강도 기준선(분위수, 2단계 정규화) |
| `mv_ad_device_profile.sql` | `models/gold/` | 조립 — 위 셋 + dim_product 조인 |
| `v_ad_device_profile.sql` | `models/serving/` | 서빙 뷰 |

### 항목별 반영

| # | 리뷰 | 반영 |
|---|---|---|
| Q1 | bizevent 에 uid/owner_id 실재, CDC 후 가구 신호 | `dim_device_account` 신설. **1차 grain 은 dev_id 유지**, uid/owner_id 부착만. 계정 합산은 CDC 후 |
| Q2 | SPLIT_PART 불필요, `product_key = dim_product.pid` 직접 | 폐기하고 직접 조인. activity 가 product_key 1급 컬럼 |
| Q3 | 커머스 구매이력 StarRocks 에 없음 | **섹터 신호 이번 범위에서 제외** 확정 |
| Q4 | dim_product_dp_schema 에 mode 컬럼 없음 | `dp_value_format` 로 1차 노이즈 제거(hex/base64/json/empty 제외). **권위적 조작성 구분은 mode 보강 필요 → 아래 추가 요청** |
| Q5 | 카테고리 분위수, lookup 2단계 | `lookup_ad_usage_baseline` 로 분리. MV 는 원자료만 |
| Q6 | 버킷3·ttl7 OK, refresh 비용 | refresh **일 1회**로 변경(EVERY 1 DAY). CURRENT_DATE 롤링창 배포 문제는 아래 |
| 🟡 | MAX(active_days) 과소집계 | activity 에서 dev_id 그레인 직접 `COUNT(DISTINCT DATE)` |
| 🟡 | CURRENT_DATE MV 증분 안 걸림 | refresh 일 1회로 완화. 비결정 함수 MV 배포 가부는 아래 확인 요청 |
| 📌 | dim_product 정적 seed 커버리지 | LEFT JOIN → null → fail-closed 미노출. 1차 감수, CDC 때 정리 |

---

## 🙏 데이터파트에 추가로 요청드리는 것

리뷰 반영 과정에서 새로 필요해진 것들입니다.

### A. `dim_product_dp_schema` 에 DP mode(ro/rw) 컬럼 보강 — 가장 중요

`dp_value_format` 만으로는 number 안의 **센서(온도) vs 제어(밝기)** 를 못 가릅니다.
남홍식 님도 지적하신 Q4 왜곡의 권위적 해결은 이 컬럼이 있어야 합니다.

- Tuya thing-model 의 DP `mode`(ro/rw/wr)를 이 dim 에 컬럼으로 넣어주실 수 있는지
- source 가 manual 시드라 같은 출처로 채우면 된다고 하셨는데, **누가 채울지**(데이터파트 시드 / 우리가 매핑 제공)
- 보강되면 `mv_ad_device_activity` 의 WHERE 에 `mode = 'rw'` 필터를 더해 센서 주기보고를 완전히 제외합니다

### B. `dp_value_format` 값 도메인·분포 확인

`WHERE dp_value_format IN ('number','bool','enum','string')` 로 페이로드성(hex/base64/json/empty)을
뺐는데, 실제 device_status 에서 이 포맷들의 **분포**를 알려주시면 필터가 맞는지 검증하겠습니다.
특히 enum 제어가 실제로 `enum` 으로 태깅되는지(아니면 `string` 으로 새는지).

### C. 카테고리 기준선 배치 소유 (Q5 후속)

`lookup_ad_usage_baseline` 를 **데이터파트 dbt model 로 소유**해 주실 수 있는지, 아니면
우리가 정의만 드리고 스케줄만 태워 주실지. 그리고 분위수 컷(제안 P70/P30)이 적절한지.

### D. stg 배포·검증 경로

- `CURRENT_DATE()` 같은 **비결정 함수가 MV 정의에 있을 때 stg StarRocks 에 배포 가능**한지.
  불가하면 남홍식 님 제안대로 `scheduled INSERT OVERWRITE` 로 전환하겠습니다.
- 우리가 이 MV 들을 **stg 에 직접 올려 검증**할 수 있는지, 아니면 데이터파트 PR/리뷰 경유인지.
- 검증용으로 `v_ad_device_profile` 를 **JSONL 로 뽑는 경로**(광고 서버 동기화 잡 입력) — 지금은
  수동 SELECT 라도 됩니다.

### E. CDC 마스터 랜딩 예정 시점 (참고)

가구 교차신호(홈 멤버십·공유 그래프) 강화 로드맵을 잡으려 합니다. 진행 중인 CDC 파이프라인의
대략적 랜딩 시점을 알려주시면, 계정 합산 프로파일을 그에 맞춰 후속 이슈로 잡겠습니다.

---

## 광고 서버 쪽 (이미 구현됨)

- 서빙 스토어(Valkey) + 동기화 잡(`adsync`)은 **JSONL 입력으로 완성**돼 있습니다.
  MV 가 확정되면 `v_ad_device_profile` → JSONL → `adsync` 만 배선하면 됩니다.
- 원본 `dev_id` 는 광고 서버로 넘어오지 않습니다 — 동기화 잡이 가명화(솔트 HMAC)합니다.
- **fail-closed**: 프로파일이 없으면 프로파일 타게팅은 매칭되지 않습니다(조용히 전체 노출로
  새지 않음). dim_product 커버리지 공백(신제품)도 이 규율로 안전하게 처리됩니다.
- 개인정보: 프로파일 TTL 로 신선도가 끊기면 자동 만료. 광고 목적 동의 범위는 별도 확인 중이며,
  동의 전까지 프로파일 타게팅은 켜지 않고 비타게팅으로만 운영합니다(그래도 CPC 는 성립).

## 다음 단계

1. 위 A~E 회신 → SQL 최종 확정
2. `goqual-data-transform` 에 PR (stg 먼저) — 소유 경로 확정 후
3. `v_ad_device_profile` → JSONL → `adsync` 배선 → stg 실측
4. 광고 목적 개인정보 동의 확인 → 프로파일 타게팅 활성화
