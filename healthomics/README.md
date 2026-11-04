# HealthOmics Exporter

AWS HealthOmics의 run·task 상태와 AWS Run Analyzer 결과(비용, 과할당)를 Prometheus 형식 `/metrics`로 제공합니다.
OpenAgent가 이 엔드포인트를 스크랩하면 수집 서버 변경 없이 OpenMetrics로 저장됩니다.

run·task의 CPU, 메모리, I/O 사용량(run metrics)은 CloudWatch에 OpenTelemetry 지표로만 있어서 이 Exporter가 아니라 `../cloudwatch-promql` Exporter가 내보냅니다.
두 Exporter의 지표는 `run_id`, `task_id`, `workflow_id` 라벨이 같아서 화면 PromQL에서 결합할 수 있습니다.

## 실행

```sh
pip install aws-healthomics-tools==0.14.0   # Run Analyzer CLI (aws-healthomics-tools)
cd healthomics && go build -o bin/healthomics_exporter .
./bin/healthomics_exporter
curl --fail http://127.0.0.1:9109/metrics
```

- 기본 listen: `127.0.0.1:9109`. 다른 호스트의 OpenAgent가 수집하면 `--web.listen-address=<주소>:9109`로 바꾸고 접근 범위를 제한하세요. TLS/HTTP 인증은 내장하지 않습니다.
- AWS 인증: AWS SDK 기본 credential chain. EC2에서는 인스턴스 프로파일의 IAM 역할을 사용합니다. Run Analyzer CLI도 같은 자격 증명을 씁니다.
- 리전: AWS SDK 환경변수/프로필, 없으면 EC2 IMDS. 필요할 때만 `--region`을 지정합니다.
- Run Analyzer는 `aws-healthomics-tools run_analyzer <runId> --region <region> -o <파일>`로 실행합니다. 0.14.0에서 확인했습니다. 문서에 나오는 `omics-run-analyzer` 실행 파일은 0.14.0에 없습니다.

| 플래그 | 기본값 | 설명 |
| --- | --- | --- |
| `--interval` | `60s` | HealthOmics API 조회 주기 |
| `--retention` | `24h` | 끝난 run을 끝난 시각부터 계속 내보내는 기간 |
| `--analyzer.enabled` | `true` | 끝난 run에 Run Analyzer 실행 |
| `--analyzer.command` | `aws-healthomics-tools` | Run Analyzer 실행 파일 |
| `--analyzer.timeout` | `10m` | Run Analyzer 1회 제한 시간 |
| `--analyzer.work-dir` | 시스템 임시 디렉터리 | 임시 CSV 위치. 분석이 끝나면 지웁니다 |

## IAM 권한 (읽기 전용)

| 용도 | action |
| --- | --- |
| run·task 상태 | `omics:ListRuns`, `omics:ListRunTasks`, `omics:GetRun`, `omics:GetWorkflow` |
| Run Analyzer: run manifest | `logs:DescribeLogStreams`, `logs:GetLogEvents` (`/aws/omics/WorkflowLog`) |
| Run Analyzer: 단가 | `pricing:DescribeServices`, `pricing:GetProducts` |

Pricing API는 `us-east-1` 엔드포인트(`api.pricing.us-east-1.amazonaws.com`)만 사용합니다. 서울 리전 VPC에서도 이 엔드포인트로 나가는 경로가 필요합니다.

## 지표

### run·task 상태 (HealthOmics API)

대상은 진행 중인 run 전체와, 끝난 지 `--retention` 이내인 run(COMPLETED, FAILED, CANCELLED)입니다.

| 지표 | 라벨 | 의미 |
| --- | --- | --- |
| `aws_omics_run_info` | `run_id`, `run_name`, `workflow_id`, `workflow_name`, `status`, `storage_type`, `failure_reason`(FAILED만) | 값 1 |
| `aws_omics_run_wait_seconds` | `run_id`, `workflow_id` | 생성부터 시작까지. 아직 시작 전이면 지금까지 |
| `aws_omics_run_duration_seconds` | `run_id`, `workflow_id` | 시작부터 종료까지. 진행 중이면 지금까지 |
| `aws_omics_task_info` | `run_id`, `task_id`, `task_name`, `workflow_id`, `status`, `instance_type` | 값 1 |
| `aws_omics_task_wait_seconds` | `run_id`, `task_id`, `workflow_id` | 생성부터 시작까지(자원 대기). 아직 시작 전이면 지금까지 |
| `aws_omics_task_duration_seconds` | `run_id`, `task_id`, `workflow_id` | 시작부터 종료까지. 실행 중이면 지금까지 |

