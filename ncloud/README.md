# NCloud Exporter

NCloud(네이버 클라우드 플랫폼) Cloud Insight 메트릭을 Prometheus 포맷으로 노출하는 Exporter.

- [설치 가이드](docs/install.md) — 설치·실행·OpenAgent 연동
- [수집 메트릭 지정 가이드](docs/metrics.md) — 서비스별 메트릭 목록 및 선택 방법
- [NCP API 구조와 디버깅](docs/api.md) — 게이트웨이 구성과 `ncp-api.sh` 사용법

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

## 빌드

```bash
./build.sh          # build.txt의 version/release_date를 주입해 linux amd64/arm64 빌드
```

산출물은 `bin/{amd64,arm64}/ncloud_exporter`. 버전은 `-ldflags`로 주입되며 `--version`으로 확인 가능.

## 빠른 시작

### 1. 설정 파일 작성

```bash
cp config.example.yml config.yml
```

`config.yml`을 편집해 NCloud API 키와 모니터링할 서비스를 지정.

```yaml
ncloud:
  access_key: "YOUR_ACCESS_KEY"
  secret_key: "YOUR_SECRET_KEY"

namespaces:
  - name: "ncloud.vserver"
    enabled: true
    regions: ["KR"]
    interval: "Min5"
    aggregations: ["AVG"]
    metrics:
      - name: "avg_cpu_used_rto"
        aggregations: ["AVG", "MAX"]
      - name: "mem_usert"
      - name: "load_average_*"
```

### 2. 빌드 및 실행

```bash
go build -o ncloud .
./ncloud
```

기본적으로 현재 디렉토리의 `config.yml`을 읽음. 다른 설정 파일을 쓰려면:

```bash
./ncloud --config=config-test.yml
```

## 실행 옵션

| 플래그 | 설명 | 기본값 |
|---|---|---|
| `--config` | 설정 파일 경로 | `config.yml` |
| `--web.listen-address` | HTTP 리스닝 주소 | `:9850` |
| `--web.telemetry-path` | 메트릭 엔드포인트 경로 | `/metrics` |
| `--log.level` | 로그 레벨 (`debug`, `info`, `warn`, `error`) | `info` |
| `--log.format` | 로그 포맷 (`text`, `json`) | `text` |
| `--version` | 버전 출력 후 종료 | - |

```bash
# 기본 실행
./ncloud

# 포트 변경
./ncloud --web.listen-address=:19850

# 디버깅 모드
./ncloud --log.level=debug

# JSON 로그 + 커스텀 설정 파일
./ncloud --config=config-prod.yml --log.format=json --log.level=warn
```

## 설정

### 설정 파일 (config.yml)

| 항목 | 설명 | 기본값 |
|---|---|---|
| `ncloud.access_key` | NCloud API Access Key | (필수) |
| `ncloud.secret_key` | NCloud API Secret Key | (필수) |
| `namespaces[].name` | NCloud 서비스 네임스페이스 | - |
| `namespaces[].enabled` | 수집 활성화 여부 | `false` |
| `namespaces[].regions` | 대상 리전 (빈 배열 = 전체) | `[]` |
| `namespaces[].interval` | 조회 간격 (`Min1`, `Min5`, `Min30`, `Hour2`, `Day1`) | `Min5` |
| `namespaces[].aggregations` | 네임스페이스 기본 aggregation | (API 제공 전체) |
| `namespaces[].metrics` | 수집할 메트릭 목록 (생략 = 전체) | `[]` |
| `namespaces[].metrics[].name` | 메트릭명. `*` 와일드카드 지원, 대소문자 무시 | (필수) |
| `namespaces[].metrics[].aggregations` | 해당 메트릭의 aggregation | (namespace 기본값) |

### 메트릭 선택

`metrics`를 지정하면 **지정한 메트릭만** 수집. CloudWatch Exporter의 `metrics:` 블록과 동일한 개념.

> 서비스별 메트릭 목록 확인 방법과 VPC Server 전체 메트릭표는 [docs/metrics.md](docs/metrics.md) 참고.

```yaml
namespaces:
  - name: "ncloud.vserver"
    enabled: true
    regions: ["KR"]
    interval: "Min5"
    aggregations: ["AVG"]      # 아래 메트릭들의 기본 aggregation
    metrics:
      - name: "avg_cpu_used_rto"
        aggregations: ["AVG", "MAX"]   # 개별 지정이 namespace 기본값을 덮어씀
      - name: "mem_usert"              # -> AVG 만 수집
      - name: "load_average_*"         # 1m / 5m / 15m 전부 매칭
```

동작 규칙:

