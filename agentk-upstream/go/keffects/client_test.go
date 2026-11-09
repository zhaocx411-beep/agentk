package keffects

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
	"google.golang.org/grpc"
)

type fakeWorkflowRPC struct {
	submitContext      context.Context
	submitRequest      *pb.SubmitRequest
	submitResponse     *pb.SubmitResponse
	submitErr          error
	querySubmitContext context.Context
	queryResultContext context.Context
	queryRequest       *pb.QueryRequest
	queryResponse      *pb.QueryResponse
}

func (f *fakeWorkflowRPC) Submit(
	ctx context.Context, request *pb.SubmitRequest, _ ...grpc.CallOption,
) (*pb.SubmitResponse, error) {
	f.submitContext = ctx
	f.submitRequest = request
	return f.submitResponse, f.submitErr
}

func (f *fakeWorkflowRPC) QuerySubmitRequest(
	ctx context.Context, request *pb.QueryRequest, _ ...grpc.CallOption,
) (*pb.SubmitRequest, error) {
	f.querySubmitContext = ctx
	f.queryRequest = request
	return &pb.SubmitRequest{}, nil
}

func (f *fakeWorkflowRPC) QueryResult(
	ctx context.Context, request *pb.QueryRequest, _ ...grpc.CallOption,
) (*pb.QueryResponse, error) {
	f.queryResultContext = ctx
	f.queryRequest = request
	return f.queryResponse, nil
}

