package main

import (
	"fmt"
	"maps"
	"sync"
	"time"
)

type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

type Job struct {
	ID         string
	SessionID  string
	ExecID     string
	Topic      string
	Shots      int
	Status     JobStatus
	Output     *StoryboardOutput
	Error      string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

type MemoryJobStore struct {
	mu    sync.RWMutex
	jobs  map[string]Job
	order []string
}

func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{jobs: make(map[string]Job)}
}

func (s *MemoryJobStore) Create(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("job %q already exists", job.ID)
	}
	s.jobs[job.ID] = cloneJob(job)
	s.order = append(s.order, job.ID)
	return nil
}

func (s *MemoryJobStore) Get(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return cloneJob(job), ok
}

func (s *MemoryJobStore) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, 0, len(s.order))
	for index := len(s.order) - 1; index >= 0; index-- {
		jobs = append(jobs, cloneJob(s.jobs[s.order[index]]))
	}
	return jobs
}

func (s *MemoryJobStore) Finish(
	id string,
	status JobStatus,
	output *StoryboardOutput,
	errorMessage string,
	finishedAt time.Time,
) (Job, error) {
	if status != JobCompleted && status != JobFailed {
		return Job{}, fmt.Errorf("cannot finish job with status %q", status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}
	if job.Status != JobRunning {
		return cloneJob(job), nil
	}
	job.Status = status
	job.Output = cloneOutput(output)
	job.Error = errorMessage
	job.FinishedAt = &finishedAt
	s.jobs[id] = job
	return cloneJob(job), nil
}

func cloneJob(job Job) Job {
	job.Output = cloneOutput(job.Output)
	if job.FinishedAt != nil {
		finishedAt := *job.FinishedAt
		job.FinishedAt = &finishedAt
	}
	return job
}

func cloneOutput(output *StoryboardOutput) *StoryboardOutput {
	if output == nil {
		return nil
	}
	copy := *output
	copy.Artifacts = append([]Artifact(nil), output.Artifacts...)
	for i := range copy.Artifacts {
		copy.Artifacts[i].Params = maps.Clone(output.Artifacts[i].Params)
	}
	return &copy
}
