package protocoltest

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	commonv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/common/v1"
	invocationv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/invocation/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAgentKServiceMethods(t *testing.T) {
	service := apiv1.File_agentk_api_v1_service_proto.Services().ByName("AgentKService")
	if service == nil {
		t.Fatal("AgentKService descriptor is missing")
	}

	var got []string
	for i := range service.Methods().Len() {
		method := service.Methods().Get(i)
		got = append(got, string(method.Name()))
		if method.Name() == "WatchEvents" {
			if !method.IsStreamingServer() || method.IsStreamingClient() {
				t.Fatal("WatchEvents must be server-streaming only")
			}
		} else if method.IsStreamingServer() || method.IsStreamingClient() {
			t.Fatalf("%s must be unary", method.Name())
		}
	}
	slices.Sort(got)
	want := []string{
		"CancelExecution",
		"CreateSession",
		"GetExecution",
		"InvokeCapability",
		"ListCapabilities",
		"ListEvents",
		"ListPipelines",
		"ListExecutions",
		"ListSessions",
		"StartExecution",
		"WatchEvents",
		"WhoAmI",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("AgentKService methods = %v, want %v", got, want)
	}
}

func TestInvokeCapabilityContract(t *testing.T) {
	messages := apiv1.File_agentk_api_v1_service_proto.Messages()
	request := messages.ByName("InvokeCapabilityRequest")
	response := messages.ByName("InvokeCapabilityResponse")
	if request == nil || response == nil {
		t.Fatal("InvokeCapability request or response message is missing")
	}
	assertFieldNumber(t, request, "capability_id", 1)
	assertFieldNumber(t, request, "input", 2)
	assertFieldNumber(t, response, "output", 1)
	if got := request.Fields().ByName("input").Message().FullName(); got != "google.protobuf.Struct" {
		t.Fatalf("InvokeCapabilityRequest.input = %s", got)
	}
	if got := response.Fields().ByName("output").Message().FullName(); got != "google.protobuf.Value" {
		t.Fatalf("InvokeCapabilityResponse.output = %s", got)
	}
}

func TestEventAndWatchOneofBinaryRoundTrip(t *testing.T) {
	event := &apiv1.Event{
		EventId:     "evt-1",
		ExecutionId: "execution-1",
		Payload: &apiv1.Event_ToolExecSucceeded{
			ToolExecSucceeded: &apiv1.ToolExecSucceeded{
				ToolCallId: "call-1",
				Result:     structpb.NewStringValue("ok"),
			},
		},
	}
	var decodedEvent apiv1.Event
	roundTripBinary(t, event, &decodedEvent)
	if _, ok := decodedEvent.GetPayload().(*apiv1.Event_ToolExecSucceeded); !ok {
		t.Fatalf("event payload type = %T", decodedEvent.GetPayload())
	}

	watch := &apiv1.WatchEventsResponse{
		Frame: &apiv1.WatchEventsResponse_Delta{
			Delta: &apiv1.Delta{
				ExecutionId: "execution-1",
				RoundId:     "round-1",
				Generation:  "generation-1",
				Text:        "hello",
			},
		},
	}
	var decodedWatch apiv1.WatchEventsResponse
	roundTripBinary(t, watch, &decodedWatch)
	if _, ok := decodedWatch.GetFrame().(*apiv1.WatchEventsResponse_Delta); !ok {
		t.Fatalf("watch frame type = %T", decodedWatch.GetFrame())
	}

	execution := &apiv1.Execution{
		Id:     "run-1",
		Status: apiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING,
		Kind: &apiv1.Execution_Pipeline{
			Pipeline: &apiv1.PipelineExecution{Name: "storyboard_film"},
		},
	}
	var decodedExecution apiv1.Execution
	roundTripBinary(t, execution, &decodedExecution)
	if _, ok := decodedExecution.GetKind().(*apiv1.Execution_Pipeline); !ok {
		t.Fatalf("execution kind = %T", decodedExecution.GetKind())
	}
}

