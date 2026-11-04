# CloudWatch PromQL Exporter

CloudWatch의 PromQL(Prometheus 호환) HTTP API로 설정한 쿼리를 주기적으로 실행하고, 결과를 Prometheus 형식 `/metrics`로 제공합니다.
OpenAgent가 이 엔드포인트를 스크랩하면 수집 서버 변경 없이 OpenMetrics로 저장됩니다.

기존 CloudWatch 연동(`prometheus/cloudwatch_exporter`)은 Classic API(`GetMetricData`)만 사용합니다.
CloudWatch에 OpenTelemetry 지표로 들어오는 데이터(예: AWS HealthOmics run metrics `aws.omics.*`)는 PromQL로만 조회되므로 이 Exporter가 필요합니다.

## 실행

```sh
cd cloudwatch-promql && go build -o bin/cloudwatch_promql_exporter .
./bin/cloudwatch_promql_exporter --config config.example.yml
curl --fail http://127.0.0.1:9108/metrics
```

- 기본 listen: `127.0.0.1:9108`. 다른 호스트의 OpenAgent가 수집하면 `--web.listen-address=<주소>:9108`로 바꾸고 접근 범위를 제한하세요. TLS/HTTP 인증은 내장하지 않습니다.
- AWS 인증: AWS SDK 기본 credential chain. EC2에서는 인스턴스 프로파일의 IAM 역할을 사용합니다. 비밀키를 설정 파일에 넣지 않습니다.
- 리전: AWS SDK 환경변수/프로필, 없으면 EC2 IMDS. 필요할 때만 `--region`을 지정합니다.
- 엔드포인트: `https://monitoring.<region>.amazonaws.com/api/v1/query`, SigV4 서비스 이름 `monitoring`.

## IAM 권한

| API | 필요한 action |
| --- | --- |
| `/api/v1/query` | `cloudwatch:GetMetricData`, `cloudwatch:ListMetrics` |

## 동작

- `interval`마다 모든 쿼리를 순서대로 한 번씩 실행(instant query)하고 결과를 캐시합니다. `/metrics` 요청은 캐시만 읽으므로 스크랩 횟수가 CloudWatch 조회 비용을 늘리지 않습니다. CloudWatch PromQL은 스캔한 샘플 수로 과금됩니다.
- `labels`에 적은 CloudWatch 라벨만 지정한 이름으로 내보내고 나머지는 버립니다. 라벨을 버린 뒤 두 시리즈가 같아지면 값을 합치지 않고 그 쿼리를 실패로 처리합니다. 이때는 둘을 구분하는 라벨을 `labels`에 추가하세요.
- 히스토그램 샘플이나 instant vector가 아닌 결과는 지원하지 않습니다. 히스토그램은 `histogram_avg`, `histogram_quantile` 등으로 감싸 float로 만드세요.
- 쿼리가 실패하면 직전 성공 결과를 `max_staleness`(기본 `interval`의 3배)까지만 내보내고, 그 뒤에는 시리즈를 내립니다.

## 설정

| 키 | 기본값 | 설명 |
| --- | --- | --- |
| `interval` | `60s` | 조회 주기 |
| `timeout` | `30s` (interval의 절반 이하) | 쿼리 1건 제한 시간. `interval`보다 짧아야 합니다 |
| `max_staleness` | `interval × 3` | 실패 중 직전 결과를 유지하는 최대 시간 |
| `queries[].name` | (필수) | 내보낼 Prometheus 지표 이름 |
| `queries[].query` | (필수) | PromQL. 점이 들어간 이름은 `{"aws.omics.task.cpu.usage"}`처럼 따옴표로 감쌉니다 |
| `queries[].type` | `gauge` | `gauge`, `counter`, `untyped` |
| `queries[].help` | 쿼리 문자열 | HELP 문구(한 줄) |
| `queries[].labels` | 없음 | `CloudWatch 라벨: 내보낼 라벨` 매핑 |

`config.example.yml`은 HealthOmics run metrics 14종 프리셋입니다.

## OpenAgent 등록

OpenAgent의 `scrape_config.yaml`에서 기존 `features.openAgent.targets` 목록에 항목을 하나 추가합니다(OpenAgent `docs/ec2-instance-state-setup.md`와 같은 형식). 기존 CloudWatch Exporter(9106) 항목은 그대로 둡니다.

```yaml
      - targetName: cloudwatch-promql
        type: StaticEndpoints
        enabled: true
        endpoints:
          - address: "127.0.0.1:9108"
            path: "/metrics"
            scheme: "http"
            interval: "60s"
            timeout: "15s"
```

## 자체 지표

| 지표 | 의미 |
| --- | --- |
| `cloudwatch_promql_query_up{query}` | 마지막 조회 성공 여부(1/0) |
| `cloudwatch_promql_query_series{query}` | 현재 내보내는 시리즈 수 |
| `cloudwatch_promql_query_last_success_timestamp_seconds{query}` | 마지막 성공 시각(Unix 초, 없으면 0) |

## HealthOmics run metrics 참고

- run metrics는 private·shared 워크플로에만 나옵니다. run 역할에 `cloudwatch:PutMetricData`가 있어야 하고, run의 log level이 `OFF`가 아니어야 합니다. 30초 넘게 실행된 task만 지표가 남습니다.
- 지표 라벨은 ID뿐입니다(`run_id`, `task_id`, `workflow_id`). run·task 이름과 상태는 HealthOmics API를 조회하는 별도 Exporter가 내보내고, 화면 PromQL에서 `group_left`로 결합합니다.
- 2026-11-04 실측: `"4 GiB"`를 요청한 task의 `aws.omics.task.memory.limit`은 3,473,932,288 bytes(약 3.24 GiB)였습니다. 요청량과 한도는 다를 수 있습니다.
