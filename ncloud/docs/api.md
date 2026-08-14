# NCP API 구조와 디버깅

Exporter가 어떤 API를 왜 호출하는지, 문제가 생겼을 때 `scripts/ncp-api.sh`로 어떻게 확인하는지 정리한 문서입니다.

## 게이트웨이가 두 개인 이유

NCP는 목적이 다른 두 개의 API 게이트웨이를 제공합니다. 하나는 **무엇이 있는지**, 다른 하나는 **그 값이 얼마인지**를 담당합니다.

| 스크립트 별칭 | 호스트 | 역할 | 얻는 것 |
|---|---|---|---|
| `vpc` | `ncloud.apigw.ntruss.com` | 리소스(관리) API | 리전 목록, 인스턴스 목록과 **인스턴스 번호** |
| `cw` | `cw.apigw.ntruss.com` | Cloud Insight(모니터링) API | 제품 키, 메트릭 정의, **메트릭 값** |

핵심은 **Cloud Insight가 인스턴스를 나열해주지 않는다**는 점입니다. 메트릭 데이터를 조회하려면 아래 세 값이 모두 필요한데, 출처가 서로 다릅니다.

```
POST cw /cw_fea/real/cw/api/data/query/multiple
{
  "prodKey":    "460438474722512896",          ← cw  : /schema/system/list
  "metric":     "avg_cpu_used_rto",            ← cw  : /rule/group/metric/search
  "dimensions": {"instanceNo": "132480189"}    ← vpc : /vserver/v2/getServerInstanceList
}
```

그래서 exporter는 양쪽 게이트웨이를 모두 사용합니다. `collector.go`가 Cloud Insight 클라이언트(`ci`)와 리소스 클라이언트(`rc`)를 함께 들고 있는 이유입니다.

```
ncloud_exporter
  ├─ cw  (CloudInsightClient) ─ 제품 키 / 메트릭 정의 / 메트릭 데이터
  └─ vpc (ResourceClient)     ─ 리전 목록 / 인스턴스 목록
```

인증 방식(signature v2, HMAC-SHA256)과 사용하는 키는 양쪽이 동일합니다. `ncp-api.sh`가 호스트만 바꿔 끼우는 구조인 것도 이 때문입니다.

## 호출하는 API 목록

| 게이트웨이 | 경로 | 메서드 | 호출 시점 |
|---|---|---|---|
| `cw` | `/cw_fea/real/cw/api/schema/system/list` | GET | 기동 시 1회 |
| `cw` | `/cw_fea/real/cw/api/rule/group/metric/search` | POST | 스크레이프 시 (5분 캐시) |
| `cw` | `/cw_fea/real/cw/api/data/query/multiple` | POST | 스크레이프마다 (최대 20개/요청) |
| `vpc` | `{service}/getRegionList` | GET | `regions: []`인 경우만 (5분 캐시) |
| `vpc` | `{service}/get{Service}InstanceList` | GET | 스크레이프 시 (5분 캐시) |

서비스별 경로는 `ncloud/resource.go`의 `KnownServices`에 정의되어 있습니다. 예를 들어 `ncloud.vserver`는 `/vserver/v2` + `/getServerInstanceList` 형태로 조합됩니다.

> 리전 목록 API가 없는 서비스도 있습니다. 해당 서비스의 리전 API가 `404`를 반환하면 exporter는 `/vserver/v2/getRegionList`로 대체 조회합니다.

## 최초 확인 시 실행하는 두 명령

```bash
export NCLOUD_ACCESS_KEY=... NCLOUD_SECRET_KEY=...

./scripts/ncp-api.sh GET vpc "/vserver/v2/getRegionList?responseFormatType=json"
./scripts/ncp-api.sh GET cw  "/cw_fea/real/cw/api/schema/system/list"
```

이 두 개를 먼저 실행하는 이유는 다음과 같습니다.

### 1. `getRegionList` — 리소스 게이트웨이 인증 확인

파라미터가 필요 없고 응답이 짧아, **키가 유효한지와 서명 생성이 올바른지** 확인하는 가장 가벼운 호출입니다. 부수적으로 `config.yml`의 `regions`에 넣을 리전 코드(`KR`, `SGN` 등)를 얻습니다.

