# NCloud Exporter 설정

네이버 클라우드 플랫폼(NCP)의 Cloud Insight 메트릭을 Prometheus 포맷으로 노출하고, WhaTap OpenAgent가 수집하도록 설정하는 방법을 안내합니다.

> **주의**
> Cloud Insight API 호출량은 `수집 메트릭 수 × 인스턴스 수`에 비례해 증가합니다. 필요한 메트릭만 선택해 수집하시기 바랍니다. 설정 방법은 [수집 메트릭 지정 가이드](metrics.md)를 참고하세요.

---

## 사전 준비 사항

### NCP 계정 및 권한 요구사항

Exporter는 아래 두 종류의 API를 호출합니다. 사용하는 API 키에 두 권한이 모두 있어야 합니다.

| 대상 API | 엔드포인트 | 용도 |
|---|---|---|
| Cloud Insight | `cw.apigw.ntruss.com` | 제품 키(`cw_key`) 조회, 메트릭 정의 조회, 메트릭 데이터 조회 |
| NCloud API Gateway | `ncloud.apigw.ntruss.com` | 리전 목록 조회, 서비스별 인스턴스 목록 조회 |

> **노트**
> 모든 호출은 조회(읽기) 전용입니다. 서브 계정을 사용하는 경우 Cloud Insight 조회 권한과, 모니터링 대상 서비스(VPC Server 등)의 리소스 목록 조회 권한을 부여하시기 바랍니다. 운영 정책상 필요한 최소 권한 정책명은 사내 확정 후 이 문서에 반영해 주세요.

### 필수 정보 수집

| 항목 | 설명 | 발급 위치 |
|---|---|---|
| Access Key | NCP API 인증 키 | 마이페이지 → 계정 관리 → 인증키 관리 |
| Secret Key | NCP API 인증 시크릿 | 위와 동일 (발급 시점에만 확인 가능) |
| 리전 코드 | 수집 대상 리전 (예: `KR`) | 미지정 시 전체 리전 자동 조회 |

> **팁**
> 서브 계정(IAM)을 사용하는 경우 `ncp_iam_`으로 시작하는 키가 발급됩니다.

---

## NCloud Exporter 설치

### Exporter 다운로드

시스템 아키텍처에 맞는 바이너리를 내려받습니다.

```bash
# linux/amd64
wget https://repo.whatap.io/exporter/ncloud_exporter/latest/amd64/ncloud_exporter

# linux/arm64
wget https://repo.whatap.io/exporter/ncloud_exporter/latest/arm64/ncloud_exporter
```

특정 버전을 지정하려면 `latest` 대신 버전을 입력합니다.

```bash
wget https://repo.whatap.io/exporter/ncloud_exporter/1.0.0/amd64/ncloud_exporter
```

> **주의**
> `ncloud_exporter`는 아직 저장소에 배포되지 않았습니다(빌드 파이프라인 구성 중). 배포 전까지는 아래와 같이 소스에서 직접 빌드해 사용하시기 바랍니다.
>
> ```bash
> git clone https://github.com/whatap/exporters.git
> cd exporters/ncloud
> GOOS=linux GOARCH=amd64 go build -o ncloud_exporter .
> ```

### 실행 권한 부여 및 설치

```bash
chmod +x ncloud_exporter
sudo mv ncloud_exporter /usr/local/bin/
sudo mkdir -p /etc/ncloud_exporter
```

---

## config.yml 파일 설정

### 지원 서비스 목록

| Namespace | NCP 서비스 |
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

### config.yml 예시 파일

`/etc/ncloud_exporter/config.yml` 경로에 설정 파일을 생성합니다. 아래는 VPC Server를 수집하는 기본 구성입니다.

