package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
)

const maxRequestBody = 64 << 10

type Server struct {
	agentk  AgentKClient
	store   *MemoryJobStore
	profile *apiv1.Profile
	models  Models
	now     func() time.Time
	newID   func() (string, error)
}

type createJobRequest struct {
	Topic string `json:"topic"`
	Shots int    `json:"shots"`
}

type jobDTO struct {
	ID         string            `json:"id"`
	Status     JobStatus         `json:"status"`
	Topic      string            `json:"topic"`
	Shots      int               `json:"shots"`
	Output     *StoryboardOutput `json:"output,omitempty"`
	Error      string            `json:"error,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
}

func NewServer(
	agentk AgentKClient,
	store *MemoryJobStore,
	profile *apiv1.Profile,
	models Models,
) *Server {
	return &Server{
		agentk:  agentk,
		store:   store,
		profile: profile,
		models:  models,
		now:     time.Now,
		newID:   newJobID,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /v1/jobs", s.handleListJobs)
	mux.HandleFunc("GET /v1/jobs/{jobID}", s.handleGetJob)
	registerAPIDocs(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	req := createJobRequest{Shots: 1}
	if err := decodeOneJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Topic = strings.TrimSpace(req.Topic)
	if req.Topic == "" {
		writeError(w, http.StatusBadRequest, "topic is required")
		return
	}
	if req.Shots < 1 || req.Shots > 8 {
		writeError(w, http.StatusBadRequest, "shots must be an integer between 1 and 8")
		return
	}

	jobID, err := s.newID()
	if err != nil {
		log.Printf("api: generate job id: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}
	sessionID, err := s.agentk.CreateSession(
		r.Context(),
		"Storyboard Film: "+req.Topic,
		s.profile,
	)
	if err != nil {
		log.Printf("api: create agent-k session: %v", err)
		writeError(w, http.StatusBadGateway, "failed to create AgentK session")
		return
	}
	execID, err := s.agentk.StartPipeline(r.Context(), sessionID, StoryboardArgs{
		Topic:      req.Topic,
		Shots:      req.Shots,
		ChatModel:  s.models.Chat,
		ImageModel: s.models.Image,
		VideoModel: s.models.Video,
	})
	if err != nil {
		log.Printf("api: start agent-k pipeline for session %s: %v", sessionID, err)
		writeError(w, http.StatusBadGateway, "failed to start AgentK pipeline")
		return
	}

	job := Job{
		ID:        jobID,
		SessionID: sessionID,
		ExecID:    execID,
		Topic:     req.Topic,
		Shots:     req.Shots,
		Status:    JobRunning,
		CreatedAt: s.now().UTC(),
	}
	if err := s.store.Create(job); err != nil {
		log.Printf("api: store job %s: %v", job.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}
	writeJSON(w, http.StatusAccepted, toJobDTO(job))
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.store.Get(r.PathValue("jobID"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	job, err := s.refreshJob(r.Context(), job)
	if err != nil {
		log.Printf("api: refresh job %s: %v", job.ID, err)
		writeError(w, http.StatusBadGateway, "failed to query AgentK pipeline")
		return
	}
	writeJSON(w, http.StatusOK, toJobDTO(job))
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.store.List()
	response := make([]jobDTO, 0, len(jobs))
	for _, job := range jobs {
		refreshed, err := s.refreshJob(r.Context(), job)
		if err != nil {
			log.Printf("api: refresh job %s: %v", job.ID, err)
			writeError(w, http.StatusBadGateway, "failed to query AgentK pipeline")
			return
		}
		response = append(response, toJobDTO(refreshed))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": response})
}

func (s *Server) refreshJob(ctx context.Context, job Job) (Job, error) {
	if job.Status != JobRunning {
		return job, nil
	}
	snapshot, err := s.agentk.GetPipelineRun(ctx, job.SessionID, job.ExecID)
	if err != nil {
		return job, err
	}
	switch JobStatus(snapshot.Status) {
	case JobRunning:
		return job, nil
	case JobCompleted:
		if !validStoryboardOutput(snapshot.Output) {
			return job, fmt.Errorf("completed pipeline returned an invalid output")
		}
		return s.store.Finish(
			job.ID,
			JobCompleted,
			snapshot.Output,
			"",
			s.now().UTC(),
		)
	case JobFailed:
		errorMessage := strings.TrimSpace(snapshot.Error)
		if errorMessage == "" {
			errorMessage = "pipeline failed"
		}
		return s.store.Finish(
			job.ID,
			JobFailed,
			nil,
			errorMessage,
			s.now().UTC(),
		)
	default:
		return job, fmt.Errorf("unknown pipeline status %q", snapshot.Status)
	}
}

func validStoryboardOutput(output *StoryboardOutput) bool {
	if output == nil ||
		len(output.Artifacts) != 1 ||
		output.Artifacts[0].Type != "video" ||
		strings.TrimSpace(output.Artifacts[0].URI) == "" ||
		output.ClipCount < 1 {
		return false
	}
	return true
}

func decodeOneJSON(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func newJobID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "job_" + hex.EncodeToString(raw), nil
}

func toJobDTO(job Job) jobDTO {
	return jobDTO{
		ID:         job.ID,
		Status:     job.Status,
		Topic:      job.Topic,
		Shots:      job.Shots,
		Output:     cloneOutput(job.Output),
		Error:      job.Error,
		CreatedAt:  job.CreatedAt,
		FinishedAt: job.FinishedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