- `metrics` **생략 시** 해당 네임스페이스의 모든 메트릭 × 모든 aggregation 수집 (기존 동작)
- aggregation 우선순위: `metrics[].aggregations` → `namespaces[].aggregations` → Cloud Insight가 해당 interval에 제공하는 전체
- 사용 가능한 aggregation은 메트릭·interval마다 상이. 미지원 값 지정 시 해당 값만 제외하고 `WARN` 로그 출력
- 설정한 `name`이 어떤 메트릭과도 매칭되지 않으면 `WARN` 로그로 알림 (오타 탐지)
- 잘못된 `interval` / `aggregation` 값은 기동 시점에 에러로 거부

> **수집량 주의:** 시리즈 수 = `메트릭 수 × aggregation 수 × 인스턴스 수`. Cloud Insight 배치 쿼리가 요청당 20개 제한이라 API 호출 수도 함께 증가. vserver는 필터 없이 수집 시 인스턴스당 125 시리즈(25개 메트릭 × 5 aggregation) 생성 → 필요한 메트릭만 지정 권장.
>
> 사용 가능한 메트릭명은 필터 없이 한 번 기동해 `/metrics`로 확인 가능. 노출 이름이 `ncloud_{service}_{metric}_{aggregation}` 형태이므로, 접두사와 aggregation 접미사를 뗀 가운데 부분이 `metrics[].name`에 넣을 값.

### 환경변수

| 환경변수 | 설명 |
|---|---|
| `NCLOUD_ACCESS_KEY` | `ncloud.access_key` 오버라이드 |
| `NCLOUD_SECRET_KEY` | `ncloud.secret_key` 오버라이드 |
| `NCLOUD_CW_API_GW` | Cloud Insight API 엔드포인트 (기본: `https://cw.apigw.ntruss.com`) |
| `NCLOUD_API_GW` | NCloud API Gateway 엔드포인트 (기본: `https://ncloud.apigw.ntruss.com`) |

## VPC Server 기본 메트릭

`config.example.yml`에 기본값으로 지정된 메트릭 목록. **15개 메트릭 → 인스턴스당 16 시리즈**로, Cloud Insight 배치 한도(20) 이내라 인스턴스 1대 기준 스크레이프당 API 호출 1회.

`Default` = `config.example.yml` 기본 포함 여부, `Statistic` = 기본값으로 수집하는 aggregation. `X`인 메트릭도 `metrics`에 추가하면 수집 가능.

`Description`은 Cloud Insight API 응답의 `desc` 값. 전체 27개 메트릭과 조회 방법은 [docs/metrics.md](docs/metrics.md) 참고.

| Metric | Description | Aggregation (Min5) | Unit | Dimension | Default | Statistic |
| --- | --- | --- | --- | --- | --- | --- |
| **CPU** | | | | | | |
| `avg_cpu_used_rto` | CPU Utilization Average | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | AVG ✅ |
| `max_cpu_used_rto` | CPU used ratio maximum | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | MAX ✅ |
| `load_average_1m` | CPU load 1 minute | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | AVG ✅ |
| `load_average_5m` | CPU load 5 minute average | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | AVG ✅ |
| `load_average_15m` | CPU load 15 minute average | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | AVG ✅ |
| **Memory** | | | | | | |
| `mem_usert` | Memory Utilization | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=memory) | O | AVG, MAX ✅ |
| `swap_usert` | Swap used ratio | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=memory) | O | AVG ✅ |
| **File System** | | | | | | |
| `avg_fs_usert` | File System Utilization Average | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | AVG ✅ |
| `max_fs_usert` | File system used ratio maximum | COUNT, SUM, MAX, MIN, AVG | % | instanceNo (type=svr) | O | MAX ✅ |
| **Disk I/O** | | | | | | |
| `avg_read_byt_cnt` | Disk read bytes average | COUNT, SUM, MAX, MIN, AVG | bytes/sec | instanceNo (type=svr) | O | AVG ✅ |
| `max_read_byt_cnt` | Disk read bytes maximum | COUNT, SUM, MAX, MIN, AVG | bytes/sec | instanceNo (type=svr) | X | - |
| `avg_write_byt_cnt` | Disk write bytes average | COUNT, SUM, MAX, MIN, AVG | bytes/sec | instanceNo (type=svr) | O | AVG ✅ |
| `max_write_byt_cnt` | Disk write bytes maximum | COUNT, SUM, MAX, MIN, AVG | bytes/sec | instanceNo (type=svr) | X | - |
| `avg_read_cnt` | Disk read count per second average | COUNT, SUM, MAX, MIN, AVG | num/sec | instanceNo (type=svr) | O | AVG ✅ |
| `max_read_cnt` | Disk read count per second maximum | COUNT, SUM, MAX, MIN, AVG | num/sec | instanceNo (type=svr) | X | - |
| `avg_write_cnt` | Disk write count per second average | COUNT, SUM, MAX, MIN, AVG | num/sec | instanceNo (type=svr) | O | AVG ✅ |
| `max_write_cnt` | Disk write count per second maximum | COUNT, SUM, MAX, MIN, AVG | num/sec | instanceNo (type=svr) | X | - |
| **Network** | | | | | | |
| `avg_rcv_bps` | Network In Average | COUNT, SUM, MAX, MIN, AVG | bits/sec | instanceNo (type=svr) | O | AVG ✅ |
| `max_rcv_bps` | Network receive bits/sec maximum | COUNT, SUM, MAX, MIN, AVG | bits/sec | instanceNo (type=svr) | X | - |
| `avg_snd_bps` | Network Out Average | COUNT, SUM, MAX, MIN, AVG | bits/sec | instanceNo (type=svr) | O | AVG ✅ |
| `max_snd_bps` | Network send bits/sec maximum | COUNT, SUM, MAX, MIN, AVG | bits/sec | instanceNo (type=svr) | X | - |
| `avg_rcv_pps` | Network receive packets/sec average | COUNT, SUM, MAX, MIN, AVG | packets/sec | instanceNo (type=svr) | X | - |
| `max_rcv_pps` | Network receive packets/sec maximum | COUNT, SUM, MAX, MIN, AVG | packets/sec | instanceNo (type=svr) | X | - |
| `avg_snd_pps` | Network send packets/sec average | COUNT, SUM, MAX, MIN, AVG | packets/sec | instanceNo (type=svr) | X | - |
| `max_snd_pps` | Network send packets/sec maximum | COUNT, SUM, MAX, MIN, AVG | packets/sec | instanceNo (type=svr) | X | - |
| **Not collected** | | | | | | |
| `dev_nm` | Device name | **Not supported** (Min1 only) | - | instanceNo (type=fs, mnt_nm=/) | X | ⚠️ STRING type → no time series even if specified |
| `mnt_stat_cd` | Mount state | **Not supported** (Min1 only) | - | instanceNo (type=fs, mnt_nm=/) | X | ⚠️ Available only with `interval: Min1` |

