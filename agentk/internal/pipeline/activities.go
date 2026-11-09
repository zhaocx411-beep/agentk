package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/execution"
	"agentk/internal/temporalx"
	"agentk/internal/types"
)

type Activities struct {
	store  Store
	deltas deltabuf.Buffer
}

func NewActivities(store Store, deltas deltabuf.Buffer) *Activities {
	return &Activities{store: store, deltas: deltas}
}

func (a *Activities) BeginExecutionActivity(ctx context.Context, input WorkflowInput) error {
	const op = "begin pipeline execution"
	if input.Execution.ID == "" || input.Execution.Session.SessionID == 0 ||
		input.Execution.Session.SessionPublicID == "" || input.Name == "" || input.CapabilityID == "" {
		return temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput(
				"executionId, sessionId, sessionPublicId, name and capabilityId are required"))
	}
	if a.store == nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("pipeline store is required")))
	}
	sess, err := a.store.GetSession(ctx, input.Execution.Session.SessionID)
	if err != nil {
		return temporalx.ClassifyActivityError(op, err)
	}
	if sess.PublicID != input.Execution.Session.SessionPublicID {
		return temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("session id/public id mismatch"))
	}
	if a.deltas != nil {
		if err := a.deltas.Begin(ctx, input.Execution.ID); err != nil {
			log.Printf("pipeline: begin execution stream %s: %v", input.Execution.ID, err)
		}
	}
	payload := event.ExecutionStarted{
		ExecutionKind:  types.ExecutionKindPipeline,
		ProfileVersion: execution.ProfileVersion(sess.Profile),
		Pipeline: &event.PipelineStarted{
			Name: input.Name, CapabilityID: input.CapabilityID, Args: event.RawJSON(input.Args),
		},
	}
	if err := a.appendEvents(ctx, input.Execution, execution.BatchID(input.Execution.ID, "started"), payload); err != nil {
		return temporalx.ClassifyActivityError(op+": append event", err)
	}
	return nil
}

func (a *Activities) FinishExecutionActivity(
	ctx context.Context, input FinishExecutionInput,
) error {
	const op = "finish pipeline execution"
	if input.Status != types.ExecutionStatusCompleted &&
		input.Status != types.ExecutionStatusFailed &&
		input.Status != types.ExecutionStatusCancelled {
		return temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("invalid terminal status %q", input.Status))
	}
	payload := event.ExecutionFinished{
		Status: input.Status,
		Error:  input.Error,
		Pipeline: &event.PipelineFinished{
			Output: event.RawJSON(input.Output),
		},
	}
	if err := a.appendEvents(ctx, input.Execution,
		execution.BatchID(input.Execution.ID, "finished"), payload); err != nil {
		return temporalx.ClassifyActivityError(op+": append event", err)
	}
	if a.deltas != nil {
		if err := a.deltas.End(ctx, input.Execution.ID); err != nil {
			log.Printf("pipeline: end execution stream %s: %v", input.Execution.ID, err)
		}
	}
	return nil
}

func (a *Activities) appendEvents(
	ctx context.Context,
	ref types.ExecutionRef,
	batchID event.BatchID,
	payloads ...event.Payload,
) error {
	drafts := make([]event.Draft, len(payloads))
	for i, payload := range payloads {
		drafts[i] = event.Draft{Payload: payload}
	}
	events, err := a.store.AppendEvents(
		ctx, ref.Session.SessionID, ref.ID, batchID, drafts,
	)
	if err != nil {
		return err
	}
	if a.deltas == nil {
		return nil
	}
	for _, ev := range events {
		raw, err := json.Marshal(ev)
		if err != nil {
			log.Printf("pipeline: marshal event %s for execution stream: %v", ev.ID, err)
			continue
		}
		if err := a.deltas.Append(ctx, ref.ID, deltabuf.Item{
			Kind: string(ev.Payload.Kind()), EventID: ev.ID, Data: raw,
		}); err != nil {
			log.Printf("pipeline: append event %s to execution stream: %v", ev.ID, err)
		}
	}
	return nil
}
