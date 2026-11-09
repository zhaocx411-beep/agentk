package brandad

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"

	"agentk/internal/capability/httpcall"
	"agentk/internal/tool"
)

func validArgs() Args {
	return Args{
		Title:       "电商",
		Images:      []string{"https://img/a.png"},
		VideoPrompt: "突出商品卖点",
	}
}

// newEnv 注册 workflow 与 httpcall 两个 activity 的桩：submit 固定返回任务身份，
// poll 按顺序返回 statuses 中的响应。
func newEnv(t *testing.T, pollResponses []string) (*testsuite.TestWorkflowEnvironment, *SubmitCapture) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowType})

	capture := &SubmitCapture{}
	env.RegisterActivityWithOptions(
		func(_ context.Context, in httpcall.SubmitInput) (httpcall.SubmitOutput, error) {
			capture.Input = in
			return httpcall.SubmitOutput{RemoteTaskID: "task-1"}, nil
		},
		activity.RegisterOptions{Name: httpcall.SubmitActivityName})
	i := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, in httpcall.CallInput) (json.RawMessage, error) {
			capture.PollURLs = append(capture.PollURLs, in.Config.URL)
			resp := pollResponses[min(i, len(pollResponses)-1)]
			i++
			return json.RawMessage(resp), nil
		},
		activity.RegisterOptions{Name: httpcall.CallActivityName})
	return env, capture
}

type SubmitCapture struct {
	Input    httpcall.SubmitInput
	PollURLs []string
}

func TestWorkflowSubmitsAndPollsToSuccess(t *testing.T) {
	env, capture := newEnv(t, []string{
		`{"status":"processing"}`,
		`{"status":"succeeded","video_url":"https://v/ad.mp4"}`,
	})
	env.ExecuteWorkflow(WorkflowType, validArgs())
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out json.RawMessage
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	artifacts := tool.ExtractResultArtifacts(out)
	if len(artifacts) != 1 || artifacts[0].URI != "https://v/ad.mp4" {
		t.Fatalf("output = %s", out)
	}
	// 提交入参：业务参数原样作 request body，任务 id 字段按远端契约声明
	if capture.Input.Config.URL != submitURL || capture.Input.Config.TaskIDField != taskIDField {
		t.Fatalf("submit config = %+v", capture.Input.Config)
	}
	var body Args
	if err := json.Unmarshal(capture.Input.Input, &body); err != nil {
		t.Fatal(err)
	}
	if body.Title != "电商" || len(body.Images) != 1 || body.VideoPrompt == "" {
		t.Fatalf("submit body = %+v", body)
	}
	// 轮询地址由模板代入远端任务 id
	if len(capture.PollURLs) != 2 || !strings.HasSuffix(capture.PollURLs[0], "/v1/tasks/task-1") {
		t.Fatalf("poll urls = %v", capture.PollURLs)
	}
}

func TestWorkflowRemoteFailure(t *testing.T) {
	env, _ := newEnv(t, []string{`{"status":"failed","error":"nsfw rejected"}`})
	env.ExecuteWorkflow(WorkflowType, validArgs())
	err := env.GetWorkflowError()
	if err == nil || !strings.Contains(err.Error(), "nsfw rejected") {
		t.Fatalf("err = %v, want remote failure reason surfaced", err)
	}
}

func TestWorkflowPollTimeout(t *testing.T) {
	env, capture := newEnv(t, []string{`{"status":"processing"}`})
	env.ExecuteWorkflow(WorkflowType, validArgs())
	err := env.GetWorkflowError()
	if err == nil || !strings.Contains(err.Error(), "not finished within") {
		t.Fatalf("err = %v, want poll timeout", err)
	}
	if len(capture.PollURLs) != maxPolls {
		t.Fatalf("polled %d times, want %d", len(capture.PollURLs), maxPolls)
	}
}

// TestWorkflowToleratesTransientPollFailure 验证单轮轮询耗尽瞬态重试时不会终止任务：
// 跳过本轮并在下个轮询周期继续；永久性错误则立即失败。
func TestWorkflowToleratesTransientPollFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowType})
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ httpcall.SubmitInput) (httpcall.SubmitOutput, error) {
			return httpcall.SubmitOutput{RemoteTaskID: "task-1"}, nil
		},
		activity.RegisterOptions{Name: httpcall.SubmitActivityName})
	calls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ httpcall.CallInput) (json.RawMessage, error) {
			calls++
			if calls <= 3 {
				// 瞬态错误耗尽 3 次 attempt 后返回 workflow，但不应终止任务；
				// workflow 跳过本轮，在下个轮询周期继续
				return nil, temporal.NewApplicationErrorWithCause(
					"upstream 503", "Transient", errors.New("boom"))
			}
			return json.RawMessage(`{"status":"succeeded","video_url":"https://v/ad.mp4"}`), nil
		},
		activity.RegisterOptions{Name: httpcall.CallActivityName})
	env.ExecuteWorkflow(WorkflowType, validArgs())
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow must survive one transient poll failure, err = %v", err)
	}
	if calls != 4 {
		t.Fatalf("poll calls = %d, want 3 failed attempts then 1 success", calls)
	}
}

func TestWorkflowValidatesArgs(t *testing.T) {
	env, _ := newEnv(t, []string{`{"status":"succeeded"}`})
	env.ExecuteWorkflow(WorkflowType, Args{Title: "只有标题"})
	err := env.GetWorkflowError()
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v, want validation failure", err)
	}
}

func TestManifestDeclaration(t *testing.T) {
	def := ManifestCapability()
	if def.ID != CapabilityID || def.TemporalType != WorkflowType ||
		def.TimeoutSeconds != int64(workflowTimeout.Seconds()) {
		t.Fatalf("capability = %+v", def)
	}
	if def.Tool == nil || def.Tool.Mode != sdkcapability.ToolModeBlocking ||
		def.Tool.Description != pipelineDescription {
		t.Fatalf("tool = %+v", def.Tool)
	}
	for name, raw := range map[string]json.RawMessage{
		"input": def.InputSchema, "output": def.OutputSchema,
	} {
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s schema: %v", name, err)
		}
	}
}