func TestStartExecutionInputOneofBinaryRoundTrip(t *testing.T) {
	request := &apiv1.StartExecutionRequest{
		SessionId: "session-1",
		Input: &apiv1.StartExecutionRequest_Answers{
			Answers: &apiv1.AgentAnswersExecutionInput{
				ToolCallId: "call-1",
				Answers: []*apiv1.AnswerInput{{
					QuestionId: "q1",
					Selected:   []string{"卡通"},
				}},
			},
		},
	}
	var decoded apiv1.StartExecutionRequest
	roundTripBinary(t, request, &decoded)
	answers, ok := decoded.GetInput().(*apiv1.StartExecutionRequest_Answers)
	if !ok {
		t.Fatalf("execution input = %T", decoded.GetInput())
	}
	if got := answers.Answers.GetAnswers(); len(got) != 1 ||
		got[0].GetQuestionId() != "q1" ||
		!slices.Equal(got[0].GetSelected(), []string{"卡通"}) {
		t.Fatalf("answers = %+v", got)
	}

	input := request.ProtoReflect().Descriptor().Oneofs().ByName("input")
	for _, name := range []protoreflect.Name{"message", "answers", "pipeline"} {
		if input.Fields().ByName(name) == nil {
			t.Fatalf("StartExecution input %q is missing", name)
		}
	}
}

func TestMediaInputAndProtocolStateRoundTrip(t *testing.T) {
	request := &apiv1.StartExecutionRequest{
		SessionId: "session-vision",
		Input: &apiv1.StartExecutionRequest_Message{Message: &apiv1.AgentMessageExecutionInput{
			Text: "inspect",
			Attachments: []*apiv1.MediaAttachment{{
				Type: "image", Uri: "artifact://cat.png", MimeType: "image/png",
			}, {
				Type: "video", Uri: "https://cdn.example.test/clip.mp4", MimeType: "video/mp4",
			}},
		}},
	}
	var decoded apiv1.StartExecutionRequest
	roundTripBinary(t, request, &decoded)
	message := decoded.GetMessage()
	if message.GetText() != "inspect" || len(message.GetAttachments()) != 2 ||
		message.GetAttachments()[0].GetUri() != "artifact://cat.png" ||
		message.GetAttachments()[1].GetType() != "video" ||
		message.GetAttachments()[1].GetMimeType() != "video/mp4" {
		t.Fatalf("message = %+v", message)
	}
	mediaMessages := apiv1.File_agentk_api_v1_event_proto.Messages()
	if mediaMessages.ByName("MediaAttachment") == nil {
		t.Fatal("event proto is missing MediaAttachment")
	}
	if mediaMessages.ByName("ImageAttachment") != nil {
		t.Fatal("event proto retains ImageAttachment")
	}

	event := &apiv1.Event{Payload: &apiv1.Event_AssistantToolCall{
		AssistantToolCall: &apiv1.AssistantToolCall{
			ToolCallId: "call-internal", ProviderToolCallId: "call-provider",
			ToolName: "view_images", ProtocolState: "opaque-signature",
		},
	}}
	var decodedEvent apiv1.Event
	roundTripBinary(t, event, &decodedEvent)
	call := decodedEvent.GetAssistantToolCall()
	if call.GetProviderToolCallId() != "call-provider" || call.GetProtocolState() != "opaque-signature" {
		t.Fatalf("tool call = %+v", call)
	}

	profile := &apiv1.Profile{DefaultModel: "gpt-5.4", MaxImagesPerRound: 7}
	var decodedProfile apiv1.Profile
	roundTripBinary(t, profile, &decodedProfile)
	if decodedProfile.GetMaxImagesPerRound() != 7 {
		t.Fatalf("max_images_per_round = %d, want 7", decodedProfile.GetMaxImagesPerRound())
	}
}

func TestProfileContract(t *testing.T) {
	fields := (&apiv1.Profile{}).ProtoReflect().Descriptor().Fields()
	if fields.ByName("http_tools") != nil {
		t.Fatal("Profile retains removed http_tools field")
	}
	if field := fields.ByName("max_tool_rounds"); field == nil || field.Number() != 6 {
		t.Fatalf("Profile.max_tool_rounds = %v, want field 6", field)
	}
	if field := fields.ByName("max_images_per_round"); field == nil || field.Number() != 7 {
		t.Fatalf("Profile.max_images_per_round = %v, want field 7", field)
	}
	messages := apiv1.File_agentk_api_v1_profile_proto.Messages()
	for _, name := range []protoreflect.Name{
		"HttpTool",
		"HttpRequest",
		"HttpToolAsync",
		"HttpPollTarget",
	} {
		if messages.ByName(name) != nil {
			t.Fatalf("profile proto retains removed message %q", name)
		}
	}
	if apiv1.File_agentk_api_v1_model_proto.Enums().ByName("HttpMethod") != nil {
		t.Fatal("API proto retains removed HttpMethod enum")
	}
}

