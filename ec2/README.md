# EC2 State Exporter

AWS EC2 `DescribeInstances`의 실제 lifecycle 상태를 Prometheus 형식으로 제공합니다.
기존 Prometheus CloudWatch Exporter(CPU·네트워크·상태 검사 지표)와 **별도 실행하는 Exporter**이며, CloudWatch의 `StatusCheckFailed`를 종료 상태로 해석하지 않습니다.

## 실행

공식 바이너리(linux amd64/arm64, 정적 링크)를 내려받아 실행합니다. arm64 호스트는 경로의 `amd64`를 `arm64`로 바꿉니다. `latest/` 경로도 있지만 운영에는 버전 경로를 권장합니다.

```sh
curl -fLo ec2_state_exporter https://repo.whatap.io/exporter/ec2_state_exporter/0.1.0/amd64/ec2_state_exporter
chmod +x ec2_state_exporter
./ec2_state_exporter --version
./ec2_state_exporter
curl --fail http://127.0.0.1:9107/metrics
```

소스에서 빌드할 때는 Go 1.24.3으로 검증했습니다(`cd ec2 && go build -o bin/ec2_state_exporter .`). 배포용 빌드는 Jenkins `PROJECT=ec2`가 `build.sh`로 생성합니다.

- 기본 listen: `127.0.0.1:9107`. 기존 CloudWatch Exporter의 `9106` 포트와 별개입니다.
- 기본 AWS 인증: AWS SDK 기본 credential chain. EC2 instance role을 사용할 수 있으며, 비밀키를 Exporter 전용 설정에 중복 입력하지 않습니다.
- 기본 리전: AWS SDK 환경변수/프로필 설정, 없으면 EC2 IMDS에서 검색합니다. 필요할 때만 `--region=ap-northeast-2`를 지정합니다.
- EC2 API 수집 권한: `ec2:DescribeInstances` (`Resource: "*"`). 별도의 role assume 등을 사용하는 인증 설정에는 해당 인증 경로의 권한도 필요합니다.
- 한 실행은 현재 credential의 계정과 선택된 **한 리전**을 조회합니다. 다중 리전/계정 자동 탐색은 하지 않습니다.
- 다른 호스트/컨테이너의 OpenAgent가 수집해야 한다면 `--web.listen-address=<접근 가능한 주소>:9107`로 바인딩하고 네트워크 접근 범위를 제한하세요. TLS/HTTP 인증을 내장하지 않습니다. `127.0.0.1`은 각 프로세스의 네트워크 네임스페이스 기준입니다.
- `GET /metrics`만 AWS 읽기를 실행합니다. 동시에 들어온 추가 scrape는 HTTP 503을 반환합니다.

## 실제 출력 계약

메트릭명: **`aws_ec2_instance_state`**, 타입: **gauge**.

| 항목 | 의미 |
| --- | --- |
| `instance_id` 레이블 | AWS `InstanceId` |
| `state_name` 레이블 | AWS `State.Name` |
| `region` 레이블 | 조회 리전 |
| 메트릭 숫자 값 | AWS `State.Code`의 public low byte |

`State.Code`의 high byte는 AWS 내부용이므로 제외합니다. 아래는 출력 **형식 예시**이며 ID는 설명용입니다.

```text
# HELP aws_ec2_instance_state EC2 lifecycle state code reported by DescribeInstances.
# TYPE aws_ec2_instance_state gauge
aws_ec2_instance_state{instance_id="i-0123456789abcdef0",region="ap-northeast-2",state_name="running"} 16
```

| 값 | 상태 |
| --- | --- |
| 0 | pending |
| 16 | running |
| 32 | shutting-down |
| 48 | terminated |
| 64 | stopping |
| 80 | stopped |

