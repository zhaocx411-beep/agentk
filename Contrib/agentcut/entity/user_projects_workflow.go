package entity

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/workflow"
)

// UserProjectsWorkflow 的名称常量。
const (
	UserProjectsWorkflowName = "UserProjectsWorkflow"
	UpdateAddProject         = "addProject"
	UpdateRenameProject      = "renameProject"
	UpdateRemoveProject      = "removeProject"
	QueryListProjects        = "listProjects"
)

// UserProjectsWorkflowID 返回终端用户对应的确定性 Workflow ID。
func UserProjectsWorkflowID(userSubject string) string { return "user-projects:" + userSubject }

// ProjectRef 是项目归属索引里的一条记录（替代关系库里的 ac_projects 表）。
type ProjectRef struct {
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId,omitempty"` // 绑定的 agent-k Session
	ProfileID string `json:"profileId,omitempty"` // 创建 Session 时选中的 Profile
	Title     string `json:"title"`
	// CreatedAtUnixMs 由调用方（cut-apiserver）提供：Workflow 内禁止取墙钟。
	CreatedAtUnixMs int64 `json:"createdAtUnixMs"`
}

// UserProjectsInit 是启动/ContinueAsNew 入参。
type UserProjectsInit struct {
	UserSubject string       `json:"userSubject"`
	Projects    []ProjectRef `json:"projects"`
	// MaxUpdatesPerRun 覆盖 ContinueAsNew 阈值（<=0 用默认值），供测试用。
	MaxUpdatesPerRun int `json:"maxUpdatesPerRun,omitempty"`
}

// AddProjectRequest / RenameProjectRequest / RemoveProjectRequest 是 Update 入参。
type AddProjectRequest struct {
	Ref ProjectRef `json:"ref"`
}

type RenameProjectRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
}

type RemoveProjectRequest struct {
	ProjectID string `json:"projectId"`
}

// UserProjectsWorkflow 维护一个终端用户的项目列表。
// 幂等：Update 用 UpdateID 原生去重；addProject 对已存在的 projectId 直接成功
// （项目创建是"先起 TimelineEntityWorkflow 再登记"的两步流程，重放必须收敛）。
func UserProjectsWorkflow(ctx workflow.Context, init UserProjectsInit) error {
	projects := append([]ProjectRef(nil), init.Projects...)
	updatesInRun := 0
	threshold := init.MaxUpdatesPerRun
	if threshold <= 0 {
		threshold = maxUpdatesPerRun
	}

	find := func(projectID string) int {
		for i := range projects {
			if projects[i].ProjectID == projectID {
				return i
			}
		}
		return -1
	}

	if err := workflow.SetQueryHandler(ctx, QueryListProjects, func() ([]ProjectRef, error) {
		return projects, nil
	}); err != nil {
		return err
	}

	if err := workflow.SetUpdateHandler(ctx, UpdateAddProject,
		func(ctx workflow.Context, req AddProjectRequest) (ProjectRef, error) {
			if req.Ref.ProjectID == "" {
				return ProjectRef{}, errors.New("projectId is required")
			}
			if i := find(req.Ref.ProjectID); i >= 0 {
				return projects[i], nil // 幂等重放
			}
			projects = append(projects, req.Ref)
			updatesInRun++
			return req.Ref, nil
		}); err != nil {
		return err
	}

	if err := workflow.SetUpdateHandler(ctx, UpdateRenameProject,
		func(ctx workflow.Context, req RenameProjectRequest) (ProjectRef, error) {
			i := find(req.ProjectID)
			if i < 0 {
				return ProjectRef{}, fmt.Errorf("project %q not found", req.ProjectID)
			}
			projects[i].Title = req.Title
			updatesInRun++
			return projects[i], nil
		}); err != nil {
		return err
	}

	if err := workflow.SetUpdateHandler(ctx, UpdateRemoveProject,
		func(ctx workflow.Context, req RemoveProjectRequest) error {
			i := find(req.ProjectID)
			if i < 0 {
				return nil // 幂等：已删视为成功
			}
			projects = append(projects[:i], projects[i+1:]...)
			updatesInRun++
			return nil
		}); err != nil {
		return err
	}

	// 用户索引 Workflow 永不主动结束，仅按 History 体量翻新 Run。
	if err := workflow.Await(ctx, func() bool { return updatesInRun >= threshold }); err != nil {
		return err
	}
	if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
		return err
	}
	return workflow.NewContinueAsNewError(ctx, UserProjectsWorkflow, UserProjectsInit{
		UserSubject:      init.UserSubject,
		Projects:         projects,
		MaxUpdatesPerRun: init.MaxUpdatesPerRun,
	})
}
