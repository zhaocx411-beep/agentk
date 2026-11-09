package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func workflowSpec(mutate func(*WorkflowBinding)) Spec {
	wb := WorkflowBinding{
		WorkflowType: "agentk.test.film",
	}
	if mutate != nil {
		mutate(&wb)
	}
	return Spec{
		Name: "film", Description: "成片",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding:     wb,
	}
}

// WorkflowBinding 的声明校验：type 必填。
func TestRegistryValidatesWorkflowBinding(t *testing.T) {
	if _, err := NewRegistry(workflowSpec(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(workflowSpec(func(wb *WorkflowBinding) {
		wb.WorkflowType = ""
	})); err == nil || !strings.Contains(err.Error(), "workflowType") {
		t.Fatalf("missing workflow type must fail, err = %v", err)
	}
}

// 绑定标签与进程内执行护栏：workflow 绑定不可进程内执行。
func TestWorkflowBindingKindAndRunGuard(t *testing.T) {
	spec := workflowSpec(nil)
	if spec.BindingKind() != BindWorkflow {
		t.Fatalf("kind = %q", spec.BindingKind())
	}
	if _, err := spec.Run(context.Background(), nil, Context{}); err == nil {
		t.Fatal("workflow binding must not run in-process")
	}
}
