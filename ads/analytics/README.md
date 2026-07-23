# 광고 프로파일 MV — 데이터팀 리뷰 요청

> **초안입니다. 아직 배포된 모델이 아닙니다.**
> 확정되면 이 폴더의 SQL 은 `goqual-data-transform/models/{gold,serving}/` 로 옮깁니다.
> 요청: 임베디드-엣지AI파트 · 2026-07-23
> 맥락: [`add-ads/AD-SERVER-DESIGN.md`](../../add-ads/AD-SERVER-DESIGN.md) §4-3

## 무엇을 하려는가

광고 서버가 **"이 기기(집)가 어떤 제품군이고 그것을 얼마나 잘 쓰는가"** 를 알아야
타게팅이 성립합니다. 이게 IoT 플랫폼의 고유 강점이고, 키즈노트 같은 서비스가
"자녀 월령"으로 **추정**하는 자리를 우리는 실사용 데이터로 **확정**할 수 있습니다.

## 왜 StarRocks 인가 — 그리고 왜 직접 조회하지 않는가

이미 파이프라인이 prod 로 돌고 있어 **새 배치를 만들 필요가 없습니다.**
`mv_device_status_hourly`(`event_hour × dev_id × dp_code`)가 사용 강도의 재료를
그대로 갖고 있습니다.

다만 **광고 서버가 StarRocks 를 직접 조회하지는 않습니다.** StarRocks 는 MPP OLAP 라
대량 스캔·집계에 최적이고, 광고는 **요청당 1건 점조회 + 수 ms 응답 + QPS** 라 성격이
정반대입니다. 그래서 기존 `v_` 서빙 뷰 관례를 그대로 따릅니다:

```
mv_ad_device_profile  (gold, ASYNC 1h)
        │
   v_ad_device_profile  (serving — blue-green 교체 지점)
        │  동기화 잡 (주기 적재)
        ▼
   서빙 스토어 (Valkey 또는 Postgres)
        │  점조회 ~1ms
        ▼
   광고 서버 /go
```

**StarRocks 에 광고 트래픽 부하가 가지 않습니다.** 파이프라인 내부가 바뀌어도
`v_` 만 유지되면 광고 서버는 흔들리지 않습니다.

## 리뷰 부탁드릴 파일

| 파일 | 위치(확정 시) |
|---|---|
| [`mv_ad_device_profile.sql`](mv_ad_device_profile.sql) | `models/gold/` |
| [`v_ad_device_profile.sql`](v_ad_device_profile.sql) | `models/serving/` |

기존 관례를 따랐습니다 — dbt config 블록, `partition_by`+`distributed_by`,
gold 버킷 소수(3), `partition_ttl_number`, serving 은 `SELECT * FROM ref(mv)` 한 줄.

---

## ❓ 확인 부탁드리는 것 (이게 리뷰의 핵심입니다)

### 1. `dev_id → 계정(uid)` 매핑이 어딘가에 있습니까? — **가장 중요**

`sources.yml` 의 silver 3종(`device_status`·`device_bizevent`·`device_sensor_serving`)과
seed dim 3종(`dim_product`·`dim_category`·`dim_product_dp_schema`)을 봤는데
**uid/account/home 컬럼을 찾지 못했습니다.**

- 그래서 이 초안은 **기기 단위(`dev_id`)** 로 잡았습니다. 광고 패널이 기기 단위로
  열리므로 1단계는 이것으로도 성립합니다.
- 다만 **계정 단위**가 되면 "이 집은 조명·플러그·공기청정기를 함께 쓴다" 같은
  교차 신호가 생겨 타게팅 품질이 크게 올라갑니다.
- `device_bizevent` 에 `bind`/`delete` 가 있던데, **bind 이벤트에 uid 가 실려 있습니까?**
  실려 있다면 거기서 `dim_device_account` 를 만들 수 있을 것 같습니다.
- 없다면 Cube/hej-api 쪽에서 별도로 가져와야 하는데, 그 경로를 아시는지요.

### 2. `dev_id` 에서 `pid`(제품)를 어떻게 얻습니까?

초안에서 `SPLIT_PART(dev_id, ':', 1)` 로 임시 처리했는데 **근거 없는 추측입니다.**
`mv_device_status_hourly_by_product` 는 `product_key` 를 갖고 있으니 silver
`device_status` 에 `product_key`(또는 pid)가 이미 있을 것 같습니다 —
그렇다면 `mv_device_status_hourly` 대신 그쪽을 쓰거나, 결합키를 알려주시면
바로 고치겠습니다.

### 3. 커머스(m.hej.life) 구매 이력이 StarRocks 에 있습니까?

현재 파이프라인은 **Cube Pulsar(기기 이벤트) 출처**로 보입니다.
**관심 섹터**(반려동물·유아·요리 등)는 구매 이력이 가장 직접적인 신호인데,
기기 보유·사용만으로 추정하면 정밀도가 떨어집니다.

- 별도 계열로 들어와 있다면 어느 스키마인지
- 없다면, 섹터는 이번 범위에서 **빼고** 제품군·사용강도만으로 시작하겠습니다

### 4. 조작성 DP 를 구분할 수 있습니까?

사용 강도를 "DP 이벤트 수"로 재는데, 온습도 센서처럼 **주기 보고만 하는 DP** 가
섞이면 "잘 쓴다"가 왜곡됩니다. `dim_product_dp_schema` 에 **읽기전용/조작가능**
구분이 있습니까? 있으면 조작성 DP 만 세도록 고치겠습니다.

### 5. `usage_level` 임계값

초안은 `active_days>=20 & events>=200 → heavy` 같은 **절대값**인데, 제품군마다
정상 빈도가 달라(조명 vs 센서) 옳지 않습니다. **카테고리별 분위수**(예: 상위 30%)로
가는 게 맞다고 보는데, StarRocks MV 에서 분위수 계산이 부담스럽다면 대안을
제안해 주시면 좋겠습니다.

### 6. 운영 관점

- MV 하나 추가가 현재 클러스터 부하에 부담이 되는지 (28일 창 스캔)
- `partition_ttl_number: 7` 이 적절한지 (프로파일은 스냅샷이라 길게 둘 이유가 없다고 봤습니다)
- 버킷 3 이 맞는지 (gold 관례 stg 2 / prod 3 를 따랐습니다)

---

## 개인정보 관련

- 광고 서버는 **원본 식별자를 저장하지 않습니다.** `dev_id` 는 솔트 HMAC 으로
  가명화해 들고, 솔트는 **일 단위로 회전**합니다(날짜를 넘는 추적 불가).
- 프로파일 사본은 **동기화 잡 경계에서 보관기간을 강제**할 수 있습니다.
- **광고 목적 개인정보 활용 동의** 범위는 별도 확인 중입니다. 동의가 확인되기 전까지는
  프로파일 타게팅을 켜지 않고 **비타게팅으로만** 운영합니다(그래도 CPC 는 성립).
- 광고 서버 쪽 규율: **프로파일을 모르면 매칭하지 않습니다(fail closed).** 조용히
  전체 노출로 새면 광고주에게 "타게팅했다"고 하면서 아무나에게 나가는 셈이 되므로,
  소스가 없으면 광고를 내보내지 않고 리포트에 소스명(`stub`)이 그대로 드러납니다.

## 다음 단계

1. 위 6개 항목 회신 → SQL 수정
2. `goqual-data-transform` 에 PR (stg 먼저)
3. 동기화 잡 + 광고 서버 `ProfileStoreProvider` 배선
4. stg 에서 프로파일 타게팅 실측 → prod
