// cut-apiserver：AgentCut 的 HTTP 入口。
//
//   - 项目管理与 Timeline Ops 网关（转发到 Temporal 实体 Workflow）；
//   - 素材上传（服务端中转进 BlobStore）；
//   - 对话请求代理到 agent-k（messages/events/stream/answers）；
//   - 不运行 Agent Loop、不直接处理视频字节、不落任何本地状态。
//
// 环境变量：
//
//	API_HOST / API_PORT            监听地址（缺省 127.0.0.1:8080）
//	TEMPORAL_ADDRESS               本地开发直连 Temporal（生产走 KESS 自动发现）
//	AGENTK_BASE_URL               可选的 agent-k HTTP endpoint 覆盖（缺省走 SDK/KESS）
//	AGENTCUT_AGENTK_DISABLED      true 时显式禁用 Agent-K，仅手工剪辑
//	KWS_SERVICE_NAME / KWS_SERVICE_CREDENTIAL   Agent-K 0.3 KESS 身份
//	FALLBACK_TO_ENV_LANE_ID / KWS_LANE_ID       可选的请求泳道入口 fallback
//	AGENTCUT_BLOB_ENDPOINT / _BUCKET / _CDN_BASE   BlobStore（未配置则上传返回 503）
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/requestlane/ksingress"

	agentcutprofile "agentcut/agents/agentcut"
	e2eprofile "agentcut/agents/e2e"
	"agentcut/platform/agentkclient"
	"agentcut/platform/api"
	"agentcut/platform/blob"
	"agentcut/platform/temporalx"
	"agentcut/render"
	"agentcut/tools"
)

func main() {
	ctx := context.Background()
	renderSelection, err := render.SelectionFromEnv()
	if err != nil {
		log.Fatalf("render executor: %v", err)
	}

	temporalClient, err := temporalx.Dial(ctx)
	if err != nil {
		log.Fatalf("dial temporal: %v", err)
	}
	defer temporalClient.Close()

	blobStore, err := blob.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("blob store: %v", err)
	}
	if blobStore == nil {
		log.Printf("WARN: blob storage not configured; uploads will return 503")
	}

	agentK, err := agentkclient.NewFromEnv()
	if err != nil {
		log.Fatalf("agent-k client: %v", err)
	}
	if agentK == nil {
		log.Printf("WARN: AGENTCUT_AGENTK_DISABLED=true; starting in manual-only mode without Agent-K sessions")
	} else {
		authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		identity, _, authErr := preflightAgentK(authCtx, agentK)
		cancel()
		if authErr != nil {
			log.Fatalf("agent-k WhoAmI preflight: %v", authErr)
		}
		log.Printf("agent-k connection verified: appId=%q displayName=%q",
			identity.AppID, identity.DisplayName)
	}
	authConfig, err := api.AuthConfigFromEnv()
	if err != nil {
		log.Fatalf("authentication: %v", err)
	}
	if authConfig.Mode == api.AuthModeDev {
		log.Printf(
			"WARN: AGENTCUT_AUTH_MODE=dev; request user headers are not authenticated; defaultUser=%q",
			authConfig.DevUser,
		)
	} else {
		log.Printf("authentication mode trusted-header; gateway must strip client-supplied %s", authConfig.UserHeader)
	}
	taskQueue := strings.TrimSpace(os.Getenv("AGENTCUT_TASK_QUEUE"))
	if taskQueue == "" {
		taskQueue = tools.InternalTaskQueue
	}
	profiles, err := profileDefinitionsFromEnv()
	if err != nil {
		log.Fatalf("profiles: %v", err)
	}

	server := &api.Server{
		Temporal:        temporalClient,
		Blob:            blobStore,
		AgentK:          agentK,
		Profile:         agentcutprofile.ProfileJSON,
		Profiles:        profiles,
		TaskQueue:       taskQueue,
		Auth:            authConfig,
		RenderSelection: renderSelection,
	}
	handler, err := requestLaneHandler(server.Handler())
	if err != nil {
		log.Fatalf("request lane ingress: %v", err)
	}

	host := strings.TrimSpace(os.Getenv("API_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("API_PORT"))
	if port == "" {
		port = "8080"
	}
	addr := net.JoinHostPort(host, port)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("cut-apiserver listening on http://%s", addr)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("apiserver exited: %v", err)
	}
}

func profileDefinitionsFromEnv() ([]api.ProfileDefinition, error) {
	profiles := []api.ProfileDefinition{{
		ProfileID:   agentcutprofile.ProfileID,
		Name:        agentcutprofile.ProfileDisplayName,
		Description: agentcutprofile.ProfileDescription,
		ProfileJSON: agentcutprofile.ProfileJSON,
		IsDefault:   true,
	}}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENTCUT_E2E_PROFILE"))) {
	case "", "false":
		return profiles, nil
	case "true":
		return append(profiles, api.ProfileDefinition{
			ProfileID:   e2eprofile.ProfileID,
			Name:        e2eprofile.ProfileDisplayName,
			Description: e2eprofile.ProfileDescription,
			ProfileJSON: e2eprofile.ProfileJSON,
		}), nil
	default:
		return nil, fmt.Errorf("AGENTCUT_E2E_PROFILE must be true or false")
	}
}

func requestLaneHandler(next http.Handler) (http.Handler, error) {
	adapter, err := ksingress.New()
	if err != nil {
		return nil, err
	}
	return adapter.HTTPMiddleware(next), nil
}

type agentKAuthenticator interface {
	ValidateAuth(context.Context) (agentkclient.Identity, error)
}

// preflightAgentK deliberately accepts a nil client for the explicit
// AGENTCUT_AGENTK_DISABLED manual-only mode; every enabled client must pass WhoAmI.
func preflightAgentK(
	ctx context.Context,
	client agentKAuthenticator,
) (agentkclient.Identity, bool, error) {
	if client == nil {
		return agentkclient.Identity{}, false, nil
	}
	identity, err := client.ValidateAuth(ctx)
	if err != nil {
		return agentkclient.Identity{}, true, err
	}
	if strings.TrimSpace(identity.AppID) == "" {
		return agentkclient.Identity{}, true, fmt.Errorf("Agent-K returned an empty app id")
	}
	return identity, true, nil
}
