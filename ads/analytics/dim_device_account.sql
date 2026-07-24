/*
  ══════════════════════════════════════════════════════════════════════════════
  📋 초안 v2 — 데이터파트 리뷰(Q1) 반영. goqual-data-transform/models 재리뷰용.
  ══════════════════════════════════════════════════════════════════════════════

  dim_device_account — dev_id → 최신 계정(uid)·소유자(owner_id)

  ## 근거 (리뷰 Q1)

  device_bizevent 에 uid·owner_id·hardware_uuid 가 실재한다(DDL 확정).
   · uid      : online/offline/bindUser/delete/nameUpdate — 활성 기기 커버율 99.99%
   · owner_id : bindUser/delete
  bizevent 는 저빈도(전체 ~0.67%)라 이 dim 비용도 낮다.

  ## 1차 한계 (감수)

  이벤트 로그 기반이라 다음은 담지 못한다 — CDC 마스터가 랜딩되면 강화한다:
   · 전체 기기 모집단 (bizevent distinct 134만 vs 마스터 150만, ~11% 휴면/캡처이전 누락)
   · 홈 멤버십·role (smart_group_user)
   · 기기 공유 그래프 (smart_group_user_sharing)
   · 현재상태 권위

  그래서 **1차 광고 프로파일 grain 은 dev_id 를 유지**한다(광고 패널이 기기 단위로 열림).
  uid/owner_id 는 컬럼으로 붙여만 두고, "이 집이 함께 쓰는 기기" 같은 가구 교차신호를
  쓰는 계정 단위 합산 프로파일은 CDC 마스터 랜딩 후 별도로 올린다.
*/

{{ config(materialized='materialized_view',
    refresh_method='ASYNC EVERY(INTERVAL 1 DAY)',
    distributed_by=['dev_id'], buckets=3,
    properties={"replication_num": "3", "storage_volume": "data_volume"}) }}

-- dev_id 별 가장 최근 uid/owner_id. bizevent 는 이벤트라 최신 승자를 뽑는다.
SELECT
    dev_id,
    -- event_time 최댓값의 uid/owner_id. NULL 이 아닌 최신값 우선.
    MAX_BY(uid, CASE WHEN uid IS NOT NULL THEN event_time END)           AS uid,
    MAX_BY(owner_id, CASE WHEN owner_id IS NOT NULL THEN event_time END) AS owner_id,
    MAX_BY(hardware_uuid, event_time)                                    AS hardware_uuid,
    MAX(event_hour)                                                      AS last_bizevent_at
FROM {{ source('silver', 'device_bizevent') }}
GROUP BY dev_id
