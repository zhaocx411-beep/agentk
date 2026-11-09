package videogen

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// scriptedClient 按脚本返回轮询结果（超出序列重复末项），记录提交次数。
// 使用公共最小接口（Request），对应 Pipeline 直接调用 ActivityName 的路径。
type scriptedClient struct {
	submits   int
	submitErr error
	taskID    string
	seq       []tool.PollResult
	pollErr   error
	polls     int
}

func (c *scriptedClient) Submit(_ context.Context, _ Request) (string, error) {
	c.submits++
	if c.submitErr != nil {
		return "", c.submitErr
	}
	return c.taskID, nil
}

func (c *scriptedClient) Poll(context.Context, string) (tool.PollResult, error) {
	c.polls++
	if c.pollErr != nil {
		return tool.PollResult{}, c.pollErr
	}
	i := c.polls - 1
	if i >= len(c.seq) {
		i = len(c.seq) - 1
	}
	return c.seq[i], nil
}

// fakeCatalog 为各调用接口分别提供查询：公共最小接口服务 Pipeline，供应商专用
// 接口服务各自工具。类型系统会阻止在装配期把条目挂到错误工具，这里保持同样约束。
type fakeCatalog struct {
	basic    map[string]Client
	t2v      map[string]KlingT2VClient
	i2v      map[string]KlingI2VClient
	omni     map[string]KlingOmniClient
	seedance map[string]SeedanceClient
}

func (c fakeCatalog) VideoClient(name string) (Client, bool) {
	client, ok := c.basic[name]
	return client, ok
}
func (c fakeCatalog) KlingT2VClient(name string) (KlingT2VClient, bool) {
	client, ok := c.t2v[name]
	return client, ok
}
func (c fakeCatalog) KlingI2VClient(name string) (KlingI2VClient, bool) {
	client, ok := c.i2v[name]
	return client, ok
}
func (c fakeCatalog) KlingOmniClient(name string) (KlingOmniClient, bool) {
	client, ok := c.omni[name]
	return client, ok
}
func (c fakeCatalog) SeedanceClient(name string) (SeedanceClient, bool) {
	client, ok := c.seedance[name]
	return client, ok
}

func newTestVideoActivity(client Client) *Activity {
	a := NewActivity(fakeCatalog{basic: map[string]Client{"kling-3.0": client}})
	a.pollDelay = func(time.Duration) time.Duration { return 0 }
	return a
}

func validInput() ActivityInput {
	return ActivityInput{
		Model:   "kling-3.0",
		Request: Request{FirstFrame: "https://img/1.png", Prompt: "运镜"},
	}
}

