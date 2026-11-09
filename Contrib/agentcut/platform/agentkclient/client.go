// Package agentkclient 是 cut-apiserver 调用 agent-k 的领域适配层。底层直接使用
// Agent-K 0.3 官方客户端：缺省通过 gRPC + KESS 发现 klingai-agentk-api，只有显式
// 配置 AGENTK_BASE_URL 时才改用 HTTP endpoint 覆盖（本地联调/测试）。
//
// 官方客户端会把 KWS_SERVICE_NAME / KWS_SERVICE_CREDENTIAL 的快照附加到 unary
// 和 stream 请求。Agent-K 0.3 不再接受 Bearer API Key。
package agentkclient

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	sdkclient "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
	agentkgrpc "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client/grpc"
	agentkhttp "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client/http"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
)

// Client 是 agent-k Connect API 客户端。
type Client struct {
	rpc *sdkclient.Client
}

type Identity struct {
	AppID       string
	DisplayName string
}

// EventStream 是 Agent-K SDK 的 WatchEvents 服务端流。
type EventStream = sdkclient.EventStream

const agentKDisabledEnv = "AGENTCUT_AGENTK_DISABLED"

type sdkFactory func(sdkclient.Options) (*sdkclient.Client, error)

// NewFromEnv 缺省使用 Agent-K SDK 的 gRPC/KESS 服务发现。AGENTK_BASE_URL 只用于
// 本地或测试 endpoint 覆盖；仅 AGENTCUT_AGENTK_DISABLED=true 会进入 manual-only。
func NewFromEnv() (*Client, error) {
	return newFromEnv(os.Getenv, agentkgrpc.New, agentkhttp.New)
}

func newFromEnv(getenv func(string) string, defaultFactory, endpointFactory sdkFactory) (*Client, error) {
	legacyAPIKey := strings.TrimSpace(getenv("AGENTK_API_KEY"))
	if legacyAPIKey != "" {
		log.Printf("WARN: AGENTK_API_KEY is deprecated and ignored; Agent-K 0.3 uses KWS_SERVICE_NAME/KWS_SERVICE_CREDENTIAL")
	}
	disabledValue := strings.TrimSpace(getenv(agentKDisabledEnv))
	if disabledValue != "" {
		disabled, err := strconv.ParseBool(disabledValue)
		if err != nil {
			return nil, fmt.Errorf("%s must be a boolean: %w", agentKDisabledEnv, err)
		}
		if disabled {
			return nil, nil
		}
	}

	endpoint := strings.TrimSpace(getenv("AGENTK_BASE_URL"))
	factory := defaultFactory
	if endpoint != "" {
		factory = endpointFactory
	}
	rpc, err := factory(sdkclient.Options{Endpoint: endpoint})
	if err != nil {
		return nil, fmt.Errorf("create Agent-K SDK client: %w", err)
	}
	if rpc == nil {
		return nil, fmt.Errorf("create Agent-K SDK client: factory returned nil")
	}
	return &Client{rpc: rpc}, nil
}

// New 构造显式 HTTP endpoint 客户端。legacyAPIKey 仅为源码兼容保留，绝不会发送。
func New(baseURL, legacyAPIKey string) (*Client, error) {
	if strings.TrimSpace(legacyAPIKey) != "" {
		log.Printf("WARN: Agent-K API key argument is deprecated and ignored; configure KESS identity instead")
	}
	rpc, err := agentkhttp.New(sdkclient.Options{Endpoint: strings.TrimSpace(baseURL)})
	if err != nil {
		return nil, fmt.Errorf("create Agent-K SDK HTTP client: %w", err)
	}
	if rpc == nil {
		return nil, fmt.Errorf("create Agent-K SDK HTTP client: factory returned nil")
	}
	return &Client{rpc: rpc}, nil
}