func TestJobProtocolIsRemoved(t *testing.T) {
	payload := (&apiv1.Event{}).ProtoReflect().Descriptor().Oneofs().ByName("payload")
	for _, name := range []protoreflect.Name{
		"job_submitted",
		"job_completed",
		"job_failed",
	} {
		if payload.Fields().ByName(name) != nil {
			t.Fatalf("Event retains removed payload %q", name)
		}
	}
	messages := apiv1.File_agentk_api_v1_event_proto.Messages()
	for _, name := range []protoreflect.Name{
		"WorkflowExec",
		"JobSubmitted",
		"JobCompleted",
		"JobFailed",
	} {
		if messages.ByName(name) != nil {
			t.Fatalf("event proto retains removed message %q", name)
		}
	}
	if apiv1.File_agentk_api_v1_model_proto.Enums().ByName("WakePolicy") != nil {
		t.Fatal("API proto retains removed WakePolicy enum")
	}
}

func TestEdition2023UsesImplicitScalarPresence(t *testing.T) {
	files := []protoreflect.FileDescriptor{
		apiv1.File_agentk_api_v1_event_proto,
		apiv1.File_agentk_api_v1_model_proto,
		apiv1.File_agentk_api_v1_profile_proto,
		apiv1.File_agentk_api_v1_service_proto,
		commonv1.File_agentk_common_v1_common_proto,
		invocationv1.File_agentk_invocation_v1_invocation_proto,
	}
	for _, file := range files {
		descriptor := protodesc.ToFileDescriptorProto(file)
		if descriptor.GetEdition() != descriptorpb.Edition_EDITION_2023 {
			t.Errorf("%s edition = %s, want EDITION_2023", file.Path(), descriptor.GetEdition())
		}
		walkMessages(file.Messages(), func(message protoreflect.MessageDescriptor) {
			fields := message.Fields()
			for i := range fields.Len() {
				field := fields.Get(i)
				if field.IsList() || field.IsMap() ||
					field.Message() != nil || field.ContainingOneof() != nil {
					continue
				}
				if field.HasPresence() {
					t.Errorf("%s unexpectedly has scalar presence", field.FullName())
				}
			}
		})
	}

	createSession := (&apiv1.CreateSessionRequest{}).ProtoReflect().Descriptor()
	if profile := createSession.Fields().ByName("profile"); profile == nil || !profile.HasPresence() {
		t.Fatal("CreateSessionRequest.profile must retain message presence")
	}
	startExecution := (&apiv1.StartExecutionRequest{}).ProtoReflect().Descriptor()
	input := startExecution.Oneofs().ByName("input")
	if input == nil || input.Fields().Len() != 3 {
		t.Fatal("StartExecutionRequest.input oneof changed")
	}
}

func TestAnswerInputContract(t *testing.T) {
	messages := apiv1.File_agentk_api_v1_service_proto.Messages()
	if messages.ByName("QuestionAnswer") != nil {
		t.Fatal("service proto retains removed QuestionAnswer")
	}
	answerInput := messages.ByName("AnswerInput")
	if answerInput == nil {
		t.Fatal("service proto is missing AnswerInput")
	}
	assertFieldNumber(t, answerInput, "question_id", 1)
	assertFieldNumber(t, answerInput, "selected", 2)

	answers := messages.ByName("AgentAnswersExecutionInput").Fields().ByName("answers")
	if answers == nil || answers.Number() != 2 || answers.Message() != answerInput {
		t.Fatalf("AgentAnswersExecutionInput.answers = %v, want field 2 of AnswerInput", answers)
	}

	eventAnswer := apiv1.File_agentk_api_v1_event_proto.Messages().ByName("Answer")
	if eventAnswer == nil {
		t.Fatal("event proto is missing Answer")
	}
	assertFieldNumber(t, eventAnswer, "question_id", 1)
	assertFieldNumber(t, eventAnswer, "question", 2)
	assertFieldNumber(t, eventAnswer, "selected", 3)
}