```yaml
ncloud:
  access_key: "YOUR_ACCESS_KEY"
  secret_key: "YOUR_SECRET_KEY"

namespaces:
  - name: "ncloud.vserver"
    enabled: true
    regions: ["KR"]          # 빈 배열이면 전체 리전
    interval: "Min5"         # Min1 | Min5 | Min30 | Hour2 | Day1
    aggregations: ["AVG"]    # 아래 metrics의 기본 aggregation
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

  - name: "ncloud.vloadbalancer"
    enabled: false
    regions: []
```

### 설정 항목

| 항목 | 설명 | 기본값 |
|---|---|---|
| `ncloud.access_key` | NCP API Access Key | (필수) |
| `ncloud.secret_key` | NCP API Secret Key | (필수) |
| `namespaces[].name` | 수집할 서비스 네임스페이스 | - |
| `namespaces[].enabled` | 수집 활성화 여부 | `false` |
| `namespaces[].regions` | 대상 리전 (빈 배열이면 전체) | `[]` |
| `namespaces[].interval` | 조회 간격 (`Min1`~`Day1`) | `Min5` |
| `namespaces[].aggregations` | 네임스페이스 기본 aggregation | (API 제공 전체) |
| `namespaces[].metrics` | 수집할 메트릭 목록 | (생략 시 전체) |

> **주의**
> `metrics`를 지정하지 않으면 해당 서비스의 **모든 메트릭 × 모든 aggregation**을 수집합니다. VPC Server의 경우 인스턴스 1대당 125개 시리즈가 생성되므로, 필요한 메트릭만 지정하시기 바랍니다. 위 예시 설정은 인스턴스당 16개 시리즈를 수집합니다.
>
> 서비스별 메트릭 목록과 선택 방법은 [수집 메트릭 지정 가이드](metrics.md)를 참고하세요.

### 자격증명을 환경변수로 주입

설정 파일에 키를 평문으로 저장하지 않으려면 환경변수를 사용합니다. 환경변수가 설정 파일 값보다 우선 적용됩니다.

| 환경변수 | 설명 |
|---|---|
| `NCLOUD_ACCESS_KEY` | `ncloud.access_key` 를 덮어씀 |
| `NCLOUD_SECRET_KEY` | `ncloud.secret_key` 를 덮어씀 |
| `NCLOUD_CW_API_GW` | Cloud Insight 엔드포인트 (기본 `https://cw.apigw.ntruss.com`) |
| `NCLOUD_API_GW` | NCloud API GW 엔드포인트 (기본 `https://ncloud.apigw.ntruss.com`) |

---

## 서비스 등록 및 실행

### 실행 옵션

| 플래그 | 설명 | 기본값 |
|---|---|---|
| `--config` | 설정 파일 경로 | `config.yml` |
| `--web.listen-address` | HTTP 리스닝 주소 | `:9850` |
| `--web.telemetry-path` | 메트릭 엔드포인트 경로 | `/metrics` |
| `--log.level` | 로그 레벨 (`debug`, `info`, `warn`, `error`) | `info` |
| `--log.format` | 로그 포맷 (`text`, `json`) | `text` |
| `--version` | 버전 출력 후 종료 | - |

설치된 바이너리의 버전은 아래로 확인합니다.

```bash
ncloud_exporter --version
# ncloud_exporter version 1.0.0 (released 2026-08-19)
```

### 방법 A: systemd 서비스 등록

```bash
sudo tee /etc/systemd/system/ncloud-exporter.service > /dev/null <<'EOF'
[Unit]
Description=NCloud Exporter
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment="NCLOUD_ACCESS_KEY=YOUR_ACCESS_KEY"
Environment="NCLOUD_SECRET_KEY=YOUR_SECRET_KEY"
ExecStart=/usr/local/bin/ncloud_exporter \
  --config=/etc/ncloud_exporter/config.yml \
  --web.listen-address=:9850
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF
```

등록 후 서비스를 시작합니다.

```bash
sudo systemctl daemon-reload
sudo systemctl enable ncloud-exporter
sudo systemctl start ncloud-exporter
sudo systemctl status ncloud-exporter
```

