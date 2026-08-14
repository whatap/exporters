# OpenAgent Exporter 인수인계 문서

> 작성일: 2026-08-12 (최종 수정: 2026-08-14)
> 저장소: https://github.com/whatap/exporters (branch: `main`, ncloud 최신 작업분은 `proto`)
> 현재 릴리스: azure `v1.0.0` (2026-05-12 빌드/배포 완료), ncloud 진행 중

---

## 1. 프로젝트 개요

각 퍼블릭 클라우드의 모니터링 API를 **Prometheus exposition 포맷으로 변환**하여 WhaTap OpenAgent가 수집할 수 있도록 하는 Exporter 저장소.

| 프로젝트 | 대상 | 기반 | 상태 |
|---|---|---|---|
| `azure/` | Azure Monitor | [RobustPerception/azure_metrics_exporter](https://github.com/RobustPerception/azure_metrics_exporter) 포크 | **v1.0.0 배포 완료** |
| `ncloud/` | Naver Cloud Platform (Cloud Insight) | 자체 구현 (와탭 [agency](https://github.com/whatap/agency)로 이관 예정) | 개발 진행 중 |
| `jenkins/` | 빌드/배포 파이프라인 (Jenkinsfile) | - | 운영 중 |

### 저장소 구조

```
exporters/
├── azure/          # Azure Metrics Exporter (Go)
│   ├── main.go, azure.go, utils.go, config/config.go
│   ├── azure-example.yml, azure_{event_hubs,redis_cache,postgresql_flexible_server}.yml
│   ├── build.sh, build.txt, VERSION
│   └── bin/{amd64,arm64}/azure_metrics_exporter    # 빌드 산출물 (Git 커밋됨)
├── ncloud/         # NCloud Exporter (Go)
│   ├── main.go, config/, ncloud/, collector/
│   ├── config.example.yml                          # 설정 템플릿 (→ config.yml로 복사해 사용)
│   ├── config.yml                                  # 실제 설정. .gitignore 대상 (자격증명 포함)
│   ├── README.md, docs/metrics.md, FLOW_DETAIL.txt
│   ├── scripts/ncp-api.sh                          # NCP API 수동 호출 디버깅 스크립트
│   └── bin/{amd64,arm64}/ncloud_exporter
│   ※ build.sh / build.txt / VERSION / Dockerfile 미존재 → 6장 참고
└── jenkins/
    ├── Jenkinsfile.build
    ├── Jenkinsfile.deploy
    └── README.md
```

---

## 2. CI/CD (Jenkins)

DevOps 담당: **신한국** 님

| 역할 | Job URL | Jenkinsfile | 실행 노드 |
|---|---|---|---|
| 빌드 | [01.20.Prod_openagent_exporter_build(agent)](https://jenkins.whatap.io/job/01.20.Prod_openagent_exporter_buildagent)/) | `jenkins/Jenkinsfile.build` | `k8s-agent-node-x86` (10.21.11.132) |
| 배포 | [01.20.Prod_openagent_exporter_deploy(agent)](https://jenkins.whatap.io/job/01.20.Prod_openagent_exporter_deployagent)/) | `jenkins/Jenkinsfile.deploy` | `master` |

- 빌드/배포 **분리** → 독립 실행 가능

### 2-1. Build Pipeline 매개변수

| 이름 | 타입 | 필수 | 설명 |
|---|---|---|---|
| `PROJECT` | choice | O | `azure` / `ncloud` / `all` |
| `VERSION` | string | O | SemVer 형식(`1.0.0`), 정규식 `^\d+\.\d+\.\d+$` 검증 |

**실행 흐름**

```
Validate Parameters → Checkout → Update build.txt → Build → Archive Artifacts → Push to GitHub
```

1. `VERSION` SemVer 검증 (미입력 시 실패)
2. SCM 설정 브랜치 체크아웃
3. `{project}/build.txt`에 `version`, `release_date`(빌드 당일) 기록
4. `{project}/build.sh` 실행 → linux amd64/arm64 크로스 컴파일
5. Jenkins 아티팩트로 `bin/**`, `build.txt` 아카이브
6. `build.txt` + `bin/` 커밋 후 `v{VERSION}` 태그와 함께 GitHub push

> 빌드 산출물(바이너리)을 **Git에 그대로 커밋**하는 구조. 배포 파이프라인이 Git 체크아웃만으로 동작하게 하기 위한 설계.

### 2-2. Deploy Pipeline 매개변수

| 이름 | 타입 | 필수 | 설명 |
|---|---|---|---|
| `PROJECT` | choice | O | `azure` / `ncloud` / `all` |
| `VERSION` | string | - | 빈 값이면 Git의 `build.txt`에서 자동 조회 |
| `BUILD_JOB_NUMBER` | string | - | 빈 값이면 Git에서 체크아웃 |

**실행 흐름**

```
Get Artifacts → Resolve Version → Verify Artifacts → Upload to S3 → Verify S3 Upload
```

### 2-3. 배포 경로 (repo.whatap.io)

S3 버킷 `repo.whatap.io`의 `exporter/` 하위에 **서비스별로** 업로드.

```
s3://repo.whatap.io/exporter/
└── {exporter_name}/
    ├── {version}/
    │   ├── amd64/{exporter_name}
    │   └── arm64/{exporter_name}
    └── latest/              ← 배포 시 항상 최신 버전으로 덮어씀
        ├── amd64/{exporter_name}
        └── arm64/{exporter_name}
```

| PROJECT | exporter_name |
|---|---|
| `azure` | `azure_metrics_exporter` |
| `ncloud` | `ncloud_exporter` |

예시 (`PROJECT=azure`, `VERSION=1.0.0`):

```
s3://repo.whatap.io/exporter/azure_metrics_exporter/1.0.0/amd64/azure_metrics_exporter
s3://repo.whatap.io/exporter/azure_metrics_exporter/1.0.0/arm64/azure_metrics_exporter
s3://repo.whatap.io/exporter/azure_metrics_exporter/latest/amd64/azure_metrics_exporter
s3://repo.whatap.io/exporter/azure_metrics_exporter/latest/arm64/azure_metrics_exporter
```

### 2-4. 사용 시나리오

**신규 버전 빌드 + 배포**
```
1) Build  : PROJECT=azure, VERSION=1.0.0
2) 빌드 성공 확인
3) Deploy : PROJECT=azure, VERSION=1.0.0
   (또는 BUILD_JOB_NUMBER=빌드번호 지정)
```

**Git 최신 빌드 배포 (VERSION 생략)**
```
Deploy : PROJECT=azure, VERSION=(빈 값)
→ azure/build.txt의 version을 읽어 배포
```

**전체 프로젝트 빌드**
```
Build : PROJECT=all, VERSION=1.0.0
```

---

## 3. Azure Metrics Exporter

### 3-1. 별도 포크 관리 사유

- 원본 [RobustPerception/azure_metrics_exporter](https://github.com/RobustPerception/azure_metrics_exporter) → 2020년 이후 사실상 유지보수 중단
- 실제 운영 환경 적용 시 **실행 중 오류 및 운영상 제약 존재**
- → 별도 포크 후 수정하는 방향으로 진행 (상세 개선 내역은 4장)

### 3-2. 빌드 방법 (로컬)

```bash
cd azure
cat build.txt            # version=1.0.0 / release_date=2026-05-12
./build.sh               # → bin/amd64, bin/arm64
```

### 3-3. 운영 참고

- 기본 리스닝 포트: `:9276` (`--web.listen-address`)
- 기본 설정 파일: `azure.yml` (`--config.file`)
- Azure API 읽기 제한: **15,000 req/hour** → `scrape_interval` 60s 이상 권장
- 필요 권한: 대상 구독에 **Monitoring Reader** 역할 (App Registration 또는 Managed Identity)
- 진단용 플래그: `--list.definitions`(사용 가능 메트릭 목록), `--list.namespaces`

---

## 4. Azure 개선 내역

커밋 이력(`1a40a09` ~ `v1.0.0`) 기준, 원본 대비 수정 사항.

| # | 개선 항목 | 커밋 |
|---|---|---|
| 1 | aggregation 설정 유연화 | `11fce2d` |
| 2 | CPU/메모리 과다 사용 해소 | `99b1196` |
| 3 | 자격증명 환경변수 지원 | `9c0c00c` |
| 4 | 버전 정보 주입 및 빌드 표준화 | `fc47066`, `e9297ec`, `b646465` |
| 5 | 메트릭 20개 초과 시 자동 batch 분할 | `fa5065b` (v1.0.0) |

### 1. aggregation 설정 유연화

**문제** — 원본은 aggregation을 리소스 단위로만 지정 가능. 같은 리소스에서 메트릭마다 다른 aggregation(CPU는 Average, IOPS는 Total/Maximum 등)을 쓰려면 동일 리소스를 target으로 중복 정의해야 함.

**해결**
- `config.Metric`에 `aggregations` 필드 추가 → metric 단위 지정 지원
- `groupMetricsByAggregation()` 신설 → 같은 aggregation 집합끼리 묶어 1회 API 요청으로 병합
- `targets` / `resource_groups` / `resource_tags` 전 경로 동일 적용
- `Validate()`에서 metric 단위 값도 검증 → 오타 시 기동 시점에 실패

**적용 우선순위** — ① metric의 `aggregations` → ② 리소스의 `aggregations` → ③ 미지정 시 전체(`Total`/`Average`/`Minimum`/`Maximum`)

### 2. CPU/메모리 과다 사용 해소

**문제** — 2019년 pre-release 의존성에 고정(`go 1.13`, `client_golang v1.1.1-0.20190913...`), vendor 디렉터리 통째 커밋. 장시간 구동 시 CPU/메모리 사용량 비정상 증가.

**해결**
- Go `1.13` → `1.21`, `client_golang` `v1.1.x` → `v1.20.5`, `yaml.v2` `v2.2.2` → `v2.4.0`
- `vendor/` 전체 삭제(약 2만 라인) → go modules 전환
- `version.NewCollector` 등록 제거 → 수집 대상과 무관한 시계열 제거

### 3. 자격증명 환경변수 지원

**문제** — `client_secret` 포함 자격증명을 YAML 평문으로만 설정 가능. 컨테이너/K8s Secret 배포와 부적합, 설정 파일이 저장소·이미지에 유입될 위험.

**해결** — `Credentials.applyEnvOverrides()` 추가. 아래 환경변수가 있으면 YAML 값을 덮어씀(환경변수 우선).

```
AZURE_SUBSCRIPTION_ID  AZURE_TENANT_ID  AZURE_CLIENT_ID  AZURE_CLIENT_SECRET
```

### 4. 버전 정보 주입 및 빌드 표준화

**문제** — Makefile + promu 기반이라 사내 Jenkins 연동이 어렵고, 배포된 바이너리의 버전 확인 수단 없음.

**해결**
- `-ldflags`로 `main.version`, `main.releaseDate` 주입 + `--version` 플래그 (미주입 시 `dev`/`unknown`)
- `build.txt`(`version=`, `release_date=`)를 읽는 `build.sh` 도입 → Jenkins는 `build.txt`만 갱신
- 산출물 경로를 `bin/{amd64,arm64}/`로 확정, darwin 빌드 제외(배포 대상은 linux 뿐)

### 5. 메트릭 20개 초과 시 자동 batch 분할

**문제** — Azure Monitor REST API는 요청당 리소스 1개에 최대 20개 메트릭만 허용. 원본은 설정된 메트릭을 그대로 이어붙여 요청하므로 **21개 이상이면 런타임 API 오류 → 해당 리소스 메트릭 전체 누락**. 회피하려면 사용자가 target을 20개씩 직접 분리해야 함.

**해결** — `groupMetricsByAggregation()`에서 aggregation 그룹 생성 후 각 그룹을 20개 단위로 자동 분할. 메트릭 개수와 무관하게 설정 수동 분할 불필요.

> aggregation을 다르게 지정하면 집합별로 그룹이 나뉘고, 각 그룹이 다시 20개 단위로 분할됨.

### 기타

- `92645d7` — 로컬 테스트용으로 바뀌어 있던 기본 설정 파일명 원복 (`azure_event_hubs.yml` → `azure.yml`)

---

## 5. NCloud (Naver Cloud Platform) Exporter

### 5-1. 진행 방향

- 현재 저장소의 `ncloud/` 코드는 **프로토타입**
- 최종적으로 **와탭 에이전시([whatap/agency](https://github.com/whatap/agency))의 코드를 수정하는 형태로 exporter를 만드는 과정 진행 중**
- azure처럼 외부 프로젝트를 포크하는 방식이 아니라, 사내 agency 코드베이스에 통합하는 방향

### 5-2. 우선 개발 대상 — 와탭 NCP 모니터링 서비스 단위 API 조회

와탭이 현재 제공 중인 NCP 모니터링의 **서비스 단위**는 아래와 같음.
**이 목록에 해당하는 서비스들의 API 조회 개발이 최우선 선행 과제.**

| # | 와탭 서비스명 | NCP 서비스 |
|---|---|---|
| 1 | Ncloud VServer Monitoring | VPC 서버 |
| 2 | Ncloud VLoadbalancer Monitoring | VPC 로드밸런서 |
| 3 | Ncloud VLBTargetGroup Monitoring | 로드밸런서 타겟 그룹 |
| 4 | Ncloud VAutoScalingGroup Monitoring | 오토스케일링 그룹 |
| 5 | Ncloud VCloudDBMySQL Monitoring | Cloud DB for MySQL |
| 6 | Ncloud VCloudDBPostgreSQL Monitoring | Cloud DB for PostgreSQL |
| 7 | Ncloud VCloudDBMSSQL Monitoring | Cloud DB for MSSQL |
| 8 | Ncloud VCloudDBRedis Monitoring | Cloud DB for Redis |
| 9 | Ncloud VCloudMongoDB Monitoring | Cloud DB for MongoDB |
| 10 | Ncloud VCloudHadoop Monitoring | Cloud Hadoop |
| 11 | Ncloud VCloudKubernetes Monitoring | Ncloud Kubernetes Service |

- 와탭 수집 주기: 서비스별 **5분** (콘솔에서 서비스 단위 활성화/비활성화)
- 각 서비스마다 **① 인스턴스(리소스) 목록 조회 API → ② Cloud Insight 메트릭 조회 API** 두 갈래가 필요
- 현재 프로토타입 지원 범위와의 차이
  - 미구현: `VLBTargetGroup`
  - 목록에 없으나 프로토타입에 존재: `vsearchengine` (Search Engine Service) → 유지 여부 확인 필요

### 5-3. 프로토타입 현황

Cloud Insight API 기반으로 아래 namespace 구현:

`ncloud.vserver`, `vloadbalancer`, `vautoscaling`, `vmysql`, `vpostgresql`, `vredis`, `vmongodb`, `vmssql`, `vnks`, `vsearchengine`, `vhadoop`

**수집 흐름**

```
Prometheus ──(scrape)──> ncloud_exporter(:9850/metrics)
                              ├─ Cloud Insight API (cw.apigw.ntruss.com)
                              │   ├─ Product Key 조회 (cw_key 매핑)
                              │   ├─ 메트릭 정의 동적 조회 (5분 캐시)
                              │   └─ 메트릭 데이터 배치 쿼리 (최대 20개/요청)
                              └─ NCloud API GW (ncloud.apigw.ntruss.com)
                                  ├─ 리전 목록 조회
                                  └─ 인스턴스 목록 조회 (5분 캐시)
```

- scrape 간격 5분, query window 10분 (Cloud Insight 수집 지연 대비 안전 마진). 항상 최신 datapoint 1개만 사용 → 중복 없음
- 메트릭 형식: `ncloud_{service}_{metric}_{aggregation}`
- 환경변수: `NCLOUD_ACCESS_KEY`, `NCLOUD_SECRET_KEY`, `NCLOUD_CW_API_GW`, `NCLOUD_API_GW`
- `SIGHUP` 시그널로 설정 리로드 지원 (자격증명·namespace·메트릭 선택 반영. 리스닝 주소/경로는 실행 인자라 재시작 필요)
- 상세: `ncloud/README.md`, `ncloud/docs/metrics.md`, `ncloud/FLOW_DETAIL.txt`

**실행 인자**

| 플래그 | 설명 | 기본값 |
|---|---|---|
| `--config` | 설정 파일 경로 | `config.yml` |
| `--web.listen-address` | HTTP 리스닝 주소 | `:9850` |
| `--web.telemetry-path` | 메트릭 엔드포인트 경로 | `/metrics` |
| `--log.level` / `--log.format` | 로그 레벨 / 포맷 | `info` / `text` |

### 5-4. 수집 메트릭 선택 (2026-08-13 추가, `proto` 브랜치)

**문제** — 초기 구현은 namespace를 활성화하면 Cloud Insight가 제공하는 **모든 메트릭 × 모든 aggregation**을 수집. VPC Server 기준 인스턴스 1대당 125 시리즈(25 메트릭 × 5 aggregation)가 생성되고, 배치 쿼리 한도(20개/요청) 때문에 스크레이프마다 API 호출 7회 발생. 인스턴스 수만큼 그대로 곱해짐.

**해결** — Azure exporter의 개선 #1과 동일한 방향으로, CloudWatch Exporter의 `metrics:` 블록과 같은 방식의 선택 기능 추가.

- `namespaces[].metrics[]` — 수집할 메트릭 지정. `*` 와일드카드 지원, 대소문자 무시. **생략 시 기존과 동일하게 전체 수집**
- `namespaces[].aggregations` / `metrics[].aggregations` — aggregation 지정
- 적용 우선순위: ① `metrics[].aggregations` → ② `namespaces[].aggregations` → ③ 미지정 시 API가 해당 interval에 제공하는 전체
- `namespaces[].interval` — 조회 간격 (`Min1`~`Day1`, 기본 `Min5`)
- 오설정 방어: 잘못된 `interval`/`aggregation`은 **기동 시점에 거부**, 매칭되지 않는 메트릭명(오타)과 미지원 aggregation은 `WARN` 로그

동시에 `exporter:` 설정 섹션을 제거하고 실행 인자로 이관. `scrape_interval`은 **어디서도 읽지 않는 죽은 설정**이었고, 스크레이프 주기는 Prometheus가 결정하므로 exporter의 설정 항목이 아님.

| 이전 (설정 파일) | 이후 (실행 인자) |
|---|---|
| `exporter.listen_address` | `--web.listen-address` |
| `exporter.metrics_path` | `--web.telemetry-path` |
| `exporter.scrape_interval` | 삭제 |

기본 설정 파일명도 `ncloud.yaml` → `config.yml`로 변경.

### 5-5. 기본 설정 구성 (`config.example.yml`)

배포 시 이 파일을 `config.yml`로 복사해 자격증명만 채우면 바로 동작하도록 구성. 자격증명은 환경변수(`NCLOUD_ACCESS_KEY` / `NCLOUD_SECRET_KEY`)로 주입하는 것을 권장하며, 환경변수가 YAML 값보다 우선함.

```yaml
ncloud:
  access_key: "YOUR_ACCESS_KEY"    # 환경변수 NCLOUD_ACCESS_KEY로 대체 가능
  secret_key: "YOUR_SECRET_KEY"    # 환경변수 NCLOUD_SECRET_KEY로 대체 가능

# 리스닝 주소/경로는 설정 파일이 아니라 실행 인자로 지정
#   ./ncloud_exporter --config config.yml --web.listen-address=:9850

namespaces:
  # VPC Server — 기본값은 서버 모니터링에 일반적으로 필요한 16개 시리즈
  # (필터 없이 전체 수집하면 인스턴스당 125개)
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
      - name: "load_average_1m"      # CPU load 1분
      - name: "load_average_5m"      # CPU load 5분
      - name: "load_average_15m"     # CPU load 15분

      # 메모리
      - name: "mem_usert"            # 메모리 사용률 (%)
        aggregations: ["AVG", "MAX"]
      - name: "swap_usert"           # swap 사용률 (%)

      # 파일시스템
      - name: "avg_fs_usert"         # 파일시스템 사용률 (%)
      - name: "max_fs_usert"         # 파일시스템 사용률 피크 (%)
        aggregations: ["MAX"]

      # 네트워크
      - name: "avg_rcv_bps"          # 수신 (bits/sec)
      - name: "avg_snd_bps"          # 송신 (bits/sec)

      # 디스크 I/O
      - name: "avg_read_byt_cnt"     # 읽기 (bytes/sec)
      - name: "avg_write_byt_cnt"    # 쓰기 (bytes/sec)
      - name: "avg_read_cnt"         # 읽기 횟수 (num/sec)
      - name: "avg_write_cnt"        # 쓰기 횟수 (num/sec)

      # 필요 시 주석 해제
      # - name: "avg_rcv_pps"        # 수신 패킷 (packets/sec)
      # - name: "avg_snd_pps"        # 송신 패킷 (packets/sec)
      # - name: "max_*_bps"          # 네트워크 순간 피크
      #   aggregations: ["MAX"]

  # 아래 서비스는 metrics 미지정 상태 → enabled: true로 켜면 전체 수집됨
  - name: "ncloud.vloadbalancer"
    enabled: false
    regions: []
  # ... vautoscaling / vmysql / vpostgresql / vredis / vmongodb
  #     vmssql / vnks / vsearchengine / vhadoop 동일 형식으로 나열
```

**기본값 요약**

| 항목 | 값 | 비고 |
|---|---|---|
| 활성 namespace | `ncloud.vserver` 만 `enabled: true` | 나머지 10개는 비활성 |
| 리전 | `KR` | 빈 배열이면 전체 리전 자동 조회 |
| interval | `Min5` | Cloud Insight 최소 수집 주기와 일치 |
| 기본 aggregation | `AVG` | 메트릭별 지정이 우선 |
| 수집 메트릭 | 15개 → **16 시리즈/인스턴스** | 배치 한도(20) 이내 → API 호출 1회 |

### 5-6. 기본 메트릭 선정 내역

**15개 메트릭 → 인스턴스당 16 시리즈**. 배치 한도(20) 안에 들어가 인스턴스 1대 기준 스크레이프당 API 호출 1회로 끝남.

| 그룹 | 메트릭 | aggregation | 단위 |
|---|---|---|---|
| CPU | `avg_cpu_used_rto` | AVG | % |
| | `max_cpu_used_rto` | MAX | % |
| | `load_average_1m` / `5m` / `15m` | AVG | - |
| 메모리 | `mem_usert` | AVG, MAX | % |
| | `swap_usert` | AVG | % |
| 파일시스템 | `avg_fs_usert` | AVG | % |
| | `max_fs_usert` | MAX | % |
| 네트워크 | `avg_rcv_bps` / `avg_snd_bps` | AVG | bits/sec |
| 디스크 I/O | `avg_read_byt_cnt` / `avg_write_byt_cnt` | AVG | bytes/sec |
| | `avg_read_cnt` / `avg_write_cnt` | AVG | num/sec |

**선정 기준**

- 사용률(%) 4종(CPU/메모리/파일시스템/swap)은 대시보드·알람에 바로 쓰이므로 전부 포함
- CPU·파일시스템은 피크도 함께 수집 — 5분 평균만으로는 순간 스파이크와 디스크 full 직전을 놓침
- 처리량(bps, bytes/sec, IOPS)은 평균만 수집. 피크까지 넣으면 시리즈가 두 배
- 패킷 수(pps)는 제외하고 설정 파일에 주석으로 남김

> **주의 — `avg_` / `max_` 접두사는 config의 `aggregations`와 별개.** NCP 서버 에이전트가 수집 주기 내 값을 미리 집계해 **서로 다른 메트릭**으로 제공하는 것. `avg_cpu_used_rto` + `MAX`는 "평균값들 중 조회 구간 최댓값"이지 순간 피크가 아니므로, 피크가 필요하면 `max_cpu_used_rto`를 써야 함.

**메트릭 목록 확인 방법** — `ncloud/docs/metrics.md`에 VPC Server 27개 메트릭 전체 표(단위·설명·지원 aggregation)와 조회 절차 정리. 다른 서비스는 `cw_key`를 바꿔가며 아래로 조회.

```bash
# 1) 제품 키 조회 → 2) 메트릭 목록 조회
./scripts/ncp-api.sh GET  cw "/cw_fea/real/cw/api/schema/system/list"
./scripts/ncp-api.sh POST cw "/cw_fea/real/cw/api/rule/group/metric/search" \
  '{"prodKey":"460438474722512896","query":""}'    # 460438474722512896 = Server(VPC)
```

> 나머지 10개 namespace는 아직 기본 메트릭 미선정 상태. 활성화 시 전체 수집되므로, 서비스별로 위 절차를 거쳐 `config.example.yml`을 채워야 함.

### 5-7. 디버깅 도구 — `ncloud/scripts/ncp-api.sh`

- NCP signature v2(HMAC-SHA256)를 직접 생성해 API 호출 가능
- 메트릭 정의·응답 형태 확인 시 유용
- 아직 Git 미커밋(untracked) 상태

```bash
export NCLOUD_ACCESS_KEY=... NCLOUD_SECRET_KEY=...
./ncp-api.sh GET vpc "/vserver/v2/getRegionList?responseFormatType=json"
./ncp-api.sh GET cw  "/cw_fea/real/cw/api/schema/system/list"
DEBUG=1 ./ncp-api.sh ...   # 서명 메시지 덤프
```

---

## 6. 미완료 항목 / 인수 시 우선 확인

인계받는 사람이 **가장 먼저 봐야 할 목록**. 우선순위 순.

### 6-1. 즉시 조치 필요

| # | 항목 | 현황 | 영향 |
|---|---|---|---|
| 1 | **NCP 자격증명이 Git 이력에 노출** | `ncloud/ncloud.yaml`에 실 IAM 키가 커밋된 상태로 존재. `proto` 브랜치에서 추적 해제 + `.gitignore` 등록했으나 **과거 커밋에는 그대로 남아 있음** | 저장소 접근 권한자 전원이 키 열람 가능 → **키 재발급 필요**. 이력 제거는 히스토리 재작성이 필요해 별도 판단 |
| 2 | **ncloud 빌드 파이프라인 미지원** | Jenkins Build Job의 `PROJECT` 선택지에 `ncloud`가 있으나, 파이프라인이 실행하는 `ncloud/build.sh`가 **존재하지 않음**. `build.txt` / `VERSION`도 없음 | `PROJECT=ncloud` 또는 `all` 실행 시 Build 단계 실패. `azure/build.sh`를 참고해 작성 필요 |
| 3 | **ncloud 버전 정보 미주입** | `main.version` / `main.releaseDate` 변수와 `--version` 플래그 없음 (azure 개선 #4는 ncloud에 미적용) | 배포된 바이너리의 버전 확인 불가. 2번과 함께 처리 |

### 6-2. 저장소 정리

| 항목 | 현황 |
|---|---|
| `ncloud/ncloud_exporter` | **macOS(arm64) 바이너리가 Git에 커밋되어 있음** (13MB). 배포 대상은 linux뿐이므로 삭제 대상 |
| `ncloud/scripts/` | `ncp-api.sh`(디버깅 도구), `test2.txt`(VPC Server 메트릭 정의 응답 덤프) 모두 **untracked**. 문서가 참조하는 도구이므로 커밋 권장 |
| `vcp.txt` (저장소 루트) | `/metrics` 응답 덤프. 커밋 대상 아님 |
| `ncloud/.idea/` | IDE 설정 일부가 커밋되어 있음 (`.gitignore`에 `.idea/` 등록 이전 파일) |

### 6-3. 문서-구현 불일치

- `ncloud/FLOW_DETAIL.txt` **9장 "Docker 실행 흐름"** — `ncloud/Dockerfile`이 실제로 존재하지 않음. 문서가 계획 단계 내용을 서술 중
- azure는 `Dockerfile`이 존재하나 CI/CD는 바이너리 S3 업로드 방식이라 사용되지 않음 → 컨테이너 배포 계획 유무 확인 필요

### 6-4. 코드 품질 / 알려진 제약

| 항목 | 내용 |
|---|---|
| 테스트 부재 | `ncloud/`에 테스트 코드 없음 (azure는 `utils_test.go` 존재). 최소한 config 파싱·메트릭명 정규화·와일드카드 매칭은 테스트 가치 있음 |
| 배치 응답 순서 가정 | `collector.go`의 `flushBatch()`가 `QueryDataMulti` 응답이 **요청과 동일한 순서·개수**로 온다고 가정하고 index로 매핑. API가 순서를 보장하지 않거나 일부 항목을 누락하면 **메트릭이 뒤섞일 수 있음** |
| dimension 단일 지원 | `instanceNo` 하나만 dimension으로 지정. 디스크·파일시스템은 원래 디바이스별 데이터지만 인스턴스 단위 집계값만 수집됨. 디바이스별 분리 미지원 |
| 리전 병렬 처리 없음 | `regions`가 여러 개면 순차 호출. 전체 리전 대상 시 스크레이프 지연 가능 |
| `ncloud_scrape_errors_total` | 이름은 `_total`(counter 관례)이나 실제로는 **스크레이프별 Gauge**로 리셋됨 |
| 미구현 서비스 | `VLBTargetGroup` (와탭 서비스 목록 3번). 반대로 `vsearchengine`은 와탭 목록에 없으나 구현되어 있음 → 유지 여부 확인 필요 |

### 6-5. 운영 정보 (미정리)

인계 전 확정이 필요한 항목.

- **Prometheus / OpenAgent 수집 설정 예시** — scrape_interval 권장값(Cloud Insight 최소 주기가 5분이므로 300s 이상), timeout, 대상 job 설정 샘플
- **NCP 계정 권한** — exporter 동작에 필요한 최소 IAM 권한 정책 (현재 사용 중인 키의 권한 범위 미문서화)
- **NCP API 호출 한도** — azure는 15,000 req/hour로 문서화되어 있으나 Cloud Insight 측 한도 미확인. 인스턴스 수 × 메트릭 수에 따라 호출량이 선형 증가하므로 확인 필요
- **운영 배포 형태** — systemd / 컨테이너 / OpenAgent 동봉 중 무엇인지, 설정 파일 배치 경로와 자격증명 주입 방식(환경변수 권장)
- **장애 대응** — 대표적인 실패 유형(401 인증 실패, cw_key 조회 실패로 기동 중단, 빈 datapoint)과 확인 절차

---

## 7. 담당/연락

| 구분 | 담당 |
|---|---|
| Jenkins 파이프라인 / 인프라 | 신한국 (DevOps) |
| Exporter 개발 | 이승훈 |

## 8. 참고 링크

- 저장소: https://github.com/whatap/exporters
- Azure exporter 원본: https://github.com/RobustPerception/azure_metrics_exporter
- 와탭 에이전시 (NCP 작업 대상): https://github.com/whatap/agency
- Build Job: https://jenkins.whatap.io/job/01.20.Prod_openagent_exporter_buildagent)/
- Deploy Job: https://jenkins.whatap.io/job/01.20.Prod_openagent_exporter_deployagent)/
- Azure Monitor REST API 제한: https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/request-limits-and-throttling
