# 수집 메트릭 지정 가이드

`config.yml`의 `namespaces[].metrics`에 어떤 값을 넣을 수 있는지, 그 목록을 어디서 확인하는지 정리한 문서입니다.

## 왜 지정해야 하나

`metrics`를 생략하면 해당 서비스의 **모든 메트릭 × 모든 aggregation**을 수집합니다. VPC Server 기준으로 인스턴스 1대당 **125개 시리즈**(25 메트릭 × 5 aggregation)가 생성되고, Cloud Insight 배치 쿼리는 요청당 20개 제한이라 스크레이프마다 **API 호출 7회**가 발생합니다. 인스턴스가 늘어나면 그대로 곱해집니다.

필요한 것만 지정하면 다음과 같이 줄어듭니다.

| | 시리즈/인스턴스 | 배치 API 호출 |
|---|---|---|
| 필터 없음 | 125 | 7 |
| `config.example.yml` 기본값 | 16 | 1 |

## 메트릭 목록 확인 방법

### 1. Cloud Insight API (권장)

가장 정확한 소스입니다. 메트릭명, 설명, 단위, **interval별 사용 가능한 aggregation**까지 전부 나옵니다. exporter가 기동 시 호출하는 API와 동일합니다.

`scripts/ncp-api.sh`로 조회할 수 있습니다.

```bash
export NCLOUD_ACCESS_KEY=... NCLOUD_SECRET_KEY=...

# 1단계: 서비스별 제품 키(cw_key) 조회
./scripts/ncp-api.sh GET cw "/cw_fea/real/cw/api/schema/system/list" \
  | jq -r '.[] | "\(.cw_key)\t\(.prodName)"'
# → 460438474722512896   Server(VPC)

# 2단계: 해당 제품의 메트릭 목록 조회
./scripts/ncp-api.sh POST cw "/cw_fea/real/cw/api/rule/group/metric/search" \
  '{"prodKey":"460438474722512896","query":""}' > metrics.json

# 표로 정리
jq -r '.metrics[] | [.metric, .unit, (.options.Min5 // [] | join(",")), .desc] | @tsv' metrics.json \
  | column -t -s $'\t'
```

`prodName`은 `ncloud/resource.go`의 `KnownServices`에 서비스별로 정의되어 있습니다 (예: `ncloud.vserver` → `Server(VPC)`).

### 2. exporter를 필터 없이 한 번 기동

가장 간단한 방법입니다. `metrics` 항목을 지운 설정으로 띄우고 `/metrics`를 확인합니다.

```bash
./ncloud_exporter --config=config.yml --web.listen-address=:19850 &
curl -s localhost:19850/metrics | grep '^ncloud_vserver' | sed 's/{.*//' | sort -u
```

노출되는 이름은 `ncloud_{service}_{metric}_{aggregation}` 형태이므로, **접두사(`ncloud_vserver_`)와 aggregation 접미사(`_avg`)를 뗀 가운데 부분**이 `metrics[].name`에 넣을 값입니다.

```
ncloud_vserver_avg_cpu_used_rto_avg
       └서비스┘└──── name ────┘└aggr┘
```

주의: 이 방법은 **실제로 데이터가 있는 메트릭만** 보입니다. 값이 비어 있는 메트릭은 시리즈가 생성되지 않아 목록에서 빠집니다. 전체 정의를 보려면 1번을 쓰세요.

### 3. NCP 콘솔 / 공식 문서

Console → Services → Cloud Insight → Metrics에서 서비스를 선택하면 목록을 볼 수 있습니다. 콘솔 표시명은 한글/영문 설명이라 API의 실제 메트릭명(`avg_cpu_used_rto`)과 다르므로, 설정에 넣을 값은 1번이나 2번으로 확인하는 편이 안전합니다.

## VPC Server (`ncloud.vserver`) 메트릭 전체

`cw_key` = `460438474722512896` (`Server(VPC)`), 식별 dimension = `instanceNo`.

아래 25개는 모든 interval(Min1/Min5/Min30/Hour2/Day1)에서 `AVG` `MAX` `MIN` `SUM` `COUNT`를 전부 지원합니다.

### CPU

| 메트릭 | 단위 | 설명 |
|---|---|---|
| `avg_cpu_used_rto` | % | CPU 사용률 평균 |
| `max_cpu_used_rto` | % | CPU 사용률 최대 |
| `load_average_1m` | % | CPU load 1분 |
| `load_average_5m` | % | CPU load 5분 평균 |
| `load_average_15m` | % | CPU load 15분 평균 |

### 메모리

