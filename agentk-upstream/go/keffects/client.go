// Package keffects provides the thin gRPC client surface AgentK consumers need
// to run published keffects workflows.
package keffects

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
	ksgrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc/grpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

const (
	DefaultServiceName = "ytech-keffects-service"
	WorkBucket         = "upload-ylab-stunt"

	CodeSuccess      int32 = 1
	CodeProcessing   int32 = 2
	CodeFailed       int32 = 3
	CodeRequestExist int32 = 5
)

type Config struct {
	ServiceName   string
	TenantID      int64
	TenantName    string
	SubmitTimeout time.Duration
	QueryTimeout  time.Duration
	OutputBucket  string
	MediaStore    MediaStore
	HTTPClient    *http.Client
}

type SubmitInput struct {
	RequestID    string
	WorkflowID   int64
	WorkflowName string
	Arguments    []*pb.Argument
}

// WorkflowClient is the testable workflow boundary used by consumers.
type WorkflowClient interface {
	Submit(context.Context, SubmitInput) (int64, error)
	FetchResult(context.Context, int64) (*pb.QueryResponse, error)
}

type workflowRPC interface {
	Submit(context.Context, *pb.SubmitRequest, ...grpc.CallOption) (*pb.SubmitResponse, error)
	QuerySubmitRequest(
		context.Context, *pb.QueryRequest, ...grpc.CallOption,
	) (*pb.SubmitRequest, error)
	QueryResult(
		context.Context, *pb.QueryRequest, ...grpc.CallOption,
	) (*pb.QueryResponse, error)
}

// Client lazily discovers keffects so constructing a consumer remains safe in
// unit tests and other environments without a running Kess agent.
type Client struct {
	config  Config
	once    sync.Once
	rpc     workflowRPC
	dialErr error
}

func NewClient(config Config) *Client {
	return &Client{config: normalizeConfig(config)}
}

func newClientWithRPC(config Config, rpc workflowRPC) *Client {
	return &Client{config: normalizeConfig(config), rpc: rpc}
}

func normalizeConfig(config Config) Config {
	config.ServiceName = strings.TrimSpace(config.ServiceName)
	config.TenantName = strings.TrimSpace(config.TenantName)
	if config.ServiceName == "" {
		config.ServiceName = DefaultServiceName
	}
	config.OutputBucket = strings.TrimSpace(config.OutputBucket)
	if config.SubmitTimeout <= 0 {
		config.SubmitTimeout = time.Minute
	}
	if config.QueryTimeout <= 0 {
		config.QueryTimeout = 10 * time.Second
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return config
}

func (c *Client) Submit(ctx context.Context, input SubmitInput) (int64, error) {
	requestID := strings.TrimSpace(input.RequestID)
	workflowName := strings.TrimSpace(input.WorkflowName)
	if requestID == "" {
		return 0, fmt.Errorf("keffects: request id is required")
	}
	if input.WorkflowID < 0 {
		return 0, fmt.Errorf("keffects: workflow id must not be negative")
	}
	if input.WorkflowID == 0 && workflowName == "" {
		return 0, fmt.Errorf("keffects: workflow id or workflow name is required")
	}
	rpc, err := c.rpcClient()
	if err != nil {
		return 0, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.config.SubmitTimeout)
	defer cancel()
	response, err := rpc.Submit(callCtx, &pb.SubmitRequest{
		RequestId:        requestID,
		TenantId:         c.config.TenantID,
		TenantName:       c.config.TenantName,
		WorkflowId:       input.WorkflowID,
		WorkflowName:     workflowName,
		Arguments:        input.Arguments,
		OutputBucketName: WorkBucket,
	})
	workflow := workflowLabel(input.WorkflowID, workflowName)
	if err != nil {
		return 0, fmt.Errorf("keffects: submit %s: %w", workflow, err)
	}
	if response == nil {
		return 0, fmt.Errorf("keffects: submit %s returned an empty response", workflow)
	}
	if response.Code != CodeSuccess && response.Code != CodeRequestExist {
		return 0, &ResponseError{Code: response.Code, Message: response.Message}
	}
	if response.WorkflowInstanceId <= 0 {
		return 0, fmt.Errorf("keffects: submit returned empty workflow instance id")
	}
	return response.WorkflowInstanceId, nil
}

func (c *Client) FetchResult(
	ctx context.Context, workflowInstanceID int64,
) (*pb.QueryResponse, error) {
	if workflowInstanceID <= 0 {
		return nil, fmt.Errorf("keffects: workflow instance id must be positive")
	}
	rpc, err := c.rpcClient()
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.config.QueryTimeout)
	defer cancel()
	response, err := rpc.QueryResult(callCtx, c.queryRequest(workflowInstanceID))
	if err != nil {
		return nil, fmt.Errorf("keffects: query workflow %d: %w", workflowInstanceID, err)
	}
	if response == nil {
		return nil, fmt.Errorf("keffects: query workflow %d returned an empty response", workflowInstanceID)
	}
	if response.Status == CodeSuccess {
		if err := c.copyOutputs(ctx, response.Outputs); err != nil {
			return nil, fmt.Errorf(
				"keffects: copy workflow %d outputs: %w", workflowInstanceID, err)
		}
	}
	return response, nil
}

