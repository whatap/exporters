# Exporter CI/CD Pipeline 가이드

## 개요

Exporter 프로젝트(azure, ncloud)의 빌드와 S3 배포를 위한 Jenkins Pipeline입니다.
프로젝트가 추가되는 경우 매개변수에서 관리가 필요합니다.
빌드와 배포가 분리되어 있어 독립적으로 실행할 수 있습니다.

## 파이프라인 구성

| 파이프라인 | 파일 | 실행 노드 | 역할 |
|-----------|------|----------|------|
| Build | `Jenkinsfile.build` | k8s-agent-node-x86 (10.21.11.132) | Go 빌드, GitHub push |
| Deploy | `Jenkinsfile.deploy` | master | S3 업로드 |

---

## Build Pipeline

### 매개변수

| 이름 | 타입 | 필수 | 설명 |
|------|------|------|------|
| PROJECT | choice | O | `azure` / `ncloud` / `all` |
| VERSION | string | O | SemVer 형식 (예: `1.0.0`, `2.1.3`) |

### 실행 흐름

```
Validate Parameters → Checkout → Update build.txt → Build → Archive Artifacts → Push to GitHub
```

1. **Validate Parameters** - VERSION이 SemVer 형식인지 검증
2. **Checkout** - SCM 설정에 지정된 브랜치를 체크아웃
3. **Update build.txt** - 선택한 프로젝트의 `build.txt`에 version, release_date 기록
4. **Build** - 프로젝트별 `build.sh` 실행 (linux/amd64, linux/arm64 크로스 컴파일)
5. **Archive Artifacts** - 빌드 결과물을 Jenkins 아티팩트로 저장
6. **Push to GitHub** - 변경된 파일(build.txt, bin/) 커밋 및 버전 태그 push

### 빌드 결과물

빌드 완료 시 아래 파일들이 GitHub에 커밋됩니다:

```
{project}/
├── build.txt                        # version, release_date 업데이트
└── bin/
    ├── amd64/{exporter_name}        # linux/amd64 바이너리
    └── arm64/{exporter_name}        # linux/arm64 바이너리
```

Git 태그도 함께 생성됩니다: `v{VERSION}` (예: `v1.0.0`)

### 프로젝트별 Exporter 파일명

| PROJECT | Exporter 파일명 |
|---------|----------------|
| azure | azure_metrics_exporter |
| ncloud | ncloud_exporter |

---

## Deploy Pipeline

### 매개변수

| 이름 | 타입 | 필수 | 설명 |
|------|------|------|------|
| PROJECT | choice | O | `azure` / `ncloud` / `all` |
| VERSION | string | - | 배포할 버전. 빈 값이면 Git의 `build.txt`에서 자동으로 가져옴 |
| BUILD_JOB_NUMBER | string | - | 빌드 Job 번호. 빈 값이면 Git에서 체크아웃 |

### 실행 흐름

```
Get Artifacts → Resolve Version → Verify Artifacts → Upload to S3 → Verify S3 Upload
```

1. **Get Artifacts** - BUILD_JOB_NUMBER가 있으면 빌드잡에서 복사, 없으면 Git에서 체크아웃
2. **Resolve Version** - VERSION이 빈 값이면 `build.txt`에서 version 읽어옴
3. **Verify Artifacts** - 아키텍처별 실행파일 존재 여부 확인
4. **Upload to S3** - 버전별 경로 + latest 경로에 업로드
5. **Verify S3 Upload** - 업로드된 파일 목록 출력

### S3 업로드 경로

```
s3://repo.whatap.io/exporter/
└── {exporter_name}/
    ├── {version}/
    │   ├── amd64/{exporter_name}
    │   └── arm64/{exporter_name}
    └── latest/
        ├── amd64/{exporter_name}       ← 항상 최신 버전으로 덮어씀
        └── arm64/{exporter_name}
```

**예시** (`PROJECT=azure`, `VERSION=1.0.0`):

```
s3://repo.whatap.io/exporter/azure_metrics_exporter/1.0.0/amd64/azure_metrics_exporter
s3://repo.whatap.io/exporter/azure_metrics_exporter/1.0.0/arm64/azure_metrics_exporter
s3://repo.whatap.io/exporter/azure_metrics_exporter/latest/amd64/azure_metrics_exporter
s3://repo.whatap.io/exporter/azure_metrics_exporter/latest/arm64/azure_metrics_exporter
```

---

## 사용 시나리오

### 1. 신규 버전 빌드 + 배포

```
1) Build Pipeline 실행: PROJECT=azure, VERSION=1.0.0
2) 빌드 성공 확인
3) Deploy Pipeline 실행: PROJECT=azure, VERSION=1.0.0
   OR Deploy Pipeline 실행: PROJECT=azure, VERSION=1.0.0, BUILD_JOB_NUMBER=빌드번호
```

### 2. Git에 있는 최신 빌드를 배포 (VERSION 생략)

```
1) Deploy Pipeline 실행: PROJECT=azure, VERSION=(빈 값)
   → Git의 azure/build.txt에서 version을 자동으로 읽어와 배포
```

### 3. 전체 프로젝트 빌드

```
1) Build Pipeline 실행: PROJECT=all, VERSION=1.0.0
   → azure, ncloud 모두 빌드
```