package api

import (
	"errors"
	"net/http"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"agentcut/platform/blob"
	"agentcut/render"
)

// handleStartRender 触发导出。渲染 Workflow 由 cut-apiserver 直接启动、
// 跑在 AgentCut 自己的 Task Queue 上，不经过 agent-k——不占用 Session 的
// 活跃 Execution 名额（架构不变量：用户可以边导出边和 Agent 对话）。
//
// 幂等：renderKey 由文档内容哈希派生，Workflow ID 含 renderKey 且用
// RejectDuplicate 策略——同一份文档重复点导出不会重复渲染大文件：
// 进行中返回同一次执行，已完成直接复用结果。
func (s *Server) handleStartRender(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	st, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	selection := s.RenderSelection
	if selection.Executor == "" {
		selection, err = render.ParseSelection("", "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "invalid render configuration")
			return
		}
	}
	selection, err = render.SelectForDoc(selection, s.Blob != nil, st.Doc)
	if err != nil {
		var unsupported *render.UnsupportedRenderFeatureError
		if errors.As(err, &unsupported) {
			writeStructuredError(
				w, http.StatusUnprocessableEntity,
				"UNSUPPORTED_RENDER_FEATURE", err.Error(),
			)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// 编译期错误（空时间线等）在启动 Workflow 前就拦下，给出 422 而不是
	// 让 Workflow 启动后立刻失败。
	var cerr error
	if selection.Executor == render.ExecutorLocal {
		_, cerr = render.CompileManaged(st.Doc)
	} else {
		_, cerr = render.Compile(st.Doc)
	}
	if cerr != nil {
		writeError(w, http.StatusUnprocessableEntity, cerr.Error())
		return
	}

	renderKey, err := render.RenderKey(st.Doc, selection)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute render key")
		return
	}
	_, err = s.Temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:                    render.WorkflowID(ref.ProjectID, renderKey),
		TaskQueue:             s.TaskQueue,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, render.WorkflowName, render.Input{
		ProjectID: ref.ProjectID,
		RenderKey: renderKey,
		Revision:  st.Revision,
		Doc:       st.Doc,
		Executor:  selection.Executor,
		Profile:   selection.Profile,
	})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if err != nil && !errors.As(err, &alreadyStarted) {
		writeError(w, http.StatusBadGateway, "failed to start render workflow")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"renderKey": renderKey,
		"revision":  st.Revision,
		"status":    "accepted",
	})
}

// handleRenderStatus 查询渲染进度/结果（前端短轮询）。
func (s *Server) handleRenderStatus(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	renderKey := r.PathValue("renderKey")
	workflowID := render.WorkflowID(ref.ProjectID, renderKey)

	desc, err := s.Temporal.DescribeWorkflowExecution(r.Context(), workflowID, "")
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "render not found")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to describe render")
		return
	}

	switch desc.GetWorkflowExecutionInfo().GetStatus() {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		payload := map[string]any{"renderKey": renderKey, "status": "running"}
		if ev, qerr := s.Temporal.QueryWorkflow(r.Context(), workflowID, "", render.QueryProgress); qerr == nil {
			var p render.Progress
			if ev.Get(&p) == nil {
				payload["progress"] = p
			}
		}
		writeJSON(w, http.StatusOK, payload)

	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		var out render.Output
		if err := s.Temporal.GetWorkflow(r.Context(), workflowID, "").Get(r.Context(), &out); err != nil {
			writeError(w, http.StatusBadGateway, "failed to load render result")
			return
		}
		outputURI := out.OutputURI
		if out.Executor == render.ExecutorLocal || out.OutputKey != "" {
			outputURI = render.ContentURI(ref.ProjectID, renderKey)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"renderKey": renderKey,
			"status":    "completed",
			"outputUri": outputURI,
			"revision":  out.Revision,
			"warnings":  out.Warnings,
		})

	default:
		// FAILED / TERMINATED / TIMED_OUT / CANCELED / CONTINUED_AS_NEW
		payload := map[string]any{
			"renderKey":      renderKey,
			"status":         "failed",
			"workflowStatus": desc.GetWorkflowExecutionInfo().GetStatus().String(),
		}
		if err := s.Temporal.GetWorkflow(r.Context(), workflowID, "").Get(r.Context(), nil); err != nil {
			reason := err.Error()
			if len(reason) > 1024 {
				reason = reason[:1024]
			}
			payload["error"] = reason
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func (s *Server) handleRenderContent(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	if s.Blob == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "blob storage is not configured",
			"code":    "BLOB_NOT_CONFIGURED",
			"missing": blob.MissingConfiguration(),
		})
		return
	}
	renderKey := r.PathValue("renderKey")
	if !validRenderKey(renderKey) {
		writeError(w, http.StatusBadRequest, "invalid render key")
		return
	}
	workflowID := render.WorkflowID(ref.ProjectID, renderKey)
	desc, err := s.Temporal.DescribeWorkflowExecution(r.Context(), workflowID, "")
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "render not found")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to describe render")
		return
	}
	if desc.GetWorkflowExecutionInfo().GetStatus() != enums.WORKFLOW_EXECUTION_STATUS_COMPLETED {
		writeError(w, http.StatusConflict, "render is not completed")
		return
	}
	var out render.Output
	if err := s.Temporal.GetWorkflow(r.Context(), workflowID, "").Get(r.Context(), &out); err != nil {
		writeError(w, http.StatusBadGateway, "failed to load render result")
		return
	}
	expectedKey := blob.OutputKey(ref.ProjectID, renderKey)
	if out.Executor != render.ExecutorLocal && out.OutputKey == "" {
		writeError(w, http.StatusNotFound, "render content is not stored locally")
		return
	}
	if out.OutputKey != "" && out.OutputKey != expectedKey {
		writeError(w, http.StatusForbidden, "render output key is outside project scope")
		return
	}
	streamBlobObject(w, r, s.Blob, expectedKey, renderKey+".mp4", ref.ProjectID, "render")
}

func validRenderKey(value string) bool {
	if len(value) != 16 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