// ValidateAuth performs a read-only WhoAmI RPC so configured services fail at
// startup instead of discovering an invalid KESS identity on the first request.
func (c *Client) ValidateAuth(ctx context.Context) (Identity, error) {
	if c == nil {
		return Identity{}, fmt.Errorf("Agent-K client is not configured")
	}
	resp, err := c.rpc.WhoAmI(ctx, &apiv1.WhoAmIRequest{})
	if err != nil {
		return Identity{}, fmt.Errorf("agent-k whoami: %w", err)
	}
	identity := Identity{
		AppID:       strings.TrimSpace(resp.GetAppId()),
		DisplayName: strings.TrimSpace(resp.GetDisplayName()),
	}
	if identity.AppID == "" {
		return Identity{}, fmt.Errorf("agent-k whoami: authenticated identity has no app id")
	}
	return identity, nil
}

// CreateSession 创建一个绑定给项目的 agent-k Session，返回 ses_ 开头的 ID。
func (c *Client) CreateSession(ctx context.Context, title string, profile *apiv1.Profile) (string, error) {
	resp, err := c.rpc.CreateSession(ctx, &apiv1.CreateSessionRequest{
		Title:   title,
		Profile: profile,
	})
	if err != nil {
		return "", fmt.Errorf("agent-k create session: %w", err)
	}
	id := resp.GetSession().GetId()
	if !strings.HasPrefix(id, "ses_") {
		return "", fmt.Errorf("agent-k create session: missing valid ses_ id")
	}
	return id, nil
}

// StartMessage 以用户文本消息启动一次 Agent Execution。
func (c *Client) StartMessage(ctx context.Context, sessionID, text string) (*apiv1.Execution, error) {
	return c.startExecution(ctx, &apiv1.StartExecutionRequest{
		SessionId: sessionID,
		Input: &apiv1.StartExecutionRequest_Message{
			Message: &apiv1.AgentMessageExecutionInput{Text: text},
		},
	})
}

// StartAnswers 以 ask_user 答复启动一次 Agent Execution。
func (c *Client) StartAnswers(ctx context.Context, sessionID, toolCallID string, answers []*apiv1.AnswerInput) (*apiv1.Execution, error) {
	return c.startExecution(ctx, &apiv1.StartExecutionRequest{
		SessionId: sessionID,
		Input: &apiv1.StartExecutionRequest_Answers{
			Answers: &apiv1.AgentAnswersExecutionInput{
				ToolCallId: toolCallID,
				Answers:    answers,
			},
		},
	})
}

func (c *Client) startExecution(ctx context.Context, req *apiv1.StartExecutionRequest) (*apiv1.Execution, error) {
	resp, err := c.rpc.StartExecution(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.GetExecution(), nil
}

// ListEvents 按新到旧分页拉取会话持久事件。
func (c *Client) ListEvents(ctx context.Context, sessionID string, pageSize int32, pageToken string) (*apiv1.ListEventsResponse, error) {
	resp, err := c.rpc.ListEvents(ctx, &apiv1.ListEventsRequest{
		SessionId: sessionID,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// LatestExecution 返回会话最新一次 Execution（没有则返回 nil）。
func (c *Client) LatestExecution(ctx context.Context, sessionID string) (*apiv1.Execution, error) {
	resp, err := c.rpc.ListExecutions(ctx, &apiv1.ListExecutionsRequest{
		SessionId: sessionID,
		PageSize:  1,
	})
	if err != nil {
		return nil, err
	}
	executions := resp.GetExecutions()
	if len(executions) == 0 {
		return nil, nil
	}
	return executions[0], nil
}

// WatchEvents 订阅一次 Execution 的实时事件流（提交事件 / delta / stream_end）。
func (c *Client) WatchEvents(ctx context.Context, sessionID, executionID string) (*EventStream, error) {
	stream, err := c.rpc.WatchEvents(ctx, &apiv1.WatchEventsRequest{
		SessionId:   sessionID,
		ExecutionId: executionID,
	})
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// CancelExecution 取消 Session 当前正在运行的 Execution。agent-k 当前没有
// DeleteSession RPC；空闲 Session 会返回 FailedPrecondition，调用方可将其
// 视为“没有活动执行需要取消”。
func (c *Client) CancelExecution(ctx context.Context, sessionID string) error {
	_, err := c.rpc.CancelExecution(ctx, &apiv1.CancelExecutionRequest{
		SessionId: sessionID,
	})
	if err != nil {
		return fmt.Errorf("agent-k cancel execution: %w", err)
	}
	return nil
}
