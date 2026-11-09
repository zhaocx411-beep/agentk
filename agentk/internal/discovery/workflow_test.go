package discovery

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestRegisterWorkflowPassesManifest(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var got RegisterActivityInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input RegisterActivityInput) error {
			got = input
			return nil
		},
		activity.RegisterOptions{Name: RegisterActivityType},
	)

	env.ExecuteWorkflow(RegisterWorkflow,
		manifestObject(t, discoveryManifest("business")))
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if got.Manifest["taskQueue"] != "business" {
		t.Fatalf("manifest = %+v", got.Manifest)
	}
}