### Run Analyzer 결과 (끝난 run별)

run이 끝나고 2분 뒤(manifest 로그가 run 종료 후 기록됨) 한 번 분석합니다. 실패하면 5분 간격으로 최대 3번 시도합니다.

| 지표 | 라벨 | 의미 |
| --- | --- | --- |
| `aws_omics_run_analysis_success` | `run_id`, `workflow_id` | 분석 성공 1, 실패 0 |
| `aws_omics_run_estimated_cost_usd` | `run_id`, `workflow_id` | task 예상 비용 합(분석 시점 정가) |
| `aws_omics_run_minimum_cost_usd` | `run_id`, `workflow_id` | 모든 task가 맞는 최소 인스턴스를 썼을 때의 비용 합 |
| `aws_omics_run_cpu_utilization_ratio`, `aws_omics_run_memory_utilization_ratio` | `run_id`, `workflow_id` | run 사용률 |
| `aws_omics_task_estimated_cost_usd`, `aws_omics_task_minimum_cost_usd` | `run_id`, `task_id`, `task_name`, `workflow_id` | task 비용 |
| `aws_omics_task_cpu_utilization_ratio`, `aws_omics_task_memory_utilization_ratio` | 위와 같음 | task 사용률 |
| `aws_omics_task_instance_type_info` | 위 + `reserved_instance_type`, `minimum_instance_type` | 값 1. 둘이 다르면 과할당 |

### 워크플로별 누적 (counter)

| 지표 | 라벨 | 의미 |
| --- | --- | --- |
| `aws_omics_workflow_analyzed_runs_total` | `workflow_id` | 분석해서 센 run 수 |
| `aws_omics_workflow_estimated_cost_usd_total` | `workflow_id` | 센 run의 예상 비용 합 |
| `aws_omics_workflow_minimum_cost_usd_total` | `workflow_id` | 센 run의 최소 비용 합. 예상과의 차이가 절감 가능액 |
| `aws_omics_workflow_info` | `workflow_id`, `workflow_name` | 값 1 |

- 카운터는 Exporter가 처음 본 워크플로부터 0으로 시작합니다. 화면은 `increase()`로 그립니다.
- Exporter가 시작할 때 이미 끝나 있던 run은 분석해서 run별 지표로는 내보내지만 카운터에는 넣지 않습니다. 재시작 전에 셌는지 알 수 없어서, 중복으로 세지 않기 위해서입니다. 그래서 Exporter가 꺼져 있던 동안 끝난 run은 카운터에서 빠집니다. 그 기간 비용은 run별 지표(`aws_omics_run_estimated_cost_usd`)로 봅니다.

### 자체 지표

| 지표 | 의미 |
| --- | --- |
| `healthomics_exporter_poll_success` | 마지막 HealthOmics API 조회 성공 여부 |

## OpenAgent 등록

OpenAgent의 `scrape_config.yaml`에서 기존 `features.openAgent.targets` 목록에 항목을 하나 추가합니다(OpenAgent `docs/ec2-instance-state-setup.md`와 같은 형식). `/metrics`는 캐시만 읽으므로 수집 간격을 조회 주기보다 짧게 잡아도 AWS 비용은 늘지 않습니다.

```yaml
      - targetName: healthomics
        type: StaticEndpoints
        enabled: true
        endpoints:
          - address: "127.0.0.1:9109"
            path: "/metrics"
            scheme: "http"
            interval: "60s"
            timeout: "15s"
```

## 동작과 한계

- `/metrics` 요청은 캐시만 읽고 AWS를 호출하지 않습니다.
- `ListRuns`는 상태 필터 없이 최근 5페이지(500개)까지 보고, 진행 중 상태(PENDING, STARTING, RUNNING, STOPPING)는 상태별로 따로 조회해서 놓치지 않습니다. 끝난 run 목록은 `ListRuns`가 최근 run부터 준다는 실측(2026-11-04)에 기대고 있습니다. AWS 문서에는 정렬 순서가 없습니다.
- 끝난 run의 task 목록과 실패 사유는 한 번만 조회합니다. 워크플로 이름은 `GetWorkflow`로 받아 캐시합니다. private 워크플로는 `ListRuns`가 이름을 비워서 줍니다.
- API 조회가 실패하면 직전 결과를 `--interval`의 3배까지만 내보냅니다.
- run·task 이름에 샘플 식별자가 들어가는 환경이면 그대로 라벨이 됩니다. 수집 서버가 고객 환경 안에 있을 때만 쓰세요.
