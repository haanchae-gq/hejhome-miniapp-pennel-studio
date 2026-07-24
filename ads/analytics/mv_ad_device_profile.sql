/*
  ══════════════════════════════════════════════════════════════════════════════
  📋 초안 v2 — 데이터파트 리뷰(윤재훈·남홍식, 2026-07-24) 반영. 재리뷰용.
     확정 시 goqual-data-transform/models/gold/ 로 이동.
  ══════════════════════════════════════════════════════════════════════════════

  mv_ad_device_profile — 광고 타게팅용 기기 프로파일 (grain: dev_id)

  조립만 한다. 사용강도 원자료는 mv_ad_device_activity, 제품 차원은 dim_product,
  계정은 dim_device_account, 사용강도 정규화 기준선은 카테고리 lookup 에서 온다.

  ## v1 에서 바뀐 것 (리뷰 반영)

   Q2  dev_id→pid : SPLIT_PART 폐기. activity.product_key = dim_product.pid 직접 조인
   Q4  사용강도 소스 : numeric-only MV → mv_ad_device_activity(전 DP)로 교체
   Q5  usage_level : 절대 임계값 → 카테고리별 기준선 lookup 조인(2단계)
   Q1  계정 : dim_device_account 로 uid/owner_id 부착(계정 합산은 CDC 후)
   🟡  active_days : dev_id 그레인 직접 집계(activity 에서 이미 처리)

  ## dim_product 커버리지 (리뷰 📌)

  dim_product 는 정적 seed 라 신제품 pid 가 빠질 수 있다 → LEFT JOIN → category NULL →
  광고 서버에서 fail-closed(프로파일 없음으로 미노출). 1차는 감수하고, CDC 제품 마스터
  반영 시 정리한다. (조용히 전체 노출로 새지 않으므로 안전한 실패다.)
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
    a.snapshot_date,
    a.dev_id,
    a.product_key,
    d.category_id,
    d.product_ko_name,

    -- 계정(1차: 부착만, 합산은 CDC 후)
    acc.uid,
    acc.owner_id,

    a.event_count_28d,
    a.active_days_28d,
    a.dp_variety,
    a.last_seen_at,

    /*
      사용강도 = 카테고리 기준선 대비 상대 등급 (Q5).
      MV 안에서 분위수를 직접 계산하지 않는다 — 원자료 집계는 activity 가, 정규화는
      카테고리 기준선 lookup(별도 배치 산출)이 담당한다. 기준선이 아직 없으면
      lookup 이 비어 heavy/light 판정이 안 나오고 'none' 으로 떨어진다(보수적).
    */
    CASE
        WHEN b.heavy_min_events IS NULL THEN 'none'  -- 기준선 미산출 → 보수적
        WHEN a.active_days_28d >= b.heavy_min_days
         AND a.event_count_28d >= b.heavy_min_events THEN 'heavy'
        WHEN a.active_days_28d >= b.light_min_days   THEN 'light'
        ELSE 'none'
    END AS usage_level

FROM {{ ref('mv_ad_device_activity') }} a
LEFT JOIN {{ ref('dim_product') }} d
       ON d.pid = a.product_key
LEFT JOIN {{ ref('dim_device_account') }} acc
       ON acc.dev_id = a.dev_id
LEFT JOIN {{ ref('lookup_ad_usage_baseline') }} b
       ON b.category_id = d.category_id
