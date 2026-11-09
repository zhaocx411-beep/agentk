package render

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"agentcut/timeline"
)

// agentk-sdk 的 routing 包是 internal；测试按公开 routing v1 wire contract
// mock Agent-K Core 的 Invoke Workflow。
const invokeWorkflowType = "agentk.capability.invoke.v1"

type invokeWorkflowInput struct {
	CapabilityID string          `json:"capabilityId"`
	Input        json.RawMessage `json:"input"`
}

// fakeFfmpegTool 顶替真实 FfmpegTool：返回以自身 WorkflowID 命名的 artifact，
// 便于断言引用替换的正确性。
func fakeFfmpegTool(ctx workflow.Context, input map[string]any) (ffmpegOutput, error) {
	var out ffmpegOutput
	out.Artifacts = append(out.Artifacts, struct {
		Type string `json:"type"`
		URI  string `json:"uri"`
	}{Type: "video", URI: "art://" + workflow.GetInfo(ctx).WorkflowExecution.ID})
	return out, nil
}

func registerFfmpegInvoke(
	env *testsuite.TestWorkflowEnvironment,
	handler func(workflow.Context, map[string]any) (ffmpegOutput, error),
) {
	env.RegisterWorkflowWithOptions(
		func(ctx workflow.Context, in invokeWorkflowInput) (json.RawMessage, error) {
			if in.CapabilityID != FfmpegCapabilityID {
				return nil, temporal.NewNonRetryableApplicationError(
					"unexpected capability id: "+in.CapabilityID, "NotFound", nil)
			}
			var input map[string]any
			if err := json.Unmarshal(in.Input, &input); err != nil {
				return nil, err
			}
			output, err := handler(ctx, input)
			if err != nil {
				return nil, err
			}
			return json.Marshal(output)
		},
		workflow.RegisterOptions{Name: invokeWorkflowType},
	)
}

func newRenderEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	registerFfmpegInvoke(env, fakeFfmpegTool)
	return env
}

func TestRenderWorkflowHappyPath(t *testing.T) {
	env := newRenderEnv(t)
	in := Input{ProjectID: "p1", RenderKey: "k1", Revision: 7, Doc: fixtureDoc()}
	env.ExecuteWorkflow(WorkflowName, in)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out Output
	require.NoError(t, env.GetWorkflowResult(&out))
	assert.Equal(t, 10, out.Calls)
	assert.Equal(t, "k1", out.RenderKey)
	assert.Equal(t, uint64(7), out.Revision)
	// 最终产物来自最后一个 call（captions）。capability.Invoke 不再暴露自定义
	// 子 WorkflowID（见 workflow.go 的说明），子 Workflow ID 由 Temporal
	// 自动生成，这里只断言产物确实来自 fakeFfmpegTool，不再校验具体 ID 值。
	assert.True(t, strings.HasPrefix(out.OutputURI, "art://"))
	assert.NotEmpty(t, out.Warnings)
}

func TestRenderWorkflowLocalBranchIsInputDeterministic(t *testing.T) {
	t.Setenv("AGENTCUT_RENDER_EXECUTOR", "agentk")
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	var seen []LocalActivityInput
	var cleanupInput LocalCleanupInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input LocalActivityInput) (LocalActivityOutput, error) {
			seen = append(seen, input)
			key := IntermediateKey(input.ProjectID, input.RenderKey, input.CallID, ".mp4")
			if input.Final {
				key = "agentcut/" + input.ProjectID + "/output/" + input.RenderKey + ".mp4"
			}
			return LocalActivityOutput{Key: key}, nil
		},
		activity.RegisterOptions{Name: LocalActivityName},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input LocalCleanupInput) error {
			cleanupInput = input
			return nil
		},
		activity.RegisterOptions{Name: LocalCleanupActivityName},
	)
	in := Input{
		ProjectID: "p1", RenderKey: "1234567890abcdef", Revision: 7,
		Doc: fixtureDoc(), Executor: ExecutorLocal, Profile: DefaultLocalProfile,
	}
	env.ExecuteWorkflow(WorkflowName, in)
	require.NoError(t, env.GetWorkflowError())
	var out Output
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Len(t, seen, 10)
	assert.Equal(t, ExecutorLocal, out.Executor)
	assert.Equal(t, ContentURI("p1", in.RenderKey), out.OutputURI)
	assert.Equal(t, "$media:mv", seen[0].Input["video"])
	assert.True(t, seen[len(seen)-1].Final)
	assert.Equal(t, in.ProjectID, cleanupInput.ProjectID)
	assert.Equal(t, in.RenderKey, cleanupInput.RenderKey)
	expectedCallIDs := make([]string, len(seen))
	for index, input := range seen {
		expectedCallIDs[index] = input.CallID
	}
	assert.Equal(t, expectedCallIDs, cleanupInput.CallIDs)
}