func TestListPaginationContract(t *testing.T) {
	tests := []struct {
		request  protoreflect.MessageDescriptor
		response protoreflect.MessageDescriptor
		resource string
	}{
		{messageDescriptor(&apiv1.ListSessionsRequest{}), messageDescriptor(&apiv1.ListSessionsResponse{}), "sessions"},
		{messageDescriptor(&apiv1.ListEventsRequest{}), messageDescriptor(&apiv1.ListEventsResponse{}), "events"},
		{messageDescriptor(&apiv1.ListPipelinesRequest{}), messageDescriptor(&apiv1.ListPipelinesResponse{}), "pipelines"},
		{messageDescriptor(&apiv1.ListExecutionsRequest{}), messageDescriptor(&apiv1.ListExecutionsResponse{}), "executions"},
		{messageDescriptor(&apiv1.ListCapabilitiesRequest{}), messageDescriptor(&apiv1.ListCapabilitiesResponse{}), "capabilities"},
	}

	for _, tt := range tests {
		t.Run(string(tt.request.Name()), func(t *testing.T) {
			pageSize := tt.request.Fields().ByName("page_size")
			pageToken := tt.request.Fields().ByName("page_token")
			if pageSize == nil || pageSize.Kind() != protoreflect.Int32Kind {
				t.Fatalf("page_size = %v, want int32", pageSize)
			}
			if pageToken == nil || pageToken.Kind() != protoreflect.StringKind {
				t.Fatalf("page_token = %v, want string", pageToken)
			}
			resources := tt.response.Fields().ByName(protoreflect.Name(tt.resource))
			next := tt.response.Fields().ByName("next_page_token")
			if resources == nil || resources.Number() != 1 || !resources.IsList() {
				t.Fatalf("%s = %v, want repeated field 1", tt.resource, resources)
			}
			if next == nil || next.Kind() != protoreflect.StringKind {
				t.Fatalf("next_page_token = %v, want string", next)
			}
		})
	}

	original := &apiv1.ListEventsRequest{
		SessionId: "session-1",
		PageSize:  20,
		PageToken: "event-20",
	}
	var decoded apiv1.ListEventsRequest
	roundTripBinary(t, original, &decoded)
	if decoded.GetSessionId() != "session-1" || decoded.GetPageSize() != 20 ||
		decoded.GetPageToken() != "event-20" {
		t.Fatalf("ListEventsRequest round trip = %+v", &decoded)
	}
}

func TestUnifiedExecutionDescriptors(t *testing.T) {
	execution := (&apiv1.Execution{}).ProtoReflect().Descriptor()
	kind := execution.Oneofs().ByName("kind")
	if kind == nil || kind.Fields().Len() != 2 ||
		kind.Fields().ByName("agent") == nil ||
		kind.Fields().ByName("pipeline") == nil {
		t.Fatalf("Execution kind oneof = %v", kind)
	}

	event := (&apiv1.Event{}).ProtoReflect().Descriptor()
	if event.Fields().ByName("event_id") == nil || event.Fields().ByName("execution_id") == nil {
		t.Fatal("Event event_id/execution_id is missing")
	}
	for _, name := range []protoreflect.Name{"seq", "run_id"} {
		if event.Fields().ByName(name) != nil {
			t.Fatalf("Event retains removed field %q", name)
		}
	}
	payload := event.Oneofs().ByName("payload")
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{
		"execution_started":   16,
		"execution_finished":  17,
		"invocation_progress": 18,
	} {
		field := payload.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("Event payload %q = %v, want field %d", name, field, number)
		}
	}
	for _, name := range []protoreflect.Name{
		"pipeline_started",
		"pipeline_completed",
		"pipeline_failed",
		"interrupted",
		"error",
	} {
		if payload.Fields().ByName(name) != nil {
			t.Fatalf("Event retains pipeline-only lifecycle payload %q", name)
		}
	}
	roundStarted := (&apiv1.RoundStarted{}).ProtoReflect().Descriptor()
	if roundStarted.Fields().ByName("folded_seq") != nil {
		t.Fatal("RoundStarted retains removed folded_seq")
	}
	if profileVersion := roundStarted.Fields().ByName("profile_version"); profileVersion == nil || profileVersion.Number() != 3 {
		t.Fatalf("RoundStarted.profile_version = %v, want field 3", profileVersion)
	}

	listEvents := (&apiv1.ListEventsRequest{}).ProtoReflect().Descriptor().Fields()
	if listEvents.ByName("after_seq") != nil || listEvents.ByName("limit") != nil {
		t.Fatal("ListEventsRequest retains seq pagination")
	}
	watchEvents := (&apiv1.WatchEventsRequest{}).ProtoReflect().Descriptor().Fields()
	if watchEvents.ByName("execution_id") == nil || watchEvents.ByName("after_seq") != nil {
		t.Fatal("WatchEventsRequest must select an execution without a seq cursor")
	}
}