### 선정 기준

- 사용률(%) 4종(CPU / 메모리 / 파일시스템 / swap)은 대시보드·알람에 직접 사용 → 전부 포함
- CPU·파일시스템은 피크도 함께 수집. 5분 평균만으로는 순간 스파이크와 디스크 full 직전을 놓침
- 처리량(bps, bytes/sec, IOPS)은 평균만 수집. 피크까지 넣으면 시리즈 두 배
- 패킷 수(pps)는 제외. bps로 대부분 판단 가능하며 필요 시 설정 파일의 주석 해제

> **주의 — `avg_` / `max_` 접두사는 `aggregations` 설정과 별개.**
> NCP 서버 에이전트가 수집 주기 내 값을 미리 집계해 **서로 다른 메트릭**으로 제공하는 것. `avg_cpu_used_rto` + `MAX`는 "평균값들 중 조회 구간의 최댓값"이지 순간 피크가 아님. 피크가 필요하면 `max_cpu_used_rto`를 사용.

> **지원 Aggregation은 interval마다 다름.** 위 표는 기본값인 `Min5` 기준. 미지원 값을 지정하면 해당 값만 제외되고 `WARN` 로그 출력.

## 메트릭 형식

```
ncloud_{service}_{metric}_{aggregation}
```

예시:
```
ncloud_vserver_avg_cpu_used_rto_avg{instanceno="12345",instancename="my-server",regioncode="KR"} 45.2
ncloud_vserver_mem_usert_avg{instanceno="12345",instancename="my-server",regioncode="KR"} 72.1
```

내부 메트릭:
```
ncloud_scrape_errors_total{namespace="ncloud.vserver"} 0
ncloud_scrape_duration_seconds 1.234
```

## 아키텍처

NCloud Cloud Insight API의 모니터링 데이터를 Prometheus가 읽을 수 있는 포맷으로 변환하는 브릿지. 메트릭을 자체 생성하지 않고 Cloud Insight 응답값을 그대로 전달.

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
4. **메트릭 선택** — `namespaces[].metrics` 필터를 적용하고 메트릭별 aggregation 확정
5. **데이터 쿼리** — `QueryDataMulti`로 최근 10분 윈도우의 데이터를 배치 조회
6. **최신 값 추출** — 응답의 datapoints 중 가장 최근 값 1개만 Prometheus 메트릭으로 변환

> **참고:** query window(10분)가 scrape 간격(5분)보다 긴 것은 Cloud Insight 수집 지연으로 인한 빈 응답을 막기 위한 안전 마진. 항상 최신 datapoint 1개만 사용하므로 데이터 중복 없음.

### 설정 리로드

실행 중 `SIGHUP` 시그널 전달 시 설정 파일을 다시 읽어 반영:

```bash
kill -HUP $(pidof ncloud)
```

API 키, namespace, 메트릭 선택은 리로드로 반영. 리스닝 주소와 경로는 실행 인자이므로 재시작 필요.
