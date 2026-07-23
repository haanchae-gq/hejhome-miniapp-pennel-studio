/*
  ══════════════════════════════════════════════════════════════════════════════
  📋 초안 — goqual-data-transform 리뷰 요청용. 아직 배포된 모델이 아닙니다.
     확정되면 이 파일은 goqual-data-transform/models/gold/ 로 옮겨집니다.
     맥락·미결 질문: 같은 폴더의 README.md
  ══════════════════════════════════════════════════════════════════════════════

  mv_ad_device_profile — 광고 타게팅용 기기 프로파일 (grain: dev_id)

  ## 무엇에 쓰나
  광고 서버가 "이 기기(집)가 어떤 제품군이고, 그것을 얼마나 잘 쓰는가"를 물어볼 때
  쓰는 프로파일. 광고는 요청당 계정 1건 점조회라 StarRocks 를 직접 때리지 않고,
  이 MV → serving v_ → 서빙 스토어(Valkey/Postgres) 동기화 → 광고 서버가 읽는다.

  ## 왜 dev_id 인가 (계정이 아니라)
  현재 silver(device_status·device_bizevent)와 dim(dim_product·dim_category)에
  **uid/account 컬럼이 없다.** 그래서 계정 단위 프로파일을 지금은 만들 수 없다.
  다만 광고 패널은 기기 단위로 열리므로(패널이 deviceId 를 안다) 기기 단위만으로도
  1단계 타게팅은 성립한다. 계정 차원이 생기면 dev_id → account 로 한 겹 올린다.
  (README §질문 1)

  ## 사용 강도를 어떻게 재나
  "제어했다"의 대리 지표로 **DP 상태 변화 이벤트 수**를 쓴다. 측정값(온습도)만 올라오는
  기기와 사람이 실제로 조작하는 기기를 구분해야 하므로, 조작성 DP 만 세는 것이 맞다 —
  그 목록은 dim_product_dp_schema 에서 와야 한다(README §질문 4).
  지금 초안은 전체 DP 를 세고, 그 한계를 주석으로 남긴다.
*/

{{ config(
    materialized='materialized_view',
    partition_by=['snapshot_date'],
    refresh_method='ASYNC EVERY(INTERVAL 1 HOUR)',
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

WITH win AS (
    -- 최근 28일. 계절성(주말·평일)을 덮으면서 너무 늙지 않은 창.
    SELECT
        CURRENT_DATE()                    AS snapshot_date,
        dev_id,
        dp_code,
        SUM(event_count)                  AS ev,
        COUNT(DISTINCT DATE(event_hour))  AS active_days,
        MAX(event_hour)                   AS last_event_hour
    FROM {{ ref('mv_device_status_hourly') }}
    WHERE event_hour >= DATE_SUB(CURRENT_DATE(), INTERVAL 28 DAY)
    GROUP BY dev_id, dp_code
),
agg AS (
    SELECT
        snapshot_date,
        dev_id,
        SUM(ev)                    AS event_count_28d,
        MAX(active_days)           AS active_days_28d,
        COUNT(DISTINCT dp_code)    AS dp_variety,
        MAX(last_event_hour)       AS last_seen_at
    FROM win
    GROUP BY snapshot_date, dev_id
)
SELECT
    a.snapshot_date,
    a.dev_id,
    d.pid                                   AS product_key,
    d.category_id,
    d.product_ko_name,

    a.event_count_28d,
    a.active_days_28d,
    a.dp_variety,
    a.last_seen_at,

    /*
      사용 강도. 임계값은 **잠정**이다 — 제품군마다 정상 이벤트 빈도가 다르므로
      (조명은 하루 수십 번, 온습도 센서는 주기 보고) 카테고리별로 분위수를 떠서
      정하는 것이 옳다. 지금은 단순 절대값이고, 그 사실을 리뷰에서 확정한다.
      (README §질문 5)
    */
    CASE
        WHEN a.active_days_28d >= 20 AND a.event_count_28d >= 200 THEN 'heavy'
        WHEN a.active_days_28d >= 5                                THEN 'light'
        ELSE 'none'
    END                                     AS usage_level

FROM agg a
LEFT JOIN {{ ref('dim_product') }} d
       ON d.pid = SPLIT_PART(a.dev_id, ':', 1)   -- ⚠ dev_id→pid 결합키 미확인 (README §질문 2)