> **팁**
> 설정 파일을 수정한 뒤에는 재시작 없이 `sudo systemctl reload ncloud-exporter`로 반영할 수 있습니다. 자격증명, 네임스페이스, 메트릭 선택이 다시 로드됩니다. 다만 리스닝 주소와 경로는 실행 인자이므로 변경 시 재시작이 필요합니다.

### 방법 B: nohup 실행

```bash
nohup /usr/local/bin/ncloud_exporter \
  --config=/etc/ncloud_exporter/config.yml \
  --web.listen-address=:9850 > /var/log/ncloud_exporter.log 2>&1 &
```

---

## 메트릭 수집 확인 및 트러블슈팅

### 메트릭 확인

```bash
curl http://localhost:9850/metrics

# 특정 서비스만 확인
curl -s http://localhost:9850/metrics | grep '^ncloud_vserver'
```

### 정상 출력 예시

```
# HELP ncloud_vserver_avg_cpu_used_rto_avg NCloud ncloud.vserver avg_cpu_used_rto (%)
# TYPE ncloud_vserver_avg_cpu_used_rto_avg gauge
ncloud_vserver_avg_cpu_used_rto_avg{instancename="my-server",instanceno="12345678",regioncode="KR",serverinstancetype="Micro"} 0.360744
ncloud_vserver_mem_usert_avg{instancename="my-server",instanceno="12345678",regioncode="KR",serverinstancetype="Micro"} 72.1
```

Exporter 자체 상태는 아래 메트릭으로 확인합니다.

```
ncloud_scrape_errors_total{namespace="ncloud.vserver"} 0
ncloud_scrape_duration_seconds 0.264890458
```

### 메트릭 이름 변환 규칙

```
ncloud_{service}_{metric}_{aggregation}
```

- `service` — 네임스페이스에서 `ncloud.` 접두사를 제거한 값 (`ncloud.vserver` → `vserver`)
- `metric` — Cloud Insight 메트릭명 (`avg_cpu_used_rto`)
- `aggregation` — 소문자 변환된 집계 방식 (`avg`, `max`, `min`, `sum`, `count`)

예시: `ncloud.vserver` + `avg_cpu_used_rto` + `AVG` → `ncloud_vserver_avg_cpu_used_rto_avg`

### 트러블슈팅

| 증상 | 원인 | 해결 방법 |
|---|---|---|
| `failed to initialize cw_keys` 로그와 함께 기동 실패, `status 401 Authentication Failed` | Access Key / Secret Key 오류 또는 권한 부족 | 키 값을 확인하고, 해당 키에 Cloud Insight 조회 권한이 있는지 점검합니다 |
| `read config file: open config.yml: no such file or directory` | 설정 파일 경로 오류 | `--config`에 절대 경로를 지정합니다 |
| `unknown aggregation "..."` / `unknown interval "..."` 로 기동 실패 | 설정 파일의 오타 | `aggregations`는 `AVG`/`MAX`/`MIN`/`SUM`/`COUNT`, `interval`은 `Min1`/`Min5`/`Min30`/`Hour2`/`Day1`만 허용합니다 |
| `WARN configured metric matched no available metric` | 존재하지 않는 메트릭명을 지정 | [메트릭 가이드](metrics.md)에서 실제 메트릭명을 확인합니다 |
| `WARN some configured aggregations are not supported` | 해당 메트릭·interval이 지원하지 않는 aggregation | 로그의 `available` 목록에 있는 값으로 수정합니다 |
| `/metrics`에 `ncloud_` 메트릭이 없음 | 해당 리전에 인스턴스가 없거나 `enabled: false` | `regions` 설정과 `enabled` 값을 확인합니다. 리전을 특정하지 않으려면 `regions: []`로 둡니다 |
| 특정 메트릭만 값이 비어 있음 | Cloud Insight 수집 지연 또는 해당 인스턴스가 미제공 | 서버에 모니터링 에이전트가 설치되어 있는지 확인합니다. 최근 10분 내 데이터가 없으면 시계열이 생성되지 않습니다 |
| `ncloud_scrape_errors_total` 값이 계속 증가 | API 호출 실패 | `--log.level=debug`로 실행해 실패한 네임스페이스와 원인을 확인합니다 |