func TestClientForwardsRequestLaneToWorkflowRPCs(t *testing.T) {
	rpc := &fakeWorkflowRPC{
		submitResponse: &pb.SubmitResponse{
			Code: CodeSuccess, WorkflowInstanceId: 42,
		},
		queryResponse: &pb.QueryResponse{Status: CodeProcessing},
	}
	client := newClientWithRPC(Config{TenantID: 99}, rpc)
	ctx, err := ksrpc.WithLane(context.Background(), "dev_x,PRT")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Submit(ctx, SubmitInput{
		RequestID: "submit", WorkflowID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.QuerySubmitRequest(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchResult(ctx, 42); err != nil {
		t.Fatal(err)
	}

	contexts := map[string]context.Context{
		"Submit":             rpc.submitContext,
		"QuerySubmitRequest": rpc.querySubmitContext,
		"FetchResult":        rpc.queryResultContext,
	}
	for method, callCtx := range contexts {
		lane, ok, err := ksrpc.LaneFromContext(callCtx)
		if err != nil || !ok || lane != "dev_x,PRT" {
			t.Errorf("%s lane = %q, present = %v, error = %v; want dev_x,PRT", method, lane, ok, err)
		}
	}
}

func TestClientDoesNotReadLaneFromEnvironment(t *testing.T) {
	t.Setenv("KWS_LANE_ID", "PRT")
	rpc := &fakeWorkflowRPC{submitResponse: &pb.SubmitResponse{
		Code: CodeSuccess, WorkflowInstanceId: 42,
	}}
	client := newClientWithRPC(Config{TenantID: 99}, rpc)

	if _, err := client.Submit(context.Background(), SubmitInput{
		RequestID: "submit", WorkflowID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	lane, ok, err := ksrpc.LaneFromContext(rpc.submitContext)
	if err != nil || ok || lane != "" {
		t.Fatalf("Submit lane = %q, present = %v, error = %v; want empty", lane, ok, err)
	}
}

func TestClientSubmitForwardsWorkflowConfiguration(t *testing.T) {
	rpc := &fakeWorkflowRPC{submitResponse: &pb.SubmitResponse{
		Code: CodeSuccess, WorkflowInstanceId: 42,
	}}
	client := newClientWithRPC(Config{TenantName: "agent"}, rpc)

	instanceID, err := client.Submit(context.Background(), SubmitInput{
		RequestID:    "request-1",
		WorkflowName: "video_workflow",
		Arguments:    []*pb.Argument{StringArgument("prompt", "hello")},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if instanceID != 42 {
		t.Fatalf("Submit() instance id = %d, want 42", instanceID)
	}
	if rpc.submitRequest.GetTenantName() != "agent" {
		t.Fatalf("tenant = %q, want agent", rpc.submitRequest.GetTenantName())
	}
	if rpc.submitRequest.GetWorkflowName() != "video_workflow" {
		t.Fatalf("workflow = %q, want video_workflow", rpc.submitRequest.GetWorkflowName())
	}
}

func TestClientSubmitForwardsNumericSelectors(t *testing.T) {
	rpc := &fakeWorkflowRPC{submitResponse: &pb.SubmitResponse{
		Code: CodeSuccess, WorkflowInstanceId: 42,
	}, queryResponse: &pb.QueryResponse{Status: CodeProcessing}}
	client := newClientWithRPC(Config{TenantID: 123}, rpc)

	if _, err := client.Submit(context.Background(), SubmitInput{
		RequestID: "request-1", WorkflowID: 456,
	}); err != nil {
		t.Fatal(err)
	}
	if rpc.submitRequest.GetTenantId() != 123 || rpc.submitRequest.GetWorkflowId() != 456 {
		t.Fatalf("submit request = %+v", rpc.submitRequest)
	}
	if _, err := client.FetchResult(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if rpc.queryRequest.GetTenantId() != 123 {
		t.Fatalf("query tenant id = %d, want 123", rpc.queryRequest.GetTenantId())
	}
}

func TestClientSubmitUsesWorkBucket(t *testing.T) {
	rpc := &fakeWorkflowRPC{submitResponse: &pb.SubmitResponse{
		Code: CodeSuccess, WorkflowInstanceId: 42,
	}}
	client := newClientWithRPC(Config{
		TenantName: "agent", OutputBucket: "business",
	}, rpc)

	if _, err := client.Submit(context.Background(), SubmitInput{
		RequestID: "request-1", WorkflowName: "video_workflow",
	}); err != nil {
		t.Fatal(err)
	}
	if got := rpc.submitRequest.GetOutputBucketName(); got != WorkBucket {
		t.Fatalf("output bucket = %q, want work", got)
	}
}

func TestClientSubmitValidatesRequiredFields(t *testing.T) {
	client := newClientWithRPC(Config{TenantName: "agent"}, &fakeWorkflowRPC{})
	for name, input := range map[string]SubmitInput{
		"request id":    {WorkflowName: "workflow"},
		"workflow name": {RequestID: "request"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := client.Submit(context.Background(), input); err == nil {
				t.Fatal("Submit() error = nil, want validation error")
			}
		})
	}
}

func TestClientSubmitReturnsResponseError(t *testing.T) {
	rpc := &fakeWorkflowRPC{submitResponse: &pb.SubmitResponse{
		Code: 400, Message: "invalid arguments",
	}}
	client := newClientWithRPC(Config{TenantName: "agent"}, rpc)

	_, err := client.Submit(context.Background(), SubmitInput{
		RequestID: "request-1", WorkflowName: "workflow",
	})
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Submit() error = %v, want *ResponseError", err)
	}
	if responseErr.Code != 400 {
		t.Fatalf("ResponseError.Code = %d, want 400", responseErr.Code)
	}
}

func TestClientFetchResultUsesConfiguredTenant(t *testing.T) {
	rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{Status: CodeProcessing}}
	client := newClientWithRPC(Config{
		TenantName:   "agent",
		QueryTimeout: time.Second,
	}, rpc)

	response, err := client.FetchResult(context.Background(), 42)
	if err != nil {
		t.Fatalf("FetchResult() error = %v", err)
	}
	if response.GetStatus() != CodeProcessing {
		t.Fatalf("status = %d, want %d", response.GetStatus(), CodeProcessing)
	}
	if rpc.queryRequest.GetTenantName() != "agent" {
		t.Fatalf("tenant = %q, want agent", rpc.queryRequest.GetTenantName())
	}
}

func TestClientFetchResultCopiesFromActualOutputBucket(t *testing.T) {
	for _, sourceBucket := range []string{WorkBucket, "submit-override"} {
		t.Run(sourceBucket, func(t *testing.T) {
			store := &fakeMediaStore{}
			rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{
				Status: CodeSuccess,
				Outputs: []*pb.Argument{MediaArgument(
					"res_video", sourceBucket, "outputs/video.mp4",
				)},
			}}
			client := newClientWithRPC(Config{
				TenantName: "agent", OutputBucket: "business",
				MediaStore: store,
			}, rpc)

			response, err := client.FetchResult(t.Context(), 42)
			if err != nil {
				t.Fatal(err)
			}
			media, err := FindMedia(response.Outputs, "res_video")
			if err != nil {
				t.Fatal(err)
			}
			if store.copyCalls != 1 ||
				store.copySrcBucket != sourceBucket ||
				store.copySrcKey != "outputs/video.mp4" ||
				store.copyDstBucket != "business" ||
				store.copyDstKey != "outputs/video.mp4" {
				t.Fatalf("copy = %+v", store)
			}
			if media.GetBucket() != "business" || media.GetKey() != "outputs/video.mp4" {
				t.Fatalf("media = %+v", media)
			}
		})
	}
}

func TestClientFetchResultCopiesMediaArrays(t *testing.T) {
	store := &fakeMediaStore{}
	faces := []*pb.Media{
		{Bucket: WorkBucket, Key: "outputs/face-1.jpg"},
		{Bucket: WorkBucket, Key: "outputs/face-2.jpg"},
	}
	rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{
		Status:  CodeSuccess,
		Outputs: []*pb.Argument{MediaValuesArgument("face_image_list", faces)},
	}}
	client := newClientWithRPC(Config{
		TenantName: "agent", OutputBucket: "business",
		MediaStore: store,
	}, rpc)

	response, err := client.FetchResult(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if store.copyCalls != 2 {
		t.Fatalf("copy calls = %d, want 2", store.copyCalls)
	}
	output, err := FindMediaValues(response.Outputs, "face_image_list")
	wantKeys := []string{"outputs/face-1.jpg", "outputs/face-2.jpg"}
	if err != nil || len(output) != len(wantKeys) {
		t.Fatalf("faces = %+v, error = %v", output, err)
	}
	for index, media := range output {
		if media.GetBucket() != "business" || media.GetKey() != wantKeys[index] {
			t.Fatalf("faces[%d] = %+v", index, media)
		}
	}
}

func TestClientFetchResultSkipsCopyWhenBucketsMatch(t *testing.T) {
	store := &fakeMediaStore{}
	rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{
		Status: CodeSuccess,
		Outputs: []*pb.Argument{MediaArgument(
			"res_video", "business", "outputs/video.mp4",
		)},
	}}
	client := newClientWithRPC(Config{
		TenantName: "agent", OutputBucket: "business",
		MediaStore: store,
	}, rpc)

	if _, err := client.FetchResult(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if store.copyCalls != 0 {
		t.Fatalf("copy calls = %d, want 0", store.copyCalls)
	}
}

func TestClientFetchResultSkipsCopyWhileProcessing(t *testing.T) {
	store := &fakeMediaStore{}
	rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{
		Status: CodeProcessing,
		Outputs: []*pb.Argument{MediaArgument(
			"res_video", WorkBucket, "outputs/video.mp4",
		)},
	}}
	client := newClientWithRPC(Config{
		TenantName: "agent", OutputBucket: "business",
		MediaStore: store,
	}, rpc)

	if _, err := client.FetchResult(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if store.copyCalls != 0 {
		t.Fatalf("copy calls = %d, want 0", store.copyCalls)
	}
}

func TestClientFetchResultReturnsCopyError(t *testing.T) {
	store := &fakeMediaStore{copyErr: errors.New("copy failed")}
	rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{
		Status: CodeSuccess,
		Outputs: []*pb.Argument{MediaArgument(
			"res_video", WorkBucket, "outputs/video.mp4",
		)},
	}}
	client := newClientWithRPC(Config{
		TenantName: "agent", OutputBucket: "business",
		MediaStore: store,
	}, rpc)

	_, err := client.FetchResult(context.Background(), 42)
	if err == nil || !strings.Contains(err.Error(), "copy failed") {
		t.Fatalf("FetchResult() error = %v, want copy error", err)
	}
}

func TestClientFetchResultRequiresMediaStoreForCopy(t *testing.T) {
	rpc := &fakeWorkflowRPC{queryResponse: &pb.QueryResponse{
		Status: CodeSuccess,
		Outputs: []*pb.Argument{MediaArgument(
			"res_video", WorkBucket, "outputs/video.mp4",
		)},
	}}
	client := newClientWithRPC(Config{
		TenantName: "agent", OutputBucket: "business",
	}, rpc)

	_, err := client.FetchResult(context.Background(), 42)
	if err == nil || !strings.Contains(err.Error(), "no media store is configured") {
		t.Fatalf("FetchResult() error = %v, want missing media store error", err)
	}
}