`regions: []`로 비워두면 exporter가 기동 후 이 API를 호출해 전체 리전을 자동으로 채웁니다.

### 2. `/schema/system/list` — Cloud Insight 게이트웨이 인증 확인 + 제품 키 확보

메트릭 조회의 **1단계**입니다. Cloud Insight는 서비스를 이름이 아니라 `cw_key`라는 숫자 ID로 식별하는데, 이 매핑을 제공하는 API가 이것뿐입니다.

```bash
./scripts/ncp-api.sh GET cw "/cw_fea/real/cw/api/schema/system/list" \
  | jq -r '.[] | "\(.cw_key)\t\(.prodName)"'
# 460438474722512896   Server(VPC)
```

이 키가 있어야 2단계인 메트릭 목록 조회를 호출할 수 있습니다.

```bash
./scripts/ncp-api.sh POST cw "/cw_fea/real/cw/api/rule/group/metric/search" \
  '{"prodKey":"460438474722512896","query":""}'
```

exporter도 기동 시 `/schema/system/list`를 가장 먼저 호출하며, **실패하면 즉시 종료**합니다(`main.go`의 `InitCWKeys`). 제품 키가 없으면 어떤 메트릭도 조회할 수 없기 때문입니다.

## 스크립트 사용법

```bash
./scripts/ncp-api.sh <METHOD> <HOST> <URI> [BODY]
```

| 인자 | 값 |
|---|---|
| `METHOD` | `GET` / `POST` |
| `HOST` | `vpc` / `cw` / 전체 URL |
| `URI` | `/`로 시작하는 경로. 쿼리스트링 포함 |
| `BODY` | POST 요청의 JSON 본문 |

서명이 맞지 않을 때는 `DEBUG=1`로 서명 대상 메시지를 확인할 수 있습니다.

```bash
DEBUG=1 ./scripts/ncp-api.sh GET cw "/cw_fea/real/cw/api/schema/system/list"
```

> **주의**
> 서명은 요청 URI 문자열과 정확히 일치해야 합니다. 쿼리스트링을 포함해 서명하므로, 파라미터 순서나 인코딩이 달라지면 401이 발생합니다.

## 자주 보는 오류

| 응답 | 원인 | 확인할 것 |
|---|---|---|
| `401` + `Authentication Failed` / `This account is not allowed.` | 키 오류, 권한 부족, 서명 불일치 | 키 값, 해당 API 권한, `DEBUG=1`로 서명 메시지 |
| `401` (키·경로가 모두 정확한데도 발생) | 서명에 사용한 타임스탬프와 서버 시각 불일치 가능성 | 서명이 타임스탬프 기반이므로 시스템 시각 동기화 상태를 확인합니다 |
| `404` | 경로 오타 또는 게이트웨이 혼동 | `cw` 경로를 `vpc`로 호출하지 않았는지 확인 |
| 메트릭 목록이 비어 있음 | `prodKey` 불일치 | `/schema/system/list`에서 해당 서비스의 `cw_key`를 다시 확인 |
| 데이터 조회 결과가 빈 배열 | 조회 구간에 데이터 없음 | 서버 모니터링 에이전트 설치 여부, 조회 시간 범위(exporter는 최근 10분 사용) |

## 엔드포인트 변경

기본값 외의 엔드포인트를 사용하려면 환경변수로 지정합니다. exporter와 스크립트 모두 동일한 호스트를 바라보게 해야 합니다.

| 환경변수 | 기본값 |
|---|---|
| `NCLOUD_CW_API_GW` | `https://cw.apigw.ntruss.com` |
| `NCLOUD_API_GW` | `https://ncloud.apigw.ntruss.com` |

## 참고

- [수집 메트릭 지정 가이드](metrics.md) — 메트릭 목록 확인 및 선택 방법
- [설치 가이드](install.md) — 설치·실행·OpenAgent 연동
- `ncloud/FLOW_DETAIL.txt` — 내부 처리 흐름 상세