- scrape마다 새로 조회하며 상태 코드/이름의 일치를 검증합니다. 명시적 Prometheus sample timestamp나 이전 snapshot의 재사용은 없습니다.
- 모든 페이지 조회가 성공한 뒤에만 출력합니다. 최대 1,000개/page, 64 pages, 전체 raw instance records 4,096개, 전체 조회 제한 10초입니다. 한도를 넘거나 중간 페이지가 실패하면 **전체 HTTP 503**, 부분 상태 출력 없음입니다.
- 같은 ID의 동일 상태는 중복 제거하고, 서로 다른 상태가 중복되면 전체 scrape를 거절합니다.
- AWS에서 사라진 instance, 빈 조회, 조회 실패를 `terminated`로 합성하지 않습니다. AWS가 종료 인스턴스를 조회 결과에서 제거한 뒤에는 이 Exporter도 그 상태를 재생할 수 없습니다. 반드시 종료 상태가 조회되는 동안 수집되어야 합니다.
- 권장 scrape 간격은 60초, HTTP timeout은 10초보다 길게 설정합니다. AWS API의 페이지 간 일관성이나 종료 이벤트의 영구 보존을 보장하지 않습니다.

## OpenAgent 연결

**OpenAgent 1.8.11 이상이 필요합니다.** 1.8.10 이하에는 아래 자동 기본값과 ParamPack ID 1002가 없습니다.

1. OpenAgent의 기존 `scrape_config.yaml`에 이 Exporter의 `/metrics` 주소를 **한 번만** 등록합니다. 이미 등록했다면 추가 등록하지 않습니다.
2. `whatap.conf`(Kubernetes Operator 환경은 CR `envs`)에는 관리할 Server 프로젝트들의 `ec2_state_licenses`와 `ec2_state_forwarding_enabled`만 설정합니다.
3. 메트릭명, 레이블명, 숫자 값 해석, 중복 URL 설정은 필요하지 않습니다. OpenAgent가 위 고정 계약을 기본 처리합니다. 기존 CloudWatch Exporter 주소만 등록한 상태라면 이 상태 Exporter 주소는 최초 1회 추가해야 합니다.

라이선스 해석·프로젝트 라우팅·명령 1002 ParamPack oneway·변경 시 전송과 기본 5분 재전송은 OpenAgent가 담당합니다. 명령 ID는 Open Agent 할당 범위 `1000~1099`에 맞춰 기존 56에서 1002로 변경했습니다. 기존 `ec2_state_param_id=56` 설정이 있다면 제거하거나 `1002`로 변경해야 합니다. Yard가 프로젝트 내 서버 identity 확인 및 최종 등록 해지를 담당합니다. 이 Exporter는 EC2 상태를 읽기만 하며 EC2 또는 WhaTap의 서버를 종료/삭제하지 않습니다.

[최신 Yard 계약](https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85949)은 종료 관측의 pcode에서 `meta:instance-id`가 일치하는 Server 등록을 모두 해지합니다. 재설치 등으로 oid가 여러 개여도 Exporter에서 Server 등록 수를 검사하거나 상태를 제외하지 않습니다. `32/shutting-down` 뒤에도 AWS가 반환하는 `48/terminated`를 계속 노출하며, AWS 목록에서 사라졌다면 종료 상태를 합성하지 않습니다.

지원 수집서버와 연동을 확인하기 전에는 `ec2_state_forwarding_enabled=false`를 유지합니다. 자세한 설정은 [OpenAgent EC2 상태 전달 문서](https://github.com/whatap/openagent/blob/master/docs/ec2-instance-state.md)를 참고하세요.

## 검증

```sh
go test -race ./...
go vet ./...
```

단위 테스트는 실제 EC2를 생성하거나 종료하지 않습니다. 실환경 확인은 Exporter의 HTTP 출력과 동일 credential/region의 AWS `describe-instances` 결과를 대조합니다. HTTP 출력 검증, OpenAgent ParamPack 변환, collector 수신, Yard 등록 해지는 서로 다른 검증 단계입니다.