func TestRenderWorkflowLocalFailureStillCleansIntermediates(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	env.RegisterActivityWithOptions(
		func(context.Context, LocalActivityInput) (LocalActivityOutput, error) {
			return LocalActivityOutput{}, temporal.NewNonRetryableApplicationError(
				"ffmpeg failed", "LOCAL_RENDER_INVALID", nil)
		},
		activity.RegisterOptions{Name: LocalActivityName},
	)
	var cleanupInput LocalCleanupInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input LocalCleanupInput) error {
			cleanupInput = input
			return nil
		},
		activity.RegisterOptions{Name: LocalCleanupActivityName},
	)

	in := Input{
		ProjectID: "p1", RenderKey: "1234567890abcdef",
		Doc: fixtureDoc(), Executor: ExecutorLocal, Profile: DefaultLocalProfile,
	}
	env.ExecuteWorkflow(WorkflowName, in)

	require.Error(t, env.GetWorkflowError())
	assert.Equal(t, in.ProjectID, cleanupInput.ProjectID)
	assert.Equal(t, in.RenderKey, cleanupInput.RenderKey)
	assert.NotEmpty(t, cleanupInput.CallIDs)
}

func TestRenderWorkflowLocalRetriesTransientActivityFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	attempts := make(map[string]int)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input LocalActivityInput) (LocalActivityOutput, error) {
			attempts[input.CallID]++
			if input.CallID == "item:i1" && attempts[input.CallID] < 3 {
				return LocalActivityOutput{}, errors.New("transient ffmpeg failure")
			}
			key := IntermediateKey(input.ProjectID, input.RenderKey, input.CallID, ".mp4")
			if input.Final {
				key = "agentcut/" + input.ProjectID + "/output/" + input.RenderKey + ".mp4"
			}
			return LocalActivityOutput{Key: key}, nil
		},
		activity.RegisterOptions{Name: LocalActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, LocalCleanupInput) error { return nil },
		activity.RegisterOptions{Name: LocalCleanupActivityName},
	)

	in := Input{
		ProjectID: "p1", RenderKey: "1234567890abcdef",
		Doc: fixtureDoc(), Executor: ExecutorLocal, Profile: DefaultLocalProfile,
	}
	env.ExecuteWorkflow(WorkflowName, in)

	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, 3, attempts["item:i1"])
}

func TestRenderWorkflowEmptyDocFailsFast(t *testing.T) {
	env := newRenderEnv(t)
	env.ExecuteWorkflow(WorkflowName, Input{ProjectID: "p1", RenderKey: "k1"})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, "RENDER_COMPILE_FAILED", appErr.Type())
}

func TestRenderWorkflowAgentKRejectsUnsupportedFeature(t *testing.T) {
	env := newRenderEnv(t)
	doc := fixtureDoc()
	doc.Transitions = []timeline.Transition{{
		TransitionID: "m3b", FromItemID: doc.Items[0].ItemID,
		ToItemID: doc.Items[0].ItemID, Kind: timeline.TransitionCrossfade, Frames: 1,
	}}
	env.ExecuteWorkflow(WorkflowName, Input{
		ProjectID: "p1", RenderKey: "k1", Doc: doc,
		Executor: ExecutorAgentK, Profile: DefaultAgentKProfile,
	})

	err := env.GetWorkflowError()
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, "UNSUPPORTED_RENDER_FEATURE", appErr.Type())
}

func TestRenderWorkflowChildFailurePropagates(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	registerFfmpegInvoke(
		env,
		func(workflow.Context, map[string]any) (ffmpegOutput, error) {
			return ffmpegOutput{}, temporal.NewNonRetryableApplicationError("ffmpeg exploded", "FFMPEG_FAILED", nil)
		},
	)

	env.ExecuteWorkflow(WorkflowName, Input{ProjectID: "p1", RenderKey: "k1", Doc: fixtureDoc()})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	assert.Contains(t, env.GetWorkflowError().Error(), "render call item:i1")
}