func execVideoActivity(t *testing.T, a *Activity, in ActivityInput, heartbeat any) (ActivityOutput, error) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(a.Run)
	if heartbeat != nil {
		env.SetHeartbeatDetails(heartbeat)
	}
	val, err := env.ExecuteActivity(a.Run, in)
	if err != nil {
		return ActivityOutput{}, err
	}
	var out ActivityOutput
	if err := val.Get(&out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func TestVideoActivitySubmitAndPoll(t *testing.T) {
	client := &scriptedClient{taskID: "task-9", seq: []tool.PollResult{
		{StatusDetail: "processing"},
		{StatusDetail: "processing"},
		{Done: true, Result: json.RawMessage(`{"artifacts":[
			{"type":"video","uri":"https://v/9.mp4"},
			{"type":"video","uri":"https://v/10.mp4","description":"候选视频","params":{"duration":"5"}}
		]}`)},
	}}
	a := newTestVideoActivity(client)

	out, err := execVideoActivity(t, a, validInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 2 || out.Artifacts[0].URI != "https://v/9.mp4" ||
		out.Artifacts[0].Description == "" || out.Artifacts[0].Model != "kling-3.0" {
		t.Fatalf("out = %+v", out)
	}
	if out.Artifacts[1].URI != "https://v/10.mp4" ||
		out.Artifacts[1].Description != "候选视频" ||
		out.Artifacts[1].Model != "kling-3.0" ||
		out.Artifacts[1].Params["duration"] != "5" {
		t.Fatalf("second artifact = %+v", out.Artifacts[1])
	}
	if client.submits != 1 || client.polls != 3 {
		t.Fatalf("submits = %d polls = %d", client.submits, client.polls)
	}
}

func TestVideoActivityResumeFromHeartbeat(t *testing.T) {
	// 上一 attempt 已提交：本 attempt 不得再提交，直接续轮询同一任务。
	client := &scriptedClient{taskID: "task-new", seq: []tool.PollResult{
		{Done: true, Result: json.RawMessage(`{"artifacts":[{"type":"video","uri":"https://v/old.mp4"}]}`)},
	}}
	a := newTestVideoActivity(client)

	out, err := execVideoActivity(t, a, validInput(), temporalx.NewRemoteTaskProgress("task-old"))
	if err != nil {
		t.Fatal(err)
	}
	if client.submits != 0 {
		t.Fatalf("resumed attempt resubmitted: submits = %d", client.submits)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].URI != "https://v/old.mp4" {
		t.Fatalf("out = %+v", out)
	}
}

func TestVideoActivityRemoteFailureNonRetryable(t *testing.T) {
	client := &scriptedClient{taskID: "t", seq: []tool.PollResult{
		{Failed: true, FailureReason: "audit rejected"},
	}}
	a := newTestVideoActivity(client)

	_, err := execVideoActivity(t, a, validInput(), nil)
	assertVideoNonRetryable(t, err)
}

func TestVideoActivityTransientPollErrorRetryable(t *testing.T) {
	client := &scriptedClient{taskID: "t", pollErr: errors.New("flake")}
	a := newTestVideoActivity(client)

	_, err := execVideoActivity(t, a, validInput(), nil)
	if err == nil {
		t.Fatal("transient poll error swallowed")
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		t.Fatalf("transient poll error must stay retryable: %v", err)
	}
}

func TestVideoActivityInputValidation(t *testing.T) {
	a := newTestVideoActivity(&scriptedClient{taskID: "t"})

	// 缺 model
	in := validInput()
	in.Model = ""
	_, err := execVideoActivity(t, a, in, nil)
	assertVideoNonRetryable(t, err)

	// 缺业务必填参数（firstFrame）
	in = validInput()
	in.FirstFrame = ""
	_, err = execVideoActivity(t, a, in, nil)
	assertVideoNonRetryable(t, err)

	// 模型不在会话环境里
	in = validInput()
	in.Model = "nope"
	_, err = execVideoActivity(t, a, in, nil)
	assertVideoNonRetryable(t, err)
}

func TestVideoActivityDoneWithoutURL(t *testing.T) {
	client := &scriptedClient{taskID: "t", seq: []tool.PollResult{
		{Done: true, Result: json.RawMessage(`{}`)},
	}}
	a := newTestVideoActivity(client)

	_, err := execVideoActivity(t, a, validInput(), nil)
	assertVideoNonRetryable(t, err)
}

/* ---------- 各供应商接口的完整执行 Activity ---------- */

func execDialect[I any](
	t *testing.T, fn func(context.Context, I) (ActivityOutput, error), in I, heartbeat any,
) (ActivityOutput, error) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(fn)
	if heartbeat != nil {
		env.SetHeartbeatDetails(heartbeat)
	}
	val, err := env.ExecuteActivity(fn, in)
	if err != nil {
		return ActivityOutput{}, err
	}
	var out ActivityOutput
	if err := val.Get(&out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

type dialectState struct {
	submits int
	polls   int
	taskID  string
	result  *tool.PollResult
}

func (s *dialectState) submit() string {
	s.submits++
	return s.taskID
}

func (s *dialectState) poll() tool.PollResult {
	s.polls++
	if s.result != nil {
		return *s.result
	}
	return tool.PollResult{Done: true, Result: json.RawMessage(
		`{"artifacts":[{"uri":"https://video.test/result.mp4"}]}`)}
}

type t2vActivityClient struct{ *dialectState }

func (c *t2vActivityClient) Submit(context.Context, KlingT2VRequest) (string, error) {
	return c.submit(), nil
}
func (c *t2vActivityClient) Poll(context.Context, string) (tool.PollResult, error) {
	return c.poll(), nil
}

type i2vActivityClient struct{ *dialectState }

func (c *i2vActivityClient) Submit(context.Context, KlingI2VRequest) (string, error) {
	return c.submit(), nil
}
func (c *i2vActivityClient) Poll(context.Context, string) (tool.PollResult, error) {
	return c.poll(), nil
}

type omniActivityClient struct{ *dialectState }

func (c *omniActivityClient) Submit(context.Context, KlingOmniRequest) (string, error) {
	return c.submit(), nil
}
func (c *omniActivityClient) Poll(context.Context, string) (tool.PollResult, error) {
	return c.poll(), nil
}

type seedanceActivityClient struct{ *dialectState }

func (c *seedanceActivityClient) Submit(context.Context, SeedanceRequest) (string, error) {
	return c.submit(), nil
}
func (c *seedanceActivityClient) Poll(context.Context, string) (tool.PollResult, error) {
	return c.poll(), nil
}

type dialectStubs struct {
	t2v      *t2vActivityClient
	i2v      *i2vActivityClient
	omni     *omniActivityClient
	seedance *seedanceActivityClient
}

func newDialectActivity(taskIDPrefix string) (*Activity, dialectStubs) {
	stubs := dialectStubs{
		t2v:      &t2vActivityClient{&dialectState{taskID: taskIDPrefix + "-t2v"}},
		i2v:      &i2vActivityClient{&dialectState{taskID: taskIDPrefix + "-i2v"}},
		omni:     &omniActivityClient{&dialectState{taskID: taskIDPrefix + "-omni"}},
		seedance: &seedanceActivityClient{&dialectState{taskID: taskIDPrefix + "-seedance"}},
	}
	a := NewActivity(fakeCatalog{
		t2v:      map[string]KlingT2VClient{"kling-3.0-t2v": stubs.t2v},
		i2v:      map[string]KlingI2VClient{"kling-3.0": stubs.i2v},
		omni:     map[string]KlingOmniClient{"kling-3.0-omni": stubs.omni},
		seedance: map[string]SeedanceClient{"doubao-seedance-2.0": stubs.seedance},
	})
	a.pollDelay = func(time.Duration) time.Duration { return 0 }
	return a, stubs
}

func TestExecuteDialects(t *testing.T) {
	a, stubs := newDialectActivity("task")
	out, err := execDialect(t, a.ExecuteKlingT2V, KlingT2VInput{
		Model: "kling-3.0-t2v",
		KlingT2VRequest: KlingT2VRequest{
			Prompt: "少女坐在火车上望向窗外", AspectRatio: "9:16",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].Model != "kling-3.0-t2v" ||
		out.Artifacts[0].Description == "" ||
		out.Artifacts[0].Params["aspectRatio"] != "9:16" ||
		out.Artifacts[0].Params["resolution"] != "1080p" {
		t.Fatalf("t2v out = %+v", out)
	}

	out, err = execDialect(t, a.ExecuteKlingI2V, KlingI2VInput{
		Model:           "kling-3.0",
		KlingI2VRequest: KlingI2VRequest{FirstFrame: "https://img/1.png", Prompt: "运镜", Duration: "10"},
	}, nil)
	if err != nil || out.Artifacts[0].Params["duration"] != "10" {
		t.Fatalf("i2v out = %+v err = %v", out, err)
	}

	out, err = execDialect(t, a.ExecuteKlingOmni, KlingOmniInput{
		Model: "kling-3.0-omni", KlingOmniRequest: KlingOmniRequest{Prompt: "一只猫跳上桌子"},
	}, nil)
	if err != nil || out.Artifacts[0].Params["aspectRatio"] != "16:9" {
		t.Fatalf("omni out = %+v err = %v", out, err)
	}

	out, err = execDialect(t, a.ExecuteSeedance, SeedanceInput{
		Model: "doubao-seedance-2.0", SeedanceRequest: SeedanceRequest{Prompt: "海边日落", Resolution: "480p"},
	}, nil)
	if err != nil || out.Artifacts[0].Params["resolution"] != "480p" ||
		out.Artifacts[0].Params["ratio"] != "adaptive" {
		t.Fatalf("seedance out = %+v err = %v", out, err)
	}
	if stubs.t2v.submits != 1 || stubs.t2v.polls != 1 ||
		stubs.i2v.submits != 1 || stubs.omni.submits != 1 || stubs.seedance.submits != 1 {
		t.Fatalf("states = %+v", stubs)
	}
}

func TestExecuteDialectResumeFromHeartbeat(t *testing.T) {
	a, stubs := newDialectActivity("task-new")
	out, err := execDialect(t, a.ExecuteKlingI2V, KlingI2VInput{
		Model:           "kling-3.0",
		KlingI2VRequest: KlingI2VRequest{FirstFrame: "u", Prompt: "p"},
	}, temporalx.NewRemoteTaskProgress("task-old"))
	if err != nil {
		t.Fatal(err)
	}
	if stubs.i2v.submits != 0 || stubs.i2v.polls != 1 ||
		len(out.Artifacts) != 1 {
		t.Fatalf("state = %+v out = %+v", stubs.i2v, out)
	}
}

func TestExecuteDialectRemoteFailure(t *testing.T) {
	a, stubs := newDialectActivity("task")
	stubs.i2v.result = &tool.PollResult{Failed: true, FailureReason: "audit rejected"}
	_, err := execDialect(t, a.ExecuteKlingI2V, KlingI2VInput{
		Model:           "kling-3.0",
		KlingI2VRequest: KlingI2VRequest{FirstFrame: "u", Prompt: "p"},
	}, nil)
	assertVideoNonRetryable(t, err)
}

func TestExecuteDialectValidation(t *testing.T) {
	a, _ := newDialectActivity("t")
	for _, in := range []KlingI2VInput{
		{KlingI2VRequest: KlingI2VRequest{FirstFrame: "u", Prompt: "p"}},
		{Model: "nope", KlingI2VRequest: KlingI2VRequest{FirstFrame: "u", Prompt: "p"}},
		{Model: "kling-3.0", KlingI2VRequest: KlingI2VRequest{Prompt: "p"}},
	} {
		_, err := execDialect(t, a.ExecuteKlingI2V, in, nil)
		assertVideoNonRetryable(t, err)
	}

	_, err := execDialect(t, a.ExecuteKlingOmni, KlingOmniInput{
		Model: "kling-3.0-omni",
		KlingOmniRequest: KlingOmniRequest{
			Prompt: "p", BaseVideo: "https://cdn/v.mp4", FirstFrame: "https://cdn/x.png",
		},
	}, nil)
	assertVideoNonRetryable(t, err)

	_, err = execDialect(t, a.ExecuteSeedance, SeedanceInput{
		Model:           "doubao-seedance-2.0",
		SeedanceRequest: SeedanceRequest{Prompt: "p", ReferenceVideos: []string{"a", "b", "c", "d"}},
	}, nil)
	assertVideoNonRetryable(t, err)

	_, err = execDialect(t, a.ExecuteKlingT2V, KlingT2VInput{
		Model:           "kling-3.0-t2v",
		KlingT2VRequest: KlingT2VRequest{Prompt: "p", AspectRatio: "21:9"},
	}, nil)
	assertVideoNonRetryable(t, err)
}

func assertVideoNonRetryable(t *testing.T, err error) {
	t.Helper()
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want non-retryable ApplicationError", err)
	}
}
