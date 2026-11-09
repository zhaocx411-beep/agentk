package httpcall

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"agentk/internal/temporalx"
)

func execCall(t *testing.T, a *Activity, in CallInput) (json.RawMessage, error) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(a.Call)
	val, err := env.ExecuteActivity(a.Call, in)
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	if err := val.Get(&out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func execSubmit(t *testing.T, a *Activity, in SubmitInput, heartbeat any) (SubmitOutput, error) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(a.Submit)
	if heartbeat != nil {
		env.SetHeartbeatDetails(heartbeat)
	}
	val, err := env.ExecuteActivity(a.Submit, in)
	if err != nil {
		return SubmitOutput{}, err
	}
	var out SubmitOutput
	if err := val.Get(&out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func TestCallPostJSON(t *testing.T) {
	var gotBody, gotContentType, gotIdempotencyKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody, gotContentType = string(raw), r.Header.Get("Content-Type")
		gotIdempotencyKey = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-1"}`))
	}))
	defer srv.Close()

	out, err := execCall(t, NewActivity(nil), CallInput{
		Config: CallConfig{URL: srv.URL},
		Input:  json.RawMessage(`{"source":"a.mp4"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"source":"a.mp4"}` || gotContentType != "application/json" {
		t.Fatalf("request: body=%q ct=%q", gotBody, gotContentType)
	}
	// Activity context 中应发送由调度身份派生的 Idempotency-Key。
	if gotIdempotencyKey == "" {
		t.Fatal("Idempotency-Key not sent")
	}
	if string(out) != `{"taskId":"t-1"}` {
		t.Fatalf("out = %s", out)
	}
}

func TestCallGetIgnoresBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) != 0 || r.Method != http.MethodGet {
			t.Errorf("method=%s body=%q", r.Method, raw)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	if _, err := execCall(t, NewActivity(nil), CallInput{
		Config: CallConfig{URL: srv.URL, Method: http.MethodGet},
		Input:  json.RawMessage(`{"x":1}`),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCallNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream broken"))
	}))
	defer srv.Close()

	_, err := execCall(t, NewActivity(nil), CallInput{
		Config: CallConfig{URL: srv.URL},
		Input:  json.RawMessage(`{}`),
	})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream broken") {
		t.Fatalf("err = %v", err)
	}
}

func TestCallSemantic4xxNonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("bad prompt"))
	}))
	defer srv.Close()

	_, err := execCall(t, NewActivity(nil), CallInput{
		Config: CallConfig{URL: srv.URL},
		Input:  json.RawMessage(`{}`),
	})
	if err == nil || !temporalx.IsPermanentActivityError(err) {
		t.Fatalf("expected non-retryable error for 422, got %v", err)
	}
}

func TestCallNonJSONWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain text result"))
	}))
	defer srv.Close()

	out, err := execCall(t, NewActivity(nil), CallInput{
		Config: CallConfig{URL: srv.URL},
		Input:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil || m["raw"] != "plain text result" {
		t.Fatalf("out = %s", out)
	}
}

func TestCallInputValidation(t *testing.T) {
	if _, err := execCall(t, NewActivity(nil), CallInput{}); err == nil {
		t.Fatal("missing url should fail")
	}
	if _, err := execCall(t, NewActivity(nil), CallInput{
		Config: CallConfig{URL: "http://x", Method: "PATCH"},
	}); err == nil {
		t.Fatal("unsupported method should fail")
	}
}

func TestSubmitExtractsTaskID(t *testing.T) {
	var submits int
	var gotIdempotencyKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submits++
		gotIdempotencyKey = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"task_id":"task-99"}}`)
	}))
	defer srv.Close()

	out, err := execSubmit(t, NewActivity(nil), SubmitInput{
		Config: SubmitConfig{
			CallConfig:  CallConfig{URL: srv.URL},
			TaskIDField: "data.task_id",
		},
		Input: json.RawMessage(`{"prompt":"猫"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.RemoteTaskID != "task-99" || submits != 1 {
		t.Fatalf("out = %+v, submits = %d", out, submits)
	}
	if gotIdempotencyKey == "" {
		t.Fatal("Idempotency-Key not sent on submit")
	}
}

func TestSubmitResumeSkipsResubmission(t *testing.T) {
	var submits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		submits++
		_, _ = fmt.Fprint(w, `{"data":{"task_id":"task-new"}}`)
	}))
	defer srv.Close()

	// 上一 attempt 已将任务 ID 写入 heartbeat，重试不应再次提交。
	out, err := execSubmit(t, NewActivity(nil), SubmitInput{
		Config: SubmitConfig{
			CallConfig:  CallConfig{URL: srv.URL},
			TaskIDField: "data.task_id",
		},
		Input: json.RawMessage(`{}`),
	}, temporalx.NewRemoteTaskProgress("task-old"))
	if err != nil {
		t.Fatal(err)
	}
	if out.RemoteTaskID != "task-old" || submits != 0 {
		t.Fatalf("out = %+v, submits = %d, want resume without resubmission", out, submits)
	}
}

func TestSubmitMissingTaskIDNonRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":0}`)
	}))
	defer srv.Close()

	_, err := execSubmit(t, NewActivity(nil), SubmitInput{
		Config: SubmitConfig{
			CallConfig:  CallConfig{URL: srv.URL},
			TaskIDField: "data.task_id",
		},
		Input: json.RawMessage(`{}`),
	}, nil)
	if err == nil || !temporalx.IsPermanentActivityError(err) {
		t.Fatalf("expected non-retryable error for missing task id, got %v", err)
	}
}

func TestSubmitInputValidation(t *testing.T) {
	if _, err := execSubmit(t, NewActivity(nil), SubmitInput{
		Config: SubmitConfig{CallConfig: CallConfig{URL: "http://x"}},
	}, nil); err == nil {
		t.Fatal("missing taskIdField should fail")
	}
}
