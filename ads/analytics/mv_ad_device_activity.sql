/*
  ══════════════════════════════════════════════════════════════════════════════
  📋 초안 v2 — 데이터파트 리뷰(윤재훈·남홍식, 2026-07-24) 반영. goqual-data-transform 재리뷰용.
     확정 시 goqual-data-transform/models/gold/ 로 이동.
  ══════════════════════════════════════════════════════════════════════════════

  mv_ad_device_activity — 광고 사용강도 **원자료** (grain: dev_id × product_key)

  ## 왜 새로 만드나 (리뷰 🔴 반영)

  초안 v1 은 mv_device_status_hourly 를 재사용했다. 그런데 그 MV 는
  `dp_value_num IS NOT NULL` 로 집계돼 **number/bool DP 만** 담고 enum/string 을 버린다
  (silver DDL 확정: dp_value_num 은 number/bool 만 채워짐). 광고 "사용강도" 기준으로는
  정확히 반대로 편향된다:

    · 버려짐 = 모드변경·씬선택·팬속도 같은 enum **제어** DP = 진짜 사용 신호  → 과소집계
    · 남음   = va_temperature·pm25 같은 센서 **주기보고** number             → Q4 노이즈

  그래서 numeric-only MV 를 버리고, silver device_status 를 직접 읽어 **전 DP 이벤트**를
  집계하는 전용 MV 를 신설한다. (기존 numeric MV 는 손대지 않는다.)

  ## dp_value_format 로 1차 노이즈 제거 (Q4 부분 대응)

  device_status.dp_value_format = number|bool|hex|base64|json|string|empty (sink heuristic).
  광고 사용신호로 부적합한 것(hex/base64/json/empty — 페이로드·인코딩)을 뺀다.
  다만 이건 **권위가 아니다**. number 안에도 센서(온도)와 제어(밝기)가 섞인다.
  진짜 조작성 구분은 dim_product_dp_schema.mode(ro/rw) 가 있어야 하며, 그 보강을
  데이터파트에 요청 중이다(README §Q4). 보강되면 아래 WHERE 에 rw 필터를 더한다.

  ## grain·수정 (리뷰 🟡 반영)

  · product_key 를 1급 컬럼으로 담는다 → 광고 프로파일에서 SPLIT_PART 없이 dim_product 조인
  · 활동일수는 **dev_id 그레인에서 직접** 센다. v1 의 MAX(active_days)(dp_code별 최댓값)는
    서로 다른 날 다른 DP 를 쓴 기기를 과소집계했다.

  ## 리프레시 (리뷰 🟡 반영)

  광고 프로파일은 시간 신선도가 불필요하다. ASYNC EVERY 1 HOUR 로 28일 창(≈1.83억 행)을
  매시 재계산하는 것은 과하다. **일 1회**로 낮춘다. 또한 snapshot_date=CURRENT_DATE() 롤링
  28일 창은 베이스 파티션(event_hour)과 안 맞아 증분이 안 걸리므로, stg 에서 비결정 함수 MV
  배포 가능 여부를 먼저 확인하고 — 불가하면 scheduled INSERT OVERWRITE 로 전환한다(README §배포).
*/

{{ config(
    materialized='materialized_view',
    partition_by=['snapshot_date'],
    refresh_method='ASYNC EVERY(INTERVAL 1 DAY)',
    distributed_by=['dev_id'],
    buckets=3,
    properties={
        "partition_ttl_number": "7",
        "replicated_storage": "true",
        "replication_num": "3",
        "query_rewrite_consistency": "LOOSE",
        "datacache.enable": "true",
        "storage_volume": "data_volume"
    }
) }}

SELECT
    CURRENT_DATE()                        AS snapshot_date,
    dev_id,
    -- product_key 는 기기당 고정. MAX 로 대표값을 뽑는다(그룹 편의).
    MAX(product_key)                      AS product_key,

    COUNT(*)                              AS event_count_28d,
    COUNT(DISTINCT DATE(event_hour))      AS active_days_28d,   -- dev_id 그레인 직접 집계
    COUNT(DISTINCT dp_code)               AS dp_variety,
    MAX(event_hour)                       AS last_seen_at

FROM {{ source('silver', 'device_status') }}
WHERE event_hour >= DATE_SUB(CURRENT_DATE(), INTERVAL 28 DAY)
  -- 광고 사용신호로 부적합한 포맷 제외(페이로드·인코딩). number 안 센서/제어 구분은
  -- dim_product_dp_schema.mode 보강 후 rw 필터를 여기 더한다.
  AND dp_value_format IN ('number', 'bool', 'enum', 'string')
GROUP BY dev_id