| 메트릭 | 단위 | 설명 |
|---|---|---|
| `mem_usert` | % | 메모리 사용률 |
| `swap_usert` | % | swap 사용률 |

### 파일시스템

| 메트릭 | 단위 | 설명 |
|---|---|---|
| `avg_fs_usert` | % | 파일시스템 사용률 평균 |
| `max_fs_usert` | % | 파일시스템 사용률 최대 |

### 디스크 I/O

| 메트릭 | 단위 | 설명 |
|---|---|---|
| `avg_read_byt_cnt` | bytes/sec | 디스크 read bytes 평균 |
| `max_read_byt_cnt` | bytes/sec | 디스크 read bytes 최대 |
| `avg_write_byt_cnt` | bytes/sec | 디스크 write bytes 평균 |
| `max_write_byt_cnt` | bytes/sec | 디스크 write bytes 최대 |
| `avg_read_cnt` | num/sec | 디스크 read 횟수 평균 |
| `max_read_cnt` | num/sec | 디스크 read 횟수 최대 |
| `avg_write_cnt` | num/sec | 디스크 write 횟수 평균 |
| `max_write_cnt` | num/sec | 디스크 write 횟수 최대 |

### 네트워크

| 메트릭 | 단위 | 설명 |
|---|---|---|
| `avg_rcv_bps` | bits/sec | 네트워크 수신 평균 |
| `max_rcv_bps` | bits/sec | 네트워크 수신 최대 |
| `avg_snd_bps` | bits/sec | 네트워크 송신 평균 |
| `max_snd_bps` | bits/sec | 네트워크 송신 최대 |
| `avg_rcv_pps` | packets/sec | 수신 패킷 평균 |
| `max_rcv_pps` | packets/sec | 수신 패킷 최대 |
| `avg_snd_pps` | packets/sec | 송신 패킷 평균 |
| `max_snd_pps` | packets/sec | 송신 패킷 최대 |

### 수집 대상이 아닌 메트릭

아래 2개는 정의상 존재하지만 **`Min1`에서만** 제공되고 값이 숫자가 아니어서 Prometheus 메트릭으로 변환되지 않습니다. 설정에 넣어도 시리즈가 생성되지 않습니다.

| 메트릭 | 타입 | 설명 |
|---|---|---|
| `dev_nm` | STRING | 디바이스명 |
| `mnt_stat_cd` | INTEGER | 마운트 상태 코드 |

> **`avg_` / `max_` 접두사에 대해:** 이건 aggregation 설정과 별개입니다. NCP가 서버 에이전트에서 수집 주기 내 값을 미리 집계해 **서로 다른 메트릭으로** 제공하는 것입니다. 즉 `avg_cpu_used_rto`에 `aggregation: [MAX]`를 걸면 "평균 CPU 사용률들 중 조회 구간의 최댓값"이고, `max_cpu_used_rto`의 `AVG`는 "피크 CPU 사용률의 평균"입니다. 순간 피크를 보려면 `max_*` 메트릭을 쓰세요.
>
> **디스크·파일시스템 메트릭:** 원본 데이터는 디바이스(`dev_nm`)별로 존재하지만 exporter는 `instanceNo` 하나만 dimension으로 지정하므로, Cloud Insight가 인스턴스 단위로 합산·집계한 값이 옵니다. 디바이스별 분리는 현재 지원하지 않습니다.

## 설정 예시

### 기본값 (`config.example.yml`에 포함)

인스턴스당 16개 시리즈. 서버 모니터링에 일반적으로 필요한 구성이며, 배치 쿼리 한도(20)에 들어가 인스턴스 1대면 API 호출 1회로 끝납니다.

```yaml
namespaces:
  - name: "ncloud.vserver"
    enabled: true
    regions: ["KR"]
    interval: "Min5"
    aggregations: ["AVG"]          # 아래 metrics의 기본 aggregation
    metrics:
      # CPU
      - name: "avg_cpu_used_rto"     # CPU 사용률 (%)
      - name: "max_cpu_used_rto"     # CPU 사용률 피크 (%)
        aggregations: ["MAX"]
      - name: "load_average_1m"
      - name: "load_average_5m"
      - name: "load_average_15m"

      # 메모리
      - name: "mem_usert"            # 메모리 사용률 (%)
        aggregations: ["AVG", "MAX"]
      - name: "swap_usert"           # swap 사용률 (%)

      # 파일시스템
      - name: "avg_fs_usert"
      - name: "max_fs_usert"
        aggregations: ["MAX"]

      # 네트워크
      - name: "avg_rcv_bps"          # 수신 (bits/sec)
      - name: "avg_snd_bps"          # 송신 (bits/sec)

      # 디스크 I/O
      - name: "avg_read_byt_cnt"
      - name: "avg_write_byt_cnt"
      - name: "avg_read_cnt"
      - name: "avg_write_cnt"
```

