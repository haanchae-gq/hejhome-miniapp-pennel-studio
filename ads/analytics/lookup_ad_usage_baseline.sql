/*
  ══════════════════════════════════════════════════════════════════════════════
  📋 초안 v2 — 데이터파트 리뷰(Q5) 반영. 재리뷰용.
  ══════════════════════════════════════════════════════════════════════════════

  lookup_ad_usage_baseline — 카테고리별 사용강도 기준선 (Q5)

  ## 왜 별도 배치인가 (리뷰 Q5)

  제품군마다 정상 이벤트 빈도가 다르다(조명 하루 수십 번 vs 온습도 센서 주기보고).
  절대 임계값은 틀리고, 카테고리별 분위수가 맞다. 다만 MV 안에서 분위수를 매번
  계산하는 것은 부담되므로 **2단계로 분리**한다 — MV(activity)는 원자료 집계만,
  정규화 기준선은 이 lookup 이 담고 프로파일 MV 가 조인한다.

  ## 산출 방식 (제안 — 데이터파트 확정 필요)

  카테고리별로 활성 기기의 event_count_28d·active_days_28d 분포에서 분위수를 뜬다.
  예: heavy = 상위 30%(P70), light = 상위 70%(P30). 아래는 그 산출 배치의 예시이며,
  실제로는 별도 스케줄 배치(또는 dbt model)로 두고 이 테이블을 채운다.

      INSERT OVERWRITE lookup_ad_usage_baseline
      SELECT
          d.category_id,
          PERCENTILE_APPROX(a.active_days_28d,  0.70) AS heavy_min_days,
          PERCENTILE_APPROX(a.event_count_28d,  0.70) AS heavy_min_events,
          PERCENTILE_APPROX(a.active_days_28d,  0.30) AS light_min_days
      FROM mv_ad_device_activity a
      LEFT JOIN dim_product d ON d.pid = a.product_key
      GROUP BY d.category_id;

  ## 데이터파트 확정 요청

   · 분위수 컷(P70/P30)이 적절한지, 아니면 다른 값
   · 이 배치를 데이터파트가 소유할지(dbt model) / 우리가 정의만 줄지
   · 표본이 적은 카테고리(신제품군)의 기준선 처리 — 기본값 or 제외
*/

{{ config(materialized='materialized_view',
    refresh_method='ASYNC EVERY(INTERVAL 1 DAY)',
    distributed_by=['category_id'], buckets=2,
    properties={"replication_num": "3", "storage_volume": "data_volume"}) }}

SELECT
    d.category_id,
    PERCENTILE_APPROX(CAST(a.active_days_28d AS DOUBLE), 0.70) AS heavy_min_days,
    PERCENTILE_APPROX(CAST(a.event_count_28d AS DOUBLE), 0.70) AS heavy_min_events,
    PERCENTILE_APPROX(CAST(a.active_days_28d AS DOUBLE), 0.30) AS light_min_days
FROM {{ ref('mv_ad_device_activity') }} a
LEFT JOIN {{ ref('dim_product') }} d ON d.pid = a.product_key
WHERE d.category_id IS NOT NULL
GROUP BY d.category_id