> **팁**
> 설정을 검증할 때는 `--log.level=debug`로 실행하시기 바랍니다. 선택된 메트릭, 매칭 실패한 설정, API 오류가 모두 로그로 출력됩니다.

---

## OpenAgent 다운로드 및 설정

### 디렉토리 구조

```
/opt/whatap/openagent/
├── openagent           # 실행 파일
├── whatap.conf         # 설정 파일
└── scrape_config.yaml  # 스크래핑 설정 파일
```

### 다운로드 및 권한 설정

```bash
mkdir -p /opt/whatap/openagent
cd /opt/whatap/openagent

# AMD64
wget https://repo.whatap.io/openagent/latest/amd/openagent

# ARM64
wget https://repo.whatap.io/openagent/latest/arm/openagent

chmod +x openagent
```

### scrape_config.yaml 생성

```yaml
features:
  openAgent:
    enabled: true
    targets:
      - targetName: ncloud-exporter
        type: StaticEndpoints
        endpoints:
          - address: "127.0.0.1:9850"
            path: "/metrics"
            scheme: "http"
            interval: "300s"
            metricRelabelConfigs:
              - source_labels: ["__name__"]
                regex: "ncloud_.*"
                action: keep
```

> **주의**
> Cloud Insight의 최소 수집 주기는 **5분**입니다. `interval`을 그보다 짧게 설정해도 동일한 값이 반복 수집될 뿐이며, API 호출량만 증가합니다. `300s` 이상을 권장합니다.

### 설정 요소

| 항목 | 설명 |
|---|---|
| `targetName` | 수집 대상 이름 |
| `type` | `StaticEndpoints` 고정 |
| `address` | Exporter 주소 (`호스트:포트`) |
| `path` | 메트릭 경로 (기본 `/metrics`) |
| `scheme` | `http` 또는 `https` |
| `interval` | 수집 주기 (`300s` 권장) |
| `metricRelabelConfigs` | 수집할 메트릭 필터링 및 레이블 변경 |

### 메트릭 필터링 예시

**Exporter의 모든 메트릭 수집**

Go 런타임 메트릭(`go_*`, `process_*`)까지 포함되므로 권장하지 않습니다.

```yaml
metricRelabelConfigs:
  - source_labels: ["__name__"]
    regex: ".*"
    action: keep
```

**NCloud 메트릭만 수집 (권장)**

```yaml
metricRelabelConfigs:
  - source_labels: ["__name__"]
    regex: "ncloud_.*"
    action: keep
```

**특정 서비스의 CPU·메모리만 수집**

```yaml
metricRelabelConfigs:
  - source_labels: ["__name__"]
    regex: "ncloud_vserver_(avg_cpu_used_rto|mem_usert)_.*"
    action: keep
```

**정적 레이블 추가**

```yaml
metricRelabelConfigs:
  - target_label: "cloud_provider"
    replacement: "ncloud"
    action: replace
```

### 실행

```bash
# 기본 실행
./openagent standalone

# 백그라운드 실행
nohup ./openagent standalone > /dev/null 2>&1 &

# 로그 확인
tail -f logs/whatap-boot-$(date +%Y%m%d).log

# 프로세스 확인 및 종료
ps aux | grep openagent
pkill openagent
```

---

## 참고

- [수집 메트릭 지정 가이드](metrics.md) — 서비스별 메트릭 목록 확인 및 선택 방법
- [NCP API 구조와 디버깅](api.md) — 게이트웨이 구성, 호출 API 목록, 인증 문제 확인 방법
- [README](../README.md) — 설정 항목 전체와 아키텍처
