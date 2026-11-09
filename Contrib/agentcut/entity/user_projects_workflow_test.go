package entity

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func sendProjectUpdate(env *testsuite.TestWorkflowEnvironment, delay time.Duration, name, updateID string, arg any, onDone func(any, error)) {
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(name, updateID, &testsuite.TestUpdateCallback{
			OnAccept:   func() {},
			OnReject:   func(err error) { onDone(nil, err) },
			OnComplete: func(result any, err error) { onDone(result, err) },
		}, arg)
	}, delay)
}

func TestUserProjectsWorkflow(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(UserProjectsWorkflow)

	ref := ProjectRef{ProjectID: "p1", SessionID: "sess-1", Title: "我的项目", CreatedAtUnixMs: 1755400000000}

	var addErr, dupErr, renameErr, removeErr, missErr error
	var dupRef ProjectRef

	sendProjectUpdate(env, 1*time.Millisecond, UpdateAddProject, "add-1",
		AddProjectRequest{Ref: ref}, func(res any, err error) { addErr = err })
	// 相同 projectId 再登记一次（两步创建流程重放）→ 幂等成功并返回已有记录
	sendProjectUpdate(env, 2*time.Millisecond, UpdateAddProject, "add-1-retry",
		AddProjectRequest{Ref: ProjectRef{ProjectID: "p1", Title: "重复登记"}}, func(res any, err error) {
			dupErr = err
			if err == nil {
				dupErr = decodeUpdateResult(res, &dupRef)
			}
		})
	sendProjectUpdate(env, 3*time.Millisecond, UpdateRenameProject, "rename-1",
		RenameProjectRequest{ProjectID: "p1", Title: "新标题"}, func(res any, err error) { renameErr = err })
	sendProjectUpdate(env, 4*time.Millisecond, UpdateRenameProject, "rename-miss",
		RenameProjectRequest{ProjectID: "nope", Title: "x"}, func(res any, err error) { missErr = err })
	sendProjectUpdate(env, 5*time.Millisecond, UpdateAddProject, "add-2",
		AddProjectRequest{Ref: ProjectRef{ProjectID: "p2", Title: "第二个"}}, func(res any, err error) {
			require.NoError(t, err)
		})
	sendProjectUpdate(env, 6*time.Millisecond, UpdateRemoveProject, "remove-2",
		RemoveProjectRequest{ProjectID: "p2"}, func(res any, err error) { removeErr = err })

	// 阈值 4：add-1 / rename-1 / add-2 / remove-2 四次有效变更后 ContinueAsNew
	env.ExecuteWorkflow(UserProjectsWorkflow, UserProjectsInit{UserSubject: "user-1", MaxUpdatesPerRun: 4})
	require.True(t, env.IsWorkflowCompleted())
	var canErr *workflow.ContinueAsNewError
	require.True(t, errors.As(env.GetWorkflowError(), &canErr), "expected ContinueAsNew, got %v", env.GetWorkflowError())

	require.NoError(t, addErr)
	require.NoError(t, dupErr)
	assert.Equal(t, "我的项目", dupRef.Title, "duplicate add must return the existing ref untouched")
	require.NoError(t, renameErr)
	require.Error(t, missErr)
	require.NoError(t, removeErr)

	v, err := env.QueryWorkflow(QueryListProjects)
	require.NoError(t, err)
	var projects []ProjectRef
	require.NoError(t, v.Get(&projects))
	require.Len(t, projects, 1)
	assert.Equal(t, "p1", projects[0].ProjectID)
	assert.Equal(t, "新标题", projects[0].Title)
	assert.Equal(t, "sess-1", projects[0].SessionID)
}