func TestStructAndValueUseNativeProtoJSON(t *testing.T) {
	args, err := structpb.NewStruct(map[string]any{
		"quality": "high",
		"count":   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := &apiv1.PipelineExecution{
		Args:   args,
		Output: structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{structpb.NewStringValue("clip")}}),
	}
	raw, err := protojson.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document["args"].(map[string]any); !ok {
		t.Fatalf("args ProtoJSON = %s, want object", raw)
	}
	if _, ok := document["output"].([]any); !ok {
		t.Fatalf("output ProtoJSON = %s, want array", raw)
	}
}

func TestCapabilityLastReportedAtRoundTrip(t *testing.T) {
	reportedAt := timestamppb.New(time.Date(
		2026, time.August, 18, 9, 30, 0, 123000000, time.UTC,
	))
	message := &apiv1.Capability{
		Id: "contrib.example.render", LastReportedAt: reportedAt,
	}
	var decoded apiv1.Capability
	roundTripBinary(t, message, &decoded)
	if !proto.Equal(message, &decoded) {
		t.Fatalf("binary round trip = %v, want %v", &decoded, message)
	}
	raw, err := protojson.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if got := document["lastReportedAt"]; got != "2026-08-18T09:30:00.123Z" {
		t.Fatalf("lastReportedAt = %v", got)
	}
}

func TestCapabilityVariantContract(t *testing.T) {
	capability := (&apiv1.Capability{}).ProtoReflect().Descriptor()
	assertFieldNumber(t, capability, "variants", 9)
	variant := (&apiv1.CapabilityVariant{}).ProtoReflect().Descriptor()
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{
		"lane_id": 1, "description": 2, "input_schema": 3, "output_schema": 4,
		"timeout": 5, "tool": 6, "last_reported_at": 7,
	} {
		assertFieldNumber(t, variant, name, number)
	}
	for _, name := range []protoreflect.Name{"kind", "temporal_type", "task_queue"} {
		if variant.Fields().ByName(name) != nil {
			t.Fatalf("CapabilityVariant exposes internal field %q", name)
		}
	}
	request := (&apiv1.ListCapabilitiesRequest{}).ProtoReflect().Descriptor()
	assertFieldNumber(t, request, "include_variants", 3)
}

func TestAppIDRenamesPreserveProtobufWireFields(t *testing.T) {
	const value = "0198d72a-7c00-7000-8000-000000000001"
	tests := []struct {
		name   string
		field  protoreflect.FieldNumber
		newMsg func() proto.Message
		got    func(proto.Message) string
	}{
		{
			name: "Session", field: 2,
			newMsg: func() proto.Message { return &apiv1.Session{} },
			got:    func(message proto.Message) string { return message.(*apiv1.Session).GetAppId() },
		},
		{
			name: "WhoAmIResponse", field: 2,
			newMsg: func() proto.Message { return &apiv1.WhoAmIResponse{} },
			got:    func(message proto.Message) string { return message.(*apiv1.WhoAmIResponse).GetAppId() },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := protowire.AppendTag(nil, tt.field, protowire.BytesType)
			wire = protowire.AppendString(wire, value)
			message := tt.newMsg()
			if err := proto.Unmarshal(wire, message); err != nil {
				t.Fatal(err)
			}
			if got := tt.got(message); got != value {
				t.Fatalf("decoded field %d = %q, want %q", tt.field, got, value)
			}
		})
	}
}

