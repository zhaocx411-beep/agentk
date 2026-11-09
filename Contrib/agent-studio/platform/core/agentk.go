// AgentK SDK 客户端，各 Agent 服务共用：创建 Session（固化 Profile）、
// 启动 Execution、翻账本事件；漫剧 pipeline 模式还会轮询 Execution 快照
// （status / output / error）。
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	agentkclient "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
	agentkgrpc "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client/grpc"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LedgerEvent 是 Studio 内部稳定的事件信封。AgentK 的 protobuf Event
// 用 oneof payload 作为事件类型；Connect 边界会把它投影回 kind + data，避免
// UI、会话折叠和 AgentK wire schema 耦合。ID 是 evt_ 前缀的 UUIDv7，按时间
// 有序且字典序一致，可直接作为折叠去重游标。
type LedgerEvent struct {
	ID          string          `json:"eventId"`
	ExecutionID string          `json:"executionId"`
	RoundID     string          `json:"roundId,omitempty"`
	Kind        string          `json:"kind"`
	Data        json.RawMessage `json:"data"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// AgentKDelta 是 WatchEvents 推送的模型未提交文本增量。Generation 在
// Activity 重试时变化；消费者应丢弃同一 round/block 下旧 generation 的预览。
type AgentKDelta struct {
	ExecutionID string `json:"executionId"`
	RoundID     string `json:"roundId"`
	Generation  string `json:"generation"`
	Index       int32  `json:"index"`
	Block       int32  `json:"block"`
	Kind        string `json:"kind"`
	Text        string `json:"text"`
}

// AgentKStreamFrame 是 AgentK WatchEvents 三种帧的稳定投影：已提交账本事件、
// 模型增量，以及 Execution 流结束标记。同一帧只会设置其中一个字段。
type AgentKStreamFrame struct {
	Event *LedgerEvent
	Delta *AgentKDelta
	End   bool
}

// AgentKStreamingClient 是 AgentKClient 的可选流式扩展。Converse 的 JSON
// 长轮询兼容任意 AgentKClient；真正的 SSE token 流要求实现此接口。
type AgentKStreamingClient interface {
	StartMessage(ctx context.Context, sessionID, text string) (string, error)
	WatchEvents(
		ctx context.Context,
		sessionID string,
		executionID string,
		onFrame func(AgentKStreamFrame) error,
	) error
}

type ChatAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// pipelineName 必须与 profile.json 中 pipelines 声明的 name 一致。
const pipelineName = "comic_drama_film"

// DramaArgs 是 pipeline 的启动参数，与 worker 侧 DramaInput 同形
// （跨语言契约：字段只加不改）。
type DramaArgs struct {
	Brief       string `json:"brief"`
	Title       string `json:"title,omitempty"`
	StylePrompt string `json:"stylePrompt,omitempty"`
	// Episodes 目标集数，0 取 worker 默认（1），上限 10。
	Episodes int `json:"episodes,omitempty"`
	// MaxShots 每集分镜数上限，0 取 worker 默认（10），上限 10。
	MaxShots    int    `json:"maxShots,omitempty"`
	ChatModel   string `json:"chatModel"`
	ImageModel  string `json:"imageModel"`
	EditModel   string `json:"editModel"`
	VideoModel  string `json:"videoModel"`
	AspectRatio string `json:"aspectRatio,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Audio       string `json:"audio,omitempty"`
}

// DramaManifest 是 pipeline 完成时的完整资产清单，与 worker 侧 DramaOutput 同形。
type DramaManifest struct {
	Title       string           `json:"title"`
	Synopsis    string           `json:"synopsis,omitempty"`
	Script      string           `json:"script"`
	StyleAnchor string           `json:"styleAnchor"`
	Characters  []CharacterAsset `json:"characters"`
	Voices      []VoiceAsset     `json:"voices,omitempty"`
	Episodes    []EpisodeAsset   `json:"episodes"`
	Film        string           `json:"film"`
	ClipCount   int              `json:"clipCount"`
}

