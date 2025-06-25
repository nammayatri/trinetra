package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/diegoholiveira/jsonlogic/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// Config represents the YAML configuration structure
type Config struct {
	Jobs []Job `yaml:"jobs"`
}

type Job struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Schedule      ScheduleConfig `yaml:"schedule"`
	Action        ActionConfig   `yaml:"action"`
	DecisionLogic interface{}    `yaml:"decision_logic"` // Fixed typo: was "decission_logic"
}

type ScheduleConfig struct {
	Type string       `yaml:"type"`
	Data ScheduleData `yaml:"data"`
}

type ScheduleData struct {
	DaysOfWeek        []int    `yaml:"daysOfWeek"`
	IntervalInSeconds int      `yaml:"intervalInSeconds,omitempty"`
	AtUTC             []string `yaml:"atUTC,omitempty"`
}

type ActionConfig struct {
	Plugin string `yaml:"plugin"`
	Script string `yaml:"script"`
}

// Global metrics
var (
	jobStatusMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "job_status_total",
			Help: "Total number of job status changes",
		},
		[]string{"job_name", "status", "exit_code"},
	)
)

func init() {
	prometheus.MustRegister(jobStatusMetric)
}

// loadConfig loads the YAML configuration file
func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}

	return &config, nil
}

// executeBashScript runs the bash script and returns the result
func executeBashScript(script string) (int, error) {
	cmd := exec.Command("bash", "-c", script)
	
	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Log the output even if there's an error
		if stdout.Len() > 0 {
			log.Printf("Script stdout: %s", stdout.String())
		}
		if stderr.Len() > 0 {
			log.Printf("Script stderr: %s", stderr.String())
		}

		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}

	// Log successful output
	if stdout.Len() > 0 {
		log.Printf("Script stdout: %s", stdout.String())
	}
	if stderr.Len() > 0 {
		log.Printf("Script stderr: %s", stderr.String())
	}

	return 0, nil
}

// evaluateDecisionLogic evaluates the JSON logic against the provided data
func evaluateDecisionLogic(logic interface{}, data map[string]interface{}) (string, error) {
	if logic == nil {
		return "OK", nil // Default decision if no logic provided
	}
	
	// Convert logic to JSON bytes
	logicBytes, err := json.Marshal(logic)
	if err != nil {
		return "", fmt.Errorf("failed to marshal logic: %w", err)
	}
	
	// Convert data to JSON bytes
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}
	
	// Create readers
	logicReader := bytes.NewReader(logicBytes)
	dataReader := bytes.NewReader(dataBytes)
	
	// Create output buffer
	var output bytes.Buffer
	
	// Apply the JSON logic
	err = jsonlogic.Apply(logicReader, dataReader, &output)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate decision logic: %w", err)
	}
	
	// Parse the result
	var result interface{}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal result: %w", err)
	}

	decision, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("decision result must be a string, got %T", result)
	}

	return decision, nil
}

// updateMetrics updates Prometheus metrics based on the decision
func updateMetrics(jobName string, decision string, exitCode int) {
	switch {
	case decision == "BAD":
		jobStatusMetric.WithLabelValues(jobName, "BAD", fmt.Sprintf("%d", exitCode)).Inc()
	case exitCode != 0:
		jobStatusMetric.WithLabelValues(jobName, "ERROR", fmt.Sprintf("%d", exitCode)).Inc()
	default:
		jobStatusMetric.WithLabelValues(jobName, "OK", fmt.Sprintf("%d", exitCode)).Inc()
	}
}

// runJob executes a single job
func runJob(job Job) {
	log.Printf("Running job: %s", job.Name)
	exitCode, err := executeBashScript(job.Action.Script)
	if err != nil {
		log.Printf("Error executing script for job %s: %v", job.Name, err)
		jobStatusMetric.WithLabelValues(job.Name, "ERROR", fmt.Sprintf("%d", exitCode)).Inc()
		return
	}
	log.Printf("Script for job %s finished with exit code: %d", job.Name, exitCode)

	data := map[string]interface{}{
		"bash_check_result": map[string]interface{}{
			"exit_code": exitCode,
		},
	}
	log.Printf("Evaluating decision logic for job %s with data: %+v", job.Name, data)
	decision, err := evaluateDecisionLogic(job.DecisionLogic, data)
	if err != nil {
		log.Printf("Error evaluating decision logic for job %s: %v", job.Name, err)
		jobStatusMetric.WithLabelValues(job.Name, "ERROR", fmt.Sprintf("%d", exitCode)).Inc()
		return
	}
	log.Printf("Decision logic for job %s returned: %s", job.Name, decision)

	updateMetrics(job.Name, decision, exitCode)
	log.Printf("Metrics updated for job %s: decision=%s, exit_code=%d", job.Name, decision, exitCode)
}

// scheduleJob creates cron schedules for a job
func scheduleJob(job Job) ([]string, error) {
	switch job.Schedule.Type {
	case "frequency":
		interval := job.Schedule.Data.IntervalInSeconds
		if interval <= 0 {
			return nil, fmt.Errorf("invalid interval: %d", interval)
		}
		return []string{fmt.Sprintf("@every %ds", interval)}, nil

	case "strict":
		if len(job.Schedule.Data.AtUTC) == 0 {
			return nil, fmt.Errorf("atUTC must not be empty for strict schedule")
		}
		var schedules []string
		for _, timeStr := range job.Schedule.Data.AtUTC {
			// Parse the time string to get hours and minutes
			var hour, min int
			_, err := fmt.Sscanf(timeStr, "%d:%d", &hour, &min)
			if err != nil {
				return nil, fmt.Errorf("invalid time format %s: %w", timeStr, err)
			}
			schedules = append(schedules, fmt.Sprintf("%d %d * * *", min, hour))
		}
		return schedules, nil

	default:
		return nil, fmt.Errorf("unsupported schedule type: %s", job.Schedule.Type)
	}
}

func main() {
	log.Println("Loading configuration...")
	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Println("Configuration loaded successfully.")

	c := cron.New(cron.WithSeconds())

	for _, job := range config.Jobs {
		log.Printf("Scheduling job: %s", job.Name)
		schedules, err := scheduleJob(job)
		if err != nil {
			log.Printf("Failed to schedule job %s: %v", job.Name, err)
			continue
		}
		for _, schedule := range schedules {
			jobCopy := job // closure safety
			schedCopy := schedule
			_, err = c.AddFunc(schedCopy, func() {
				log.Printf("Job %s triggered (schedule: %s)", jobCopy.Name, schedCopy)
				runJob(jobCopy)
			})
			if err != nil {
				log.Printf("Failed to add job %s to scheduler (schedule: %s): %v", job.Name, schedCopy, err)
			} else {
				log.Printf("Job %s scheduled for cron: %s", job.Name, schedCopy)
			}
		}
	}

	c.Start()
	log.Println("Scheduler started.")

	// Start Prometheus metrics server
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		log.Println("Starting Prometheus metrics server on :2112...")
		if err := http.ListenAndServe(":2112", nil); err != nil {
			log.Fatalf("Failed to start metrics server: %v", err)
		}
	}()

	// Keep the main goroutine alive
	select {}
}