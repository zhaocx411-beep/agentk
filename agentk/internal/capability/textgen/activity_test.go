package textgen

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"agentk/internal/chat"
)

type fakeCatalog map[string]chat.Client

func (c fakeCatalog) ChatClient(name string) (chat.Client, bool) {
	client, ok := c[name]
	return client, ok
}

type fakeModel struct {
	text string
	last chat.Request
}

func (m *fakeModel) Stream(_ context.Context, request chat.Request) (chat.Response, error) {
	m.last = request
	return chat.Response{
		Stream: func(func(chat.Delta, error) bool) {},
		Result: func() (chat.Result, error) {
			return chat.Result{Blocks: []chat.Block{{Kind: chat.BlockText, Text: m.text}}}, nil
		},
	}, nil
}

func execute(t *testing.T, activity *Activity, input Input) (Output, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(activity.Run)
	value, err := env.ExecuteActivity(activity.Run, input)
	if err != nil {
		return Output{}, err
	}
	var output Output
	if err := value.Get(&output); err != nil {
		t.Fatal(err)
	}
	return output, nil
}

func TestActivityRoutesExplicitModelAndDerivesIdempotency(t *testing.T) {
	model := &fakeModel{text: `{"ok":true}`}
	activity := NewActivity(fakeCatalog{"chat-cap": model})
	output, err := execute(t, activity, Input{
		Model: "chat-cap", Prompt: "return json",
		OutputSchema: json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(output.Result) != `{"ok":true}` || model.last.IdempotencyKey == "" {
		t.Fatalf("output = %s request = %+v", output.Result, model.last)
	}
}

func TestActivityRequiresModelAndRejectsUnknownRef(t *testing.T) {
	activity := NewActivity(fakeCatalog{"chat-cap": &fakeModel{text: "ok"}})
	for _, input := range []Input{
		{Prompt: "hello"},
		{Model: "missing", Prompt: "hello"},
	} {
		_, err := execute(t, activity, input)
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Fatalf("input = %+v err = %v", input, err)
		}
	}
}