type CharacterAsset struct {
	Name     string `json:"name"`
	Variant  string `json:"variant,omitempty"`
	Prompt   string `json:"prompt"`
	ImageURL string `json:"imageUrl,omitempty"`
	Error    string `json:"error,omitempty"`
}

// VoiceAsset 一个角色（或旁白）的全剧固定音色。
type VoiceAsset struct {
	Name   string `json:"name"`
	Gender string `json:"gender,omitempty"`
	Age    string `json:"age,omitempty"`
	Timbre string `json:"timbre"`
}

// EpisodeAsset 一集的全部产物；Error 非空表示该集失败（其余集不受影响）。
type EpisodeAsset struct {
	Number    int                 `json:"number"`
	Title     string              `json:"title,omitempty"`
	Script    string              `json:"script"`
	Assets    []EpisodeImageAsset `json:"assets,omitempty"`
	Shots     []ShotAsset         `json:"shots"`
	Film      string              `json:"film,omitempty"`
	ClipCount int                 `json:"clipCount"`
	Error     string              `json:"error,omitempty"`
}

// EpisodeImageAsset 一条分集图片资产（人物状态 / 场景 / 道具）。
type EpisodeImageAsset struct {
	Category     string `json:"category"`
	Name         string `json:"name"`
	CharacterRef string `json:"characterRef,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	ReuseFromEp  int    `json:"reuseFromEp,omitempty"`
	ImageURL     string `json:"imageUrl,omitempty"`
	Error        string `json:"error,omitempty"`
}

type ShotAsset struct {
	Index           int      `json:"index"`
	ShotID          string   `json:"shotId"`
	Characters      []string `json:"characters,omitempty"`
	Scenes          []string `json:"scenes,omitempty"`
	Props           []string `json:"props,omitempty"`
	Speakers        []string `json:"speakers,omitempty"`
	FirstFrame      string   `json:"firstFrame"`
	Storyboard      string   `json:"storyboard"`
	FramePrompt     string   `json:"framePrompt"`
	VideoPrompt     string   `json:"videoPrompt"`
	DurationSeconds int      `json:"durationSeconds"`
	FrameURL        string   `json:"frameUrl,omitempty"`
	VideoURL        string   `json:"videoUrl,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// PipelineSnapshot 是一次 pipeline run 的 exec 级快照。
type PipelineSnapshot struct {
	Status string
	Output *DramaManifest
	Error  string
}

type AgentKClient interface {
	CreateSession(
		ctx context.Context,
		title string,
		profile json.RawMessage,
	) (string, error)
	// StartPipeline 启动一次 comic_drama_film 执行，返回 exe_ 执行 ID
	//（pipeline 模式）。
	StartPipeline(ctx context.Context, sessionID string, args DramaArgs) (string, error)
	// GetPipelineRun 查询 exec 快照（running / completed / failed / cancelled）。
	GetPipelineRun(ctx context.Context, sessionID, execID string) (PipelineSnapshot, error)
	// PostMessage 向会话追加一条用户消息驱动 Agent（agent 模式）。
	PostMessage(ctx context.Context, sessionID, text string) error
	// AnswerQuestions 闭合 ask_user 调用并唤醒 Agent 继续执行。
	AnswerQuestions(
		ctx context.Context,
		sessionID string,
		toolCallID string,
		answers []ChatAnswer,
	) error
	// ListEvents 遍历 AgentK 的全部分页，返回按时间正序排列的完整事件日志；
	// 调用方按 LedgerEvent.ID 与已折叠游标比较去重。
	ListEvents(ctx context.Context, sessionID string) ([]LedgerEvent, error)
}

const defaultAgentKRequestTimeout = 30 * time.Second

// agentKEventStream 是 SDK EventStream 与本地 Connect 测试流的共同最小接口。
type agentKEventStream interface {
	Receive() bool
	Msg() *apiv1.WatchEventsResponse
	Err() error
	Close() error
}

// agentKRPC 把上层稳定投影与具体传输解耦。线上使用 SDK KESS gRPC；本地
// Fake AgentK 继续使用 Connect/JSON，避免测试环境依赖 kess-agent。
type agentKRPC interface {
	ListSessions(context.Context, *apiv1.ListSessionsRequest) (*apiv1.ListSessionsResponse, error)
	CreateSession(context.Context, *apiv1.CreateSessionRequest) (*apiv1.CreateSessionResponse, error)
	StartExecution(context.Context, *apiv1.StartExecutionRequest) (*apiv1.StartExecutionResponse, error)
	WatchEvents(context.Context, *apiv1.WatchEventsRequest) (agentKEventStream, error)
	GetExecution(context.Context, *apiv1.GetExecutionRequest) (*apiv1.GetExecutionResponse, error)
	ListEvents(context.Context, *apiv1.ListEventsRequest) (*apiv1.ListEventsResponse, error)
}

type SDKAgentKClient struct {
	rpc            agentKRPC
	requestTimeout time.Duration
}

// NewAgentKClient 默认经 KESS 自动发现 AgentK gRPC 服务。localBaseURL 只用于
// 本地 Fake AgentK / E2E；线上必须留空，不配置固定网关地址。
func NewAgentKClient(localBaseURL string) (*SDKAgentKClient, error) {
	if strings.TrimSpace(localBaseURL) != "" {
		return newLocalHTTPAgentKClient(localBaseURL, defaultAgentKRequestTimeout)
	}
	client, err := agentkgrpc.New(agentkclient.Options{})
	if err != nil {
		return nil, fmt.Errorf("create AgentK KESS gRPC client: %w", err)
	}
	return &SDKAgentKClient{
		rpc:            sdkAgentKRPC{client: client},
		requestTimeout: defaultAgentKRequestTimeout,
	}, nil
}

type sdkAgentKRPC struct {
	client *agentkclient.Client
}

func (c sdkAgentKRPC) ListSessions(
	ctx context.Context,
	request *apiv1.ListSessionsRequest,
) (*apiv1.ListSessionsResponse, error) {
	return c.client.ListSessions(ctx, request)
}

func (c sdkAgentKRPC) CreateSession(
	ctx context.Context,
	request *apiv1.CreateSessionRequest,
) (*apiv1.CreateSessionResponse, error) {
	return c.client.CreateSession(ctx, request)
}

func (c sdkAgentKRPC) StartExecution(
	ctx context.Context,
	request *apiv1.StartExecutionRequest,
) (*apiv1.StartExecutionResponse, error) {
	return c.client.StartExecution(ctx, request)
}

func (c sdkAgentKRPC) WatchEvents(
	ctx context.Context,
	request *apiv1.WatchEventsRequest,
) (agentKEventStream, error) {
	return c.client.WatchEvents(ctx, request)
}

func (c sdkAgentKRPC) GetExecution(
	ctx context.Context,
	request *apiv1.GetExecutionRequest,
) (*apiv1.GetExecutionResponse, error) {
	return c.client.GetExecution(ctx, request)
}

func (c sdkAgentKRPC) ListEvents(
	ctx context.Context,
	request *apiv1.ListEventsRequest,
) (*apiv1.ListEventsResponse, error) {
	return c.client.ListEvents(ctx, request)
}

type connectAgentKRPC struct {
	unary  apiv1connect.AgentKServiceClient
	stream apiv1connect.AgentKServiceClient
}

func newLocalHTTPAgentKClient(
	baseURL string,
	requestTimeout time.Duration,
) (*SDKAgentKClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("AGENTK_LOCAL_BASE_URL must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("AGENTK_LOCAL_BASE_URL must use http or https")
	}
	options := []connect.ClientOption{connect.WithProtoJSON()}
	return &SDKAgentKClient{
		rpc: connectAgentKRPC{
			unary: apiv1connect.NewAgentKServiceClient(
				&http.Client{Timeout: requestTimeout}, baseURL, options...,
			),
			stream: apiv1connect.NewAgentKServiceClient(
				&http.Client{}, baseURL, options...,
			),
		},
		requestTimeout: 0, // 本地 unary client 自己持有 timeout；流不受它影响。
	}, nil
}

func (c connectAgentKRPC) ListSessions(
	ctx context.Context,
	request *apiv1.ListSessionsRequest,
) (*apiv1.ListSessionsResponse, error) {
	response, err := c.unary.ListSessions(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func (c connectAgentKRPC) CreateSession(
	ctx context.Context,
	request *apiv1.CreateSessionRequest,
) (*apiv1.CreateSessionResponse, error) {
	response, err := c.unary.CreateSession(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func (c connectAgentKRPC) StartExecution(
	ctx context.Context,
	request *apiv1.StartExecutionRequest,
) (*apiv1.StartExecutionResponse, error) {
	response, err := c.unary.StartExecution(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func (c connectAgentKRPC) WatchEvents(
	ctx context.Context,
	request *apiv1.WatchEventsRequest,
) (agentKEventStream, error) {
	return c.stream.WatchEvents(ctx, connect.NewRequest(request))
}

func (c connectAgentKRPC) GetExecution(
	ctx context.Context,
	request *apiv1.GetExecutionRequest,
) (*apiv1.GetExecutionResponse, error) {
	response, err := c.unary.GetExecution(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func (c connectAgentKRPC) ListEvents(
	ctx context.Context,
	request *apiv1.ListEventsRequest,
) (*apiv1.ListEventsResponse, error) {
	response, err := c.unary.ListEvents(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func (c *SDKAgentKClient) unaryContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if c.requestTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}

func (c *SDKAgentKClient) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	var sessions []SessionSummary
	pageToken := ""
	seenTokens := make(map[string]struct{})
	for {
		request := &apiv1.ListSessionsRequest{
			PageSize: 100, PageToken: pageToken,
		}
		rpcCtx, cancel := c.unaryContext(ctx)
		response, err := c.rpc.ListSessions(rpcCtx, request)
		cancel()
		if err != nil {
			return nil, wrapAgentKRPCError("list sessions", err)
		}
		for _, session := range response.GetSessions() {
			if session == nil || !strings.HasPrefix(session.GetId(), "ses_") {
				return nil, fmt.Errorf("agent-k list sessions returned an invalid session")
			}
			sessions = append(sessions, SessionSummary{
				ID:        session.GetId(),
				Title:     session.GetTitle(),
				CreatedAt: protobufTime(session.GetCreatedAt()),
			})
		}
		next := response.GetNextPageToken()
		if next == "" {
			break
		}
		if next == pageToken {
			return nil, errors.New("agent-k list sessions returned a repeated page token")
		}
		if _, duplicate := seenTokens[next]; duplicate {
			return nil, errors.New("agent-k list sessions returned a cyclic page token")
		}
		seenTokens[next] = struct{}{}
		pageToken = next
	}
	return sessions, nil
}

func (c *SDKAgentKClient) CreateSession(
	ctx context.Context,
	title string,
	profile json.RawMessage,
) (string, error) {
	parsedProfile, err := DecodeAgentKProfile(profile)
	if err != nil {
		return "", err
	}
	request := &apiv1.CreateSessionRequest{
		Title: title, Profile: parsedProfile,
	}
	rpcCtx, cancel := c.unaryContext(ctx)
	defer cancel()
	response, err := c.rpc.CreateSession(rpcCtx, request)
	if err != nil {
		return "", wrapAgentKRPCError("create session", err)
	}
	sessionID := response.GetSession().GetId()
	if !strings.HasPrefix(sessionID, "ses_") {
		return "", fmt.Errorf("agent-k response is missing a valid ses_ id")
	}
	return sessionID, nil
}

// DecodeAgentKProfile 对本地内嵌或 PROFILE_FILE 提供的 protojson 做严格
// 解码。它会在启动/发请求前拦住 v0.1 的 ref 等未知字段，避免错误延迟到
// AgentK CreateSession 才暴露。
func DecodeAgentKProfile(profile json.RawMessage) (*apiv1.Profile, error) {
	parsed := &apiv1.Profile{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(
		profile, parsed,
	); err != nil {
		return nil, fmt.Errorf("decode AgentK profile: %w", err)
	}
	if strings.TrimSpace(parsed.GetDefaultModel()) == "" {
		return nil, errors.New("decode AgentK profile: defaultModel is required")
	}
	return parsed, nil
}

func (c *SDKAgentKClient) StartPipeline(
	ctx context.Context,
	sessionID string,
	args DramaArgs,
) (string, error) {
	pipelineArgs, err := protobufStruct(args)
	if err != nil {
		return "", fmt.Errorf("encode pipeline arguments: %w", err)
	}
	request := &apiv1.StartExecutionRequest{
		SessionId: sessionID,
		Input: &apiv1.StartExecutionRequest_Pipeline{
			Pipeline: &apiv1.PipelineExecutionInput{
				Pipeline: pipelineName,
				Args:     pipelineArgs,
			},
		},
	}
	rpcCtx, cancel := c.unaryContext(ctx)
	defer cancel()
	response, err := c.rpc.StartExecution(rpcCtx, request)
	if err != nil {
		return "", wrapAgentKRPCError("start pipeline execution", err)
	}
	execution := response.GetExecution()
	if execution == nil || execution.GetPipeline() == nil ||
		!strings.HasPrefix(execution.GetId(), "exe_") {
		return "", fmt.Errorf("agent-k response is missing a valid execution id")
	}
	return execution.GetId(), nil
}

func (c *SDKAgentKClient) PostMessage(
	ctx context.Context,
	sessionID string,
	text string,
) error {
	_, err := c.StartMessage(ctx, sessionID, text)
	return err
}

// StartMessage 启动一次 Agent 消息 Execution，并保留 WatchEvents 所需的
// execution ID。PostMessage 是不关心实时流的兼容包装。
func (c *SDKAgentKClient) StartMessage(
	ctx context.Context,
	sessionID string,
	text string,
) (string, error) {
	request := &apiv1.StartExecutionRequest{
		SessionId: sessionID,
		Input: &apiv1.StartExecutionRequest_Message{
			Message: &apiv1.AgentMessageExecutionInput{Text: text},
		},
	}
	rpcCtx, cancel := c.unaryContext(ctx)
	defer cancel()
	response, err := c.rpc.StartExecution(rpcCtx, request)
	if err != nil {
		return "", wrapAgentKRPCError("start message execution", err)
	}
	execution := response.GetExecution()
	if execution == nil ||
		execution.GetAgent() == nil || !strings.HasPrefix(execution.GetId(), "exe_") {
		return "", errors.New("agent-k response is missing a valid agent execution")
	}
	return execution.GetId(), nil
}

// WatchEvents 消费 AgentK 的 execution 级实时流。模型 delta 不入持久账本，
// committed event 则复用 ListEvents 的稳定投影，供上层使用同一折叠器收口。
func (c *SDKAgentKClient) WatchEvents(
	ctx context.Context,
	sessionID string,
	executionID string,
	onFrame func(AgentKStreamFrame) error,
) error {
	request := &apiv1.WatchEventsRequest{
		SessionId: sessionID, ExecutionId: executionID,
	}
	stream, err := c.rpc.WatchEvents(ctx, request)
	if err != nil {
		return wrapAgentKRPCError("watch events", err)
	}
	defer stream.Close()
	for stream.Receive() {
		message := stream.Msg()
		frame := AgentKStreamFrame{}
		switch {
		case message.GetCommittedEvent() != nil:
			projected, err := projectLedgerEvent(message.GetCommittedEvent())
			if err != nil {
				return err
			}
			frame.Event = &projected
		case message.GetDelta() != nil:
			delta := message.GetDelta()
			kind := ""
			switch delta.GetKind() {
			case apiv1.DeltaBlockKind_DELTA_BLOCK_KIND_TEXT:
				kind = "text"
			case apiv1.DeltaBlockKind_DELTA_BLOCK_KIND_REASONING:
				kind = "reasoning"
			default:
				continue
			}
			frame.Delta = &AgentKDelta{
				ExecutionID: delta.GetExecutionId(), RoundID: delta.GetRoundId(),
				Generation: delta.GetGeneration(), Index: delta.GetIndex(),
				Block: delta.GetBlock(), Kind: kind, Text: delta.GetText(),
			}
		case message.GetStreamEnd() != nil:
			frame.End = true
		default:
			continue
		}
		if onFrame != nil {
			if err := onFrame(frame); err != nil {
				return err
			}
		}
	}
	if err := stream.Err(); err != nil {
		return wrapAgentKRPCError("watch events", err)
	}
	return nil
}

func (c *SDKAgentKClient) AnswerQuestions(
	ctx context.Context,
	sessionID string,
	toolCallID string,
	answers []ChatAnswer,
) error {
	protobufAnswers := make([]*apiv1.AnswerInput, 0, len(answers))
	for _, answer := range answers {
		protobufAnswers = append(protobufAnswers, &apiv1.AnswerInput{
			QuestionId: answer.QuestionID,
			Selected:   append([]string(nil), answer.Selected...),
		})
	}
	request := &apiv1.StartExecutionRequest{
		SessionId: sessionID,
		Input: &apiv1.StartExecutionRequest_Answers{
			Answers: &apiv1.AgentAnswersExecutionInput{
				ToolCallId: toolCallID,
				Answers:    protobufAnswers,
			},
		},
	}
	rpcCtx, cancel := c.unaryContext(ctx)
	defer cancel()
	response, err := c.rpc.StartExecution(rpcCtx, request)
	if err != nil {
		return wrapAgentKRPCError("start answers execution", err)
	}
	if execution := response.GetExecution(); execution == nil ||
		execution.GetAgent() == nil || !strings.HasPrefix(execution.GetId(), "exe_") {
		return errors.New("agent-k response is missing a valid agent execution")
	}
	return nil
}

func (c *SDKAgentKClient) GetPipelineRun(
	ctx context.Context,
	sessionID string,
	execID string,
) (PipelineSnapshot, error) {
	request := &apiv1.GetExecutionRequest{
		SessionId: sessionID, ExecutionId: execID,
	}
	rpcCtx, cancel := c.unaryContext(ctx)
	defer cancel()
	response, err := c.rpc.GetExecution(rpcCtx, request)
	if err != nil {
		return PipelineSnapshot{}, wrapAgentKRPCError("get pipeline execution", err)
	}
	execution := response.GetExecution()
	if execution == nil || execution.GetId() != execID || execution.GetPipeline() == nil {
		return PipelineSnapshot{}, errors.New("agent-k returned an invalid pipeline execution")
	}
	status, err := executionStatus(execution.GetStatus())
	if err != nil {
		return PipelineSnapshot{}, err
	}
	var output *DramaManifest
	if value := execution.GetPipeline().GetOutput(); value != nil &&
		value.GetKind() != nil {
		raw, err := json.Marshal(value.AsInterface())
		if err != nil {
			return PipelineSnapshot{}, fmt.Errorf("encode pipeline output: %w", err)
		}
		if string(raw) != "null" {
			output = &DramaManifest{}
			if err := json.Unmarshal(raw, output); err != nil {
				return PipelineSnapshot{}, fmt.Errorf("decode pipeline output: %w", err)
			}
		}
	}
	return PipelineSnapshot{
		Status: status,
		Output: output,
		Error:  execution.GetError(),
	}, nil
}

func (c *SDKAgentKClient) ListEvents(
	ctx context.Context,
	sessionID string,
) ([]LedgerEvent, error) {
	// AgentK 每页 newest-first。先按页收齐，再整体反转，确保现有增量折叠
	// 总是 oldest-first；若直接折叠第一页，LastEventID 会把历史全部跳过。
	var newestFirst []*apiv1.Event
	pageToken := ""
	seenTokens := make(map[string]struct{})
	for {
		request := &apiv1.ListEventsRequest{
			SessionId: sessionID, PageSize: 100, PageToken: pageToken,
		}
		rpcCtx, cancel := c.unaryContext(ctx)
		response, err := c.rpc.ListEvents(rpcCtx, request)
		cancel()
		if err != nil {
			return nil, wrapAgentKRPCError("list events", err)
		}
		newestFirst = append(newestFirst, response.GetEvents()...)
		next := response.GetNextPageToken()
		if next == "" {
			break
		}
		if next == pageToken {
			return nil, errors.New("agent-k list events returned a repeated page token")
		}
		if _, duplicate := seenTokens[next]; duplicate {
			return nil, errors.New("agent-k list events returned a cyclic page token")
		}
		seenTokens[next] = struct{}{}
		pageToken = next
	}
	events := make([]LedgerEvent, 0, len(newestFirst))
	for index := len(newestFirst) - 1; index >= 0; index-- {
		event, err := projectLedgerEvent(newestFirst[index])
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func protobufStruct(value any) (*structpb.Struct, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]any)
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return structpb.NewStruct(fields)
}

func projectLedgerEvent(event *apiv1.Event) (LedgerEvent, error) {
	if event == nil || !strings.HasPrefix(event.GetEventId(), "evt_") {
		return LedgerEvent{}, errors.New("agent-k returned an invalid event")
	}
	kind, payload, err := ledgerPayload(event)
	if err != nil {
		return LedgerEvent{}, fmt.Errorf("project AgentK event %s: %w", event.GetEventId(), err)
	}
	raw, err := protojson.Marshal(payload)
	if err != nil {
		return LedgerEvent{}, fmt.Errorf("marshal payload: %w", err)
	}
	raw, err = normalizePayloadEnums(kind, payload, raw)
	if err != nil {
		return LedgerEvent{}, fmt.Errorf("normalize payload: %w", err)
	}
	return LedgerEvent{
		ID:          event.GetEventId(),
		ExecutionID: event.GetExecutionId(),
		RoundID:     event.GetRoundId(),
		Kind:        kind,
		Data:        raw,
		CreatedAt:   protobufTime(event.GetCreatedAt()),
	}, nil
}

func ledgerPayload(event *apiv1.Event) (string, proto.Message, error) {
	switch payload := event.GetPayload().(type) {
	case *apiv1.Event_UserInput:
		return "user.input", payload.UserInput, nil
	case *apiv1.Event_RoundStarted:
		return "round.started", payload.RoundStarted, nil
	case *apiv1.Event_RoundCompleted:
		return "round.completed", payload.RoundCompleted, nil
	case *apiv1.Event_AssistantText:
		return "assistant.text", payload.AssistantText, nil
	case *apiv1.Event_AssistantReasoning:
		return "assistant.reasoning", payload.AssistantReasoning, nil
	case *apiv1.Event_AssistantToolCall:
		return "assistant.tool_call", payload.AssistantToolCall, nil
	case *apiv1.Event_ToolExecStarted:
		return "tool.exec_started", payload.ToolExecStarted, nil
	case *apiv1.Event_ToolExecSucceeded:
		return "tool.exec_succeeded", payload.ToolExecSucceeded, nil
	case *apiv1.Event_ToolExecFailed:
		return "tool.exec_failed", payload.ToolExecFailed, nil
	case *apiv1.Event_AskUserRequested:
		return "ask_user.requested", payload.AskUserRequested, nil
	case *apiv1.Event_AskUserResponded:
		return "ask_user.responded", payload.AskUserResponded, nil
	case *apiv1.Event_ExecutionStarted:
		return "execution.started", payload.ExecutionStarted, nil
	case *apiv1.Event_ExecutionFinished:
		return "execution.finished", payload.ExecutionFinished, nil
	case *apiv1.Event_InvocationProgress:
		return "invocation.progress", payload.InvocationProgress, nil
	case nil:
		return "", nil, errors.New("event payload is missing")
	default:
		return "", nil, fmt.Errorf("unsupported event payload %T", payload)
	}
}

func normalizePayloadEnums(
	kind string,
	payload proto.Message,
	raw json.RawMessage,
) (json.RawMessage, error) {
	object := make(map[string]any)
	switch typed := payload.(type) {
	case *apiv1.ExecutionFinished:
		status, err := executionStatus(typed.GetStatus())
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		object["status"] = status
	case *apiv1.InvocationProgress:
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		switch typed.GetInvocationKind().String() {
		case "INVOCATION_KIND_PIPELINE":
			object["invocationKind"] = "pipeline"
		case "INVOCATION_KIND_TOOL":
			object["invocationKind"] = "tool"
		default:
			return nil, fmt.Errorf("unknown invocation kind %q", typed.GetInvocationKind())
		}
	default:
		return raw, nil
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	return normalized, nil
}

func executionStatus(status apiv1.ExecutionStatus) (string, error) {
	switch status {
	case apiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING:
		return "running", nil
	case apiv1.ExecutionStatus_EXECUTION_STATUS_COMPLETED:
		return "completed", nil
	case apiv1.ExecutionStatus_EXECUTION_STATUS_FAILED:
		return "failed", nil
	case apiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED:
		return "cancelled", nil
	default:
		return "", fmt.Errorf("agent-k returned unknown execution status %q", status)
	}
}

func protobufTime(timestamp *timestamppb.Timestamp) time.Time {
	if timestamp == nil || timestamp.CheckValid() != nil {
		return time.Time{}
	}
	return timestamp.AsTime()
}

func wrapAgentKRPCError(operation string, err error) error {
	var rpcError *connect.Error
	if !errors.As(err, &rpcError) {
		return fmt.Errorf("agent-k %s: %w", operation, err)
	}
	return &AgentKHTTPError{
		StatusCode: connectCodeHTTPStatus(rpcError.Code()),
		Message:    rpcError.Message(),
	}
}

func connectCodeHTTPStatus(code connect.Code) int {
	switch code {
	case connect.CodeCanceled:
		return http.StatusRequestTimeout
	case connect.CodeInvalidArgument, connect.CodeOutOfRange:
		return http.StatusBadRequest
	case connect.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case connect.CodeNotFound:
		return http.StatusNotFound
	case connect.CodeAlreadyExists, connect.CodeAborted, connect.CodeFailedPrecondition:
		return http.StatusConflict
	case connect.CodePermissionDenied:
		return http.StatusForbidden
	case connect.CodeResourceExhausted:
		return http.StatusTooManyRequests
	case connect.CodeUnimplemented:
		return http.StatusNotImplemented
	case connect.CodeUnavailable:
		return http.StatusServiceUnavailable
	case connect.CodeUnauthenticated:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

type AgentKHTTPError struct {
	StatusCode int
	Message    string
}

func (e *AgentKHTTPError) Error() string {
	return fmt.Sprintf("agent-k returned HTTP %d: %s", e.StatusCode, e.Message)
}
