package main

import (
	"context"
	"log"

	agentktemporal "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/temporal"
	agentkworker "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"
	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const pipelineDescription = "围绕主题生成分镜，并行生成各镜头视频片段，最后拼接成片。"

func main() {
	temporalClient, err := agentktemporal.Dial(context.Background())
	if err != nil {
		log.Fatalf("dial temporal through KESS: %v", err)
	}
	defer temporalClient.Close()

	temporalWorker := agentkworker.New(
		temporalClient,
		WorkerTaskQueue,
		temporalworker.Options{},
	)
	temporalWorker.RegisterWorkflowWithOptions(
		StoryboardFilmWorkflow,
		workflow.RegisterOptions{Name: WorkflowType},
	)
	temporalWorker.PublishWorkflow(sdkcapability.Capability{
		ID:             CapabilityID,
		TemporalType:   WorkflowType,
		Description:    pipelineDescription,
		InputSchema:    sdkcapability.SchemaOf[StoryboardInput](),
		OutputSchema:   sdkcapability.SchemaOf[StoryboardOutput](),
		TimeoutSeconds: 24 * 60 * 60,
		Tool:           &sdkcapability.Tool{},
	})

	log.Printf(
		"worker started: task queue %q, workflow %q",
		WorkerTaskQueue,
		WorkflowType,
	)
	if err := temporalWorker.Run(temporalworker.InterruptCh()); err != nil {
		log.Fatalf("worker exited: %v", err)
	}
}
