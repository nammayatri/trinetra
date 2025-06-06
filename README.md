# Monitor: Functional Go Job Scheduler with Bash, JSON Logic, and Prometheus

This project is a functional Go application for running scheduled jobs defined in a YAML config. Each job can execute a bash script, evaluate the result with JSON logic, and publish metrics to Prometheus. It is designed for extensibility and ease of use in monitoring and automation scenarios.

## Features
- **YAML-based job configuration**
- **Frequency and strict (time-based) scheduling**
- **Runs bash scripts with full output capture**
- **Evaluates job results using JSON logic**
- **Publishes job status metrics to Prometheus**
- **Runs easily in Docker with all common Unix tools (curl, jq, sed, etc.)**

## Getting Started

### 1. Clone the repository
```sh
git clone <your-repo-url>
cd <repo-directory>
```

### 2. Configure jobs
Edit `config.yaml` to define your jobs, schedules, scripts, and decision logic. Example:

```yaml
jobs:
  - name: "Test HTTP Check"
    description: "Check if a website is accessible"
    schedule:
        type: "frequency"
        data:
            daysOfWeek: [1, 1, 1, 1, 1, 1, 1]
            intervalInSeconds: 30
    action:
        plugin: "bash_check"
        script: |
          echo "testing job"
          if curl -s -o /dev/null -w "%{http_code}" https://httpbin.org/status/200 | grep -q "200"; then
            exit 0
          else
            exit 1
          fi
    decision_logic: >
      {
          "if": [
              {  
                  "==": [{"var": "bash_check_result.exit_code"}, 0]
              },
              "GOOD",
              "BAD"
          ]
      }
```

### 3. Build and Run Locally

#### Prerequisites
- Go 1.21+
- Docker (for containerized runs)

#### Run with Go
```sh
go run main.go
```

#### Build and Run with Docker
```sh
docker build -t monitor .
docker run -p 2112:2112 monitor
```

### 4. Prometheus Metrics
- Metrics are exposed at [http://localhost:2112/metrics](http://localhost:2112/metrics)
- You can scrape this endpoint with Prometheus or view it in your browser.

### 5. Customizing Scripts
- Scripts are run in a real bash shell with all common Unix tools (curl, jq, sed, grep, etc.) available.
- Use YAML `|` for multiline scripts to preserve line breaks.

### 6. Adding Jobs
- Add more jobs to `config.yaml` with different schedules, scripts, and logic.
- Both `frequency` and `strict` (specific time) schedules are supported.

## Development
- All code is in `main.go` for simplicity.
- Uses only functional programming (no OOP/struct methods for core logic).
- Dependencies are managed with Go modules.

## License
MIT 