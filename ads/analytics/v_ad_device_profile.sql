/*
  ══════════════════════════════════════════════════════════════════════════════
  📋 초안 — goqual-data-transform 리뷰 요청용.
     확정되면 goqual-data-transform/models/serving/ 로 옮겨집니다.
  ══════════════════════════════════════════════════════════════════════════════

  소비자(광고 서버 동기화 잡)용 얇은 뷰 — blue-green 무중단 교체 지점.
  소비자는 mv_* 를 직접 보지 말고 항상 이 v_* 를 통해 조회한다.
  MV 정의/이름 변경 시: 이 v_ 의 ref 만 교체 → 광고 서버 무영향.

  (기존 serving/v_*.sql 관례를 그대로 따름)
*/
{{ config(materialized='view') }}

SELECT *
FROM {{ ref('mv_ad_device_profile') }}