기본값 선정 기준:

- **사용률 계열(`%`)은 대시보드와 알람에 바로 쓰입니다** — CPU / 메모리 / 파일시스템 / swap.
- **CPU와 파일시스템은 피크도 함께 봅니다** — 5분 평균만 보면 순간 스파이크와 디스크 full 직전 상황을 놓칩니다. 그래서 `max_*` 메트릭을 `MAX`로 추가했습니다.
- **load average 3종**은 부하 추세(증가/감소)를 판단하는 표준 지표라 함께 넣었습니다.
- **처리량(네트워크 bps, 디스크 bytes/IOPS)은 평균만** 수집합니다. 용량 산정·이상 탐지에는 평균으로 충분하고, 피크까지 넣으면 시리즈가 두 배가 됩니다.
- **패킷 수(pps)는 제외**했습니다. bps로 대부분 판단 가능하고, 필요하면 주석을 해제하면 됩니다.

### 알람용 최소 구성

인스턴스당 4개 시리즈.

```yaml
    aggregations: ["MAX"]
    metrics:
      - name: "max_cpu_used_rto"
      - name: "mem_usert"
      - name: "max_fs_usert"
      - name: "swap_usert"
```

### 디스크 I/O 포함 상세 구성

```yaml
    aggregations: ["AVG"]
    metrics:
      - name: "avg_cpu_used_rto"
        aggregations: ["AVG", "MAX"]
      - name: "mem_usert"
      - name: "avg_fs_usert"
      - name: "avg_*_bps"        # rcv / snd 네트워크
      - name: "avg_*_byt_cnt"    # read / write 디스크
      - name: "load_average_1m"
```

## 작성 규칙

- **와일드카드** — `*`로 여러 메트릭을 한 번에 지정합니다. 대소문자는 무시합니다.
  - `load_average_*` → `load_average_1m`, `load_average_5m`, `load_average_15m`
  - `avg_*` → `avg_`로 시작하는 10개 전부
  - `*_bps` → 네트워크 대역폭 4개
- **aggregation 우선순위** — `metrics[].aggregations` → `namespaces[].aggregations` → API가 해당 interval에 제공하는 전체(=5개 전부).
  - 즉 `aggregations`를 아무 데도 안 쓰면 메트릭 1개가 시리즈 5개가 됩니다. 네임스페이스 레벨에 `["AVG"]` 정도는 지정해두는 것을 권장합니다.
- **`metrics`를 생략하면** 그 네임스페이스의 전체 메트릭을 수집합니다. 목록 파악용으로만 쓰고 운영 설정에는 남기지 마세요.
- **interval** — `Min1`, `Min5`, `Min30`, `Hour2`, `Day1`. 기본값 `Min5`. Prometheus 스크레이프 주기보다 짧게 잡아도 원본 데이터가 그만큼 자주 갱신되지 않으므로 의미가 없습니다.

## 설정 검증

기동 시 로그로 확인할 수 있습니다.

```bash
./ncloud_exporter --config=config.yml --log.level=debug
```

- 오타로 아무것도 매칭되지 않으면:
  ```
  WARN configured metric matched no available metric namespace=ncloud.vserver metric=typo_metric_name
  ```
- 해당 메트릭·interval이 지원하지 않는 aggregation을 지정하면 그 값만 제외하고 경고합니다:
  ```
  WARN some configured aggregations are not supported metric=... using=[AVG] available=[...]
  ```
- 잘못된 `interval` / `aggregation` 값은 기동 자체를 거부합니다:
  ```
  ERROR failed to load config error="namespaces[\"ncloud.vserver\"]: unknown aggregation \"MEDIAN\" (use AVG, MAX, MIN, SUM, COUNT)"
  ```

설정을 바꾼 뒤에는 재시작 없이 `SIGHUP`으로 반영할 수 있습니다.

```bash
kill -HUP $(pgrep -f ncloud_exporter)
```

## 다른 서비스

이 문서는 VPC Server 기준입니다. 다른 네임스페이스(`ncloud.vmysql`, `ncloud.vloadbalancer` 등)의 메트릭명은 위 **1번 방법**으로 `cw_key`를 바꿔가며 조회하면 됩니다. 메트릭명 체계는 서비스마다 다릅니다.