func TestConsumerCapabilityExcludesInternalFields(t *testing.T) {
	capability := (&apiv1.Capability{}).ProtoReflect().Descriptor()
	fields := capability.Fields()
	for _, name := range []protoreflect.Name{
		"kind",
		"temporal_type",
		"task_queue",
		"namespace",
		"tool_ref",
		"pipeline_ref",
		"pipeline",
	} {
		if fields.ByName(name) != nil {
			t.Fatalf("consumer Capability exposes internal field %q", name)
		}
	}
	if field := fields.ByName("tool"); field == nil || field.Number() != 7 {
		t.Fatalf("consumer Capability.tool = %v, want field 7", field)
	}
	if field := fields.ByName("last_reported_at"); field == nil || field.Number() != 8 {
		t.Fatalf("consumer Capability.last_reported_at = %v, want field 8", field)
	}
	if apiv1.File_agentk_api_v1_model_proto.Messages().ByName("CapabilityPipeline") != nil {
		t.Fatal("API proto retains removed CapabilityPipeline message")
	}
	toolDescriptor := (&apiv1.CapabilityTool{}).ProtoReflect().Descriptor()
	tool := toolDescriptor.Fields()
	if tool.ByName("name") != nil {
		t.Fatal("CapabilityTool retains removed name field")
	}
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{
		"description":  1,
		"max_attempts": 2,
	} {
		field := tool.ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("CapabilityTool.%s = %v, want field %d", name, field, number)
		}
	}
	if tool.ByName("mode") != nil {
		t.Fatal("CapabilityTool retains removed mode")
	}
	if apiv1.File_agentk_api_v1_model_proto.Enums().ByName("CapabilityToolMode") != nil {
		t.Fatal("API proto retains removed CapabilityToolMode enum")
	}
}

func TestProfileAndPipelineDTOsUseCapabilityIdentity(t *testing.T) {
	for _, message := range []proto.Message{
		&apiv1.ProfileTool{},
		&apiv1.ProfilePipeline{},
	} {
		fields := message.ProtoReflect().Descriptor().Fields()
		if fields.ByName("ref") != nil {
			t.Fatalf("%s retains removed ref field", message.ProtoReflect().Descriptor().Name())
		}
		if field := fields.ByName("capability"); field == nil || field.Number() != 1 {
			t.Fatalf("%s.capability = %v, want field 1", message.ProtoReflect().Descriptor().Name(), field)
		}
	}

	for _, tt := range []struct {
		message      proto.Message
		capabilityID protoreflect.FieldNumber
		name         protoreflect.FieldNumber
	}{
		{message: &apiv1.Pipeline{}, capabilityID: 6, name: 1},
		{message: &apiv1.PipelineExecution{}, capabilityID: 4, name: 1},
		{message: &apiv1.PipelineExecutionStarted{}, capabilityID: 3, name: 1},
	} {
		fields := tt.message.ProtoReflect().Descriptor().Fields()
		if field := fields.ByName("capability_id"); field == nil || field.Number() != tt.capabilityID {
			t.Fatalf("%s.capability_id = %v, want field %d", tt.message.ProtoReflect().Descriptor().Name(), field, tt.capabilityID)
		}
		if field := fields.ByName("name"); field == nil || field.Number() != tt.name {
			t.Fatalf("%s.name = %v, want field %d", tt.message.ProtoReflect().Descriptor().Name(), field, tt.name)
		}
	}

	input := (&apiv1.PipelineExecutionInput{}).ProtoReflect().Descriptor().Fields()
	if input.ByName("capability_id") != nil || input.ByName("pipeline") == nil {
		t.Fatal("PipelineExecutionInput must continue selecting by profile pipeline name")
	}
}

func roundTripBinary(t *testing.T, input proto.Message, output proto.Message) {
	t.Helper()
	raw, err := proto.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(raw, output); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(input, output) {
		t.Fatalf("binary round-trip mismatch:\ninput:  %v\noutput: %v", input, output)
	}
}

func messageDescriptor(message proto.Message) protoreflect.MessageDescriptor {
	return message.ProtoReflect().Descriptor()
}

func walkMessages(
	messages protoreflect.MessageDescriptors,
	visit func(protoreflect.MessageDescriptor),
) {
	for i := range messages.Len() {
		message := messages.Get(i)
		visit(message)
		walkMessages(message.Messages(), visit)
	}
}

func assertFieldNumber(
	t *testing.T,
	message protoreflect.MessageDescriptor,
	name protoreflect.Name,
	want protoreflect.FieldNumber,
) {
	t.Helper()
	field := message.Fields().ByName(name)
	if field == nil || field.Number() != want {
		t.Fatalf("%s.%s = %v, want field %d", message.FullName(), name, field, want)
	}
}