func (c *Client) copyOutputs(ctx context.Context, outputs []*pb.Argument) error {
	if c.config.OutputBucket == "" {
		return nil
	}
	for _, output := range outputs {
		if output == nil {
			continue
		}
		if media := output.GetMediaValue(); media != nil {
			if err := c.copyOutputMedia(
				ctx, fmt.Sprintf("output %q", output.GetName()), media,
			); err != nil {
				return err
			}
		}
		if mediaValues := output.GetMediaValues(); mediaValues != nil {
			for index, media := range mediaValues.GetValue() {
				if err := c.copyOutputMedia(
					ctx, fmt.Sprintf("output %q[%d]", output.GetName(), index), media,
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Client) copyOutputMedia(
	ctx context.Context, label string, media *pb.Media,
) error {
	if media == nil || media.GetBucket() == "" || media.GetKey() == "" ||
		media.GetBucket() == c.config.OutputBucket {
		return nil
	}
	if c.config.MediaStore == nil {
		return fmt.Errorf(
			"%s requires copy from %q to %q, but no media store is configured",
			label, media.GetBucket(), c.config.OutputBucket)
	}
	if err := c.config.MediaStore.Copy(
		ctx,
		media.GetBucket(), media.GetKey(),
		c.config.OutputBucket, media.GetKey(),
	); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	media.Bucket = c.config.OutputBucket
	return nil
}

// QuerySubmitRequest returns the request persisted by keffects. Consumers use
// it for E2E diagnostics of the exact protobuf arguments received.
func (c *Client) QuerySubmitRequest(
	ctx context.Context, workflowInstanceID int64,
) (*pb.SubmitRequest, error) {
	if workflowInstanceID <= 0 {
		return nil, fmt.Errorf("keffects: workflow instance id must be positive")
	}
	rpc, err := c.rpcClient()
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.config.QueryTimeout)
	defer cancel()
	response, err := rpc.QuerySubmitRequest(callCtx, c.queryRequest(workflowInstanceID))
	if err != nil {
		return nil, fmt.Errorf(
			"keffects: query submitted request for workflow %d: %w", workflowInstanceID, err)
	}
	if response == nil {
		return nil, fmt.Errorf(
			"keffects: query submitted request for workflow %d returned an empty response",
			workflowInstanceID,
		)
	}
	return response, nil
}

func (c *Client) queryRequest(workflowInstanceID int64) *pb.QueryRequest {
	return &pb.QueryRequest{
		TenantId:           c.config.TenantID,
		TenantName:         c.config.TenantName,
		WorkflowInstanceId: workflowInstanceID,
	}
}

func (c *Client) rpcClient() (workflowRPC, error) {
	c.once.Do(func() {
		if c.config.TenantID < 0 {
			c.dialErr = fmt.Errorf("keffects: tenant id must not be negative")
			return
		}
		if c.config.TenantID == 0 && c.config.TenantName == "" {
			c.dialErr = fmt.Errorf("keffects: tenant id or tenant name is required")
			return
		}
		if c.rpc != nil {
			return
		}
		conn, err := ksgrpc.NewClient(c.config.ServiceName, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
		if err != nil {
			c.dialErr = fmt.Errorf(
				"keffects: discover service %s: %w", c.config.ServiceName, err)
			return
		}
		c.rpc = pb.NewWorkflowServiceClient(conn)
		if c.rpc == nil {
			c.dialErr = fmt.Errorf("keffects: workflow service client is unavailable")
		}
	})
	return c.rpc, c.dialErr
}

func workflowLabel(workflowID int64, workflowName string) string {
	if workflowName != "" {
		return workflowName
	}
	return strconv.FormatInt(workflowID, 10)
}

type ResponseError struct {
	Code    int32
	Message string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("keffects: request rejected: code=%d message=%q", e.Code, e.Message)
}

func ParseInstanceID(taskID string) (int64, error) {
	id, err := strconv.ParseInt(taskID, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("keffects: invalid workflow instance id %q", taskID)
	}
	return id, nil
}
