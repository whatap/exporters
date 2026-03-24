# NCloud Exporter

NCloud(네이버 클라우드 플랫폼) Cloud Insight 메트릭을 Prometheus 포맷으로 노출하는 Exporter입니다.

## 지원 서비스

| Namespace | 서비스 |
|---|---|
| `ncloud.vserver` | VPC Server |
| `ncloud.vloadbalancer` | VPC Load Balancer |
| `ncloud.vautoscaling` | VPC Auto Scaling |
| `ncloud.vmysql` | Cloud DB for MySQL |
| `ncloud.vpostgresql` | Cloud DB for PostgreSQL |
| `ncloud.vredis` | Cloud DB for Redis |
| `ncloud.vmongodb` | Cloud DB for MongoDB |
| `ncloud.vmssql` | Cloud DB for MSSQL |
| `ncloud.vnks` | Ncloud Kubernetes Service |
| `ncloud.vsearchengine` | Search Engine Service |
| `ncloud.vhadoop` | Cloud Hadoop |

## 빠른 시작

### 1. 설정 파일 작성

```bash
cp config.example.yaml ncloud.yaml
```

`ncloud.yaml`을 편집하여 NCloud API 키와 모니터링할 서비스를 설정합니다.

```yaml
ncloud:
  access_key: "YOUR_ACCESS_KEY"
  secret_key: "YOUR_SECRET_KEY"

exporter:
  listen_address: ":9850"
  metrics_path: "/metrics"
  scrape_interval: 300

namespaces:
  - name: "ncloud.vserver"
    enabled: true
    regions: ["KR"]
```

### 2. 빌드 및 실행

```bash
go build -o ncloud .
./ncloud
```

기본적으로 현재 디렉토리의 `ncloud.yaml`을 읽습니다. 다른 설정 파일을 사용하려면:

```bash
./ncloud --config ncloud-test.yaml
```

## 실행 옵션

| 플래그 | 설명 | 기본값 |
|---|---|---|
| `--config` | 설정 파일 경로 | `ncloud.yaml` |
| `--log.level` | 로그 레벨 (`debug`, `info`, `warn`, `error`) | `info` |
| `--log.format` | 로그 포맷 (`text`, `json`) | `text` |

```bash
# 기본 실행
./ncloud

# 디버깅 모드
./ncloud --log.level=debug

# JSON 로그 + 커스텀 설정 파일
./ncloud --config ncloud-prod.yaml --log.format=json --log.level=warn
```

## 설정

### 설정 파일 (ncloud.yaml)

| 항목 | 설명 | 기본값 |
|---|---|---|
| `ncloud.access_key` | NCloud API Access Key | (필수) |
| `ncloud.secret_key` | NCloud API Secret Key | (필수) |
| `exporter.listen_address` | HTTP 리스닝 주소 | `:9850` |
| `exporter.metrics_path` | 메트릭 엔드포인트 경로 | `/metrics` |
| `exporter.scrape_interval` | 스크래핑 간격 (초) | `300` |
| `namespaces[].name` | NCloud 서비스 네임스페이스 | - |
| `namespaces[].enabled` | 수집 활성화 여부 | `false` |
| `namespaces[].regions` | 대상 리전 (빈 배열 = 전체) | `[]` |

### 환경변수

| 환경변수 | 설명 |
|---|---|
| `NCLOUD_ACCESS_KEY` | `ncloud.access_key` 오버라이드 |
| `NCLOUD_SECRET_KEY` | `ncloud.secret_key` 오버라이드 |
| `NCLOUD_CW_API_GW` | Cloud Insight API 엔드포인트 (기본: `https://cw.apigw.ntruss.com`) |
| `NCLOUD_API_GW` | NCloud API Gateway 엔드포인트 (기본: `https://ncloud.apigw.ntruss.com`) |

## 메트릭 형식

```
ncloud_{service}_{metric}_{aggregation}
```

예시:
```
ncloud_vserver_cpu_used_rto_avg{instanceNo="12345", instanceName="my-server", regionCode="KR"} 45.2
ncloud_vserver_mem_usert_avg{instanceNo="12345", instanceName="my-server", regionCode="KR"} 72.1
```

내부 메트릭:
```
ncloud_scrape_errors_total{namespace="ncloud.vserver"} 0
ncloud_scrape_duration_seconds 1.234
```

## 아키텍처

NCloud Cloud Insight API의 모니터링 데이터를 Prometheus가 읽을 수 있는 포맷으로 변환하는 브릿지 역할을 합니다. 메트릭을 자체 생성하지 않으며, Cloud Insight 응답값을 그대로 전달합니다.

```
Prometheus  ──(scrape)──>  ncloud_exporter(:9850/metrics)
                                │
                                ├─ Cloud Insight API (cw.apigw.ntruss.com)
                                │   ├─ Product Key 조회 (cw_key 매핑)
                                │   ├─ 메트릭 정의 조회 (사용 가능한 메트릭 동적 발견)
                                │   └─ 메트릭 데이터 배치 쿼리 (최대 20개/요청)
                                │
                                └─ NCloud API Gateway (ncloud.apigw.ntruss.com)
                                    ├─ 리전 목록 조회
                                    └─ 인스턴스 목록 조회
```

### 데이터 수집 흐름

1. **Product Key 조회** — 초기 기동 시 `/schema/system/list`에서 서비스별 `cw_key` 매핑을 캐싱
2. **인스턴스 발견** — 각 namespace의 리소스 API로 인스턴스 목록 조회 (5분 캐시)
3. **메트릭 정의 조회** — Cloud Insight에서 사용 가능한 메트릭 목록 동적 발견 (5분 캐시)
4. **데이터 쿼리** — `QueryDataMulti`로 최근 10분 윈도우의 데이터를 배치 조회
5. **최신 값 추출** — 응답의 datapoints 중 가장 최근 값 1개만 Prometheus 메트릭으로 변환

> **참고:** scrape 간격은 5분이지만 query window가 10분인 이유는 Cloud Insight 데이터에 수집 지연이 있을 수 있어 빈 응답을 방지하기 위한 안전 마진입니다. 항상 최신 datapoint 1개만 사용하므로 데이터 중복은 발생하지 않습니다.

### 설정 리로드

실행 중 `SIGHUP` 시그널을 보내면 설정 파일을 다시 읽어 반영합니다:

```bash
kill -HUP $(pidof ncloud)
```
