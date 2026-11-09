package temporal

import (
	"context"
	"os"
	"testing"
	"time"

	workflowservice "go.temporal.io/api/workflowservice/v1"
)

func TestDialKESSIntegration(t *testing.T) {
	if os.Getenv("AGENTK_SDK_TEST_KESS") != "1" {
		t.Skip("AGENTK_SDK_TEST_KESS is not enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.WorkflowService().DescribeNamespace(
		ctx,
		&workflowservice.DescribeNamespaceRequest{Namespace: temporalNamespace},
	)
	if err != nil {
		t.Fatalf("describe namespace %q: %v", temporalNamespace, err)
	}
}
