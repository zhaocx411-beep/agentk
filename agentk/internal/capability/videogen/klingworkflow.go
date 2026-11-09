package videogen

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/keffects"
	"go.temporal.io/sdk/activity"

	"agentk/internal/artifact"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

const (
	klingWorkflowTenantName = "agent"
	klingWorkflowName       = "kling_video"
	klingWorkflowOutputName = "res_video"
	klingWorkBucketEnv      = "AGENTK_KEFFECTS_WORK_BUCKET"
)

// KlingWorkflow is the shared keffects-backed implementation behind the three
// existing Kling tool contracts.
type KlingWorkflow struct {
	rpc keffects.MediaWorkflowClient
}

func NewKlingWorkflow(
	storage artifact.Storage,
	rpc keffects.MediaWorkflowClient,
) *KlingWorkflow {
	if rpc == nil {
		if store, ok := storage.(*artifact.BlobStorage); ok {
			mediaStore, workBucket, finalBucket := store.KeffectsMediaStore(
				os.Getenv(klingWorkBucketEnv))
			rpc = keffects.NewClient(keffects.Config{
				TenantName:  klingWorkflowTenantName,
				WorkBucket:  workBucket,
				FinalBucket: finalBucket,
				MediaStore:  mediaStore,
			})
		}
	}
	return &KlingWorkflow{rpc: rpc}
}

func (k *KlingWorkflow) T2V() KlingT2VClient   { return klingWorkflowT2V{k} }
func (k *KlingWorkflow) I2V() KlingI2VClient   { return klingWorkflowI2V{k} }
func (k *KlingWorkflow) Omni() KlingOmniClient { return klingWorkflowOmni{k} }
func (k *KlingWorkflow) Basic() Client         { return klingWorkflowBasic{k} }

type klingWorkflowT2V struct{ *KlingWorkflow }
type klingWorkflowI2V struct{ *KlingWorkflow }
type klingWorkflowOmni struct{ *KlingWorkflow }
type klingWorkflowBasic struct{ *KlingWorkflow }

func (k klingWorkflowT2V) Submit(ctx context.Context, req KlingT2VRequest) (string, error) {
	if err := validateKlingWorkflowResolution(req.Resolution); err != nil {
		return "", err
	}
	duration, _ := strconv.ParseFloat(req.Duration, 64)
	return k.submit(ctx, []*pb.Argument{
		keffects.StringArgument("prompt", req.Prompt),
		keffects.FloatArgument("duration", duration),
		keffects.StringArgument("resolution", req.Resolution),
		keffects.StringArgument("aspect_ratio", req.AspectRatio),
		keffects.BoolArgument("enable_audio", req.Audio == "native"),
		keffects.BoolArgument("enable_multishot", req.MultiShot != nil && *req.MultiShot),
	})
}

func (k klingWorkflowI2V) Submit(ctx context.Context, req KlingI2VRequest) (string, error) {
	if err := validateKlingWorkflowResolution(req.Resolution); err != nil {
		return "", err
	}
	if k.rpc == nil {
		return "", klingWorkflowStorageError()
	}
	first, err := k.rpc.StageMedia(ctx, req.FirstFrame, keffects.StageMediaOptions{
		MaxBytes:  klingMaxImageBytes,
		KeyPrefix: "capabilities/videos/kling/inputs",
	})
	if err != nil {
		return "", err
	}
	duration, _ := strconv.ParseFloat(req.Duration, 64)
	args := []*pb.Argument{
		keffects.StringArgument("prompt", req.Prompt),
		keffects.FloatArgument("duration", duration),
		keffects.StringArgument("resolution", req.Resolution),
		keffects.StringArgument("aspect_ratio", "auto"),
		keffects.MediaArgument("first_frame", first.Bucket, first.Key),
		keffects.BoolArgument("enable_audio", req.Audio == "native"),
		keffects.BoolArgument("enable_multishot", req.MultiShot != nil && *req.MultiShot),
	}
	if req.LastFrame != "" {
		last, err := k.rpc.StageMedia(ctx, req.LastFrame, keffects.StageMediaOptions{
			MaxBytes:  klingMaxImageBytes,
			KeyPrefix: "capabilities/videos/kling/inputs",
		})
		if err != nil {
			return "", err
		}
		args = append(args, keffects.MediaArgument("tail_image", last.Bucket, last.Key))
	}
	return k.submit(ctx, args)
}

func (k klingWorkflowOmni) Submit(ctx context.Context, req KlingOmniRequest) (string, error) {
	if err := validateKlingWorkflowResolution(req.Resolution); err != nil {
		return "", err
	}
	if k.rpc == nil {
		return "", klingWorkflowStorageError()
	}
	if req.BaseVideo != "" {
		return "", temporalx.InvalidActivityInput(
			"kling_video workflow does not currently support baseVideo editing")
	}
	if len(req.ReferImages) == 0 && req.FeatureVideo == "" {
		return "", temporalx.InvalidActivityInput(
			"kling_omni_video requires referImages or featureVideo; " +
				"use kling_text_to_video for prompt-only generation or " +
				"kling_image_to_video for first/last-frame generation")
	}
	duration, _ := strconv.ParseFloat(req.Duration, 64)
	args := []*pb.Argument{
		keffects.StringArgument("prompt", req.Prompt),
		keffects.FloatArgument("duration", duration),
		keffects.StringArgument("resolution", req.Resolution),
		keffects.BoolArgument("enable_audio", req.Audio == "native"),
		keffects.BoolArgument("enable_multishot", req.MultiShot != nil && *req.MultiShot),
	}
	if req.AspectRatio != "" {
		args = append(args, keffects.StringArgument("aspect_ratio", req.AspectRatio))
	} else if req.FirstFrame != "" || req.FeatureVideo != "" {
		args = append(args, keffects.StringArgument("aspect_ratio", "auto"))
	}
	if req.FirstFrame != "" {
		first, err := k.rpc.StageMedia(ctx, req.FirstFrame, keffects.StageMediaOptions{
			MaxBytes:  klingMaxImageBytes,
			KeyPrefix: "capabilities/videos/kling/inputs",
		})
		if err != nil {
			return "", err
		}
		args = append(args, keffects.MediaArgument("first_frame", first.Bucket, first.Key))
	}
	if req.LastFrame != "" {
		last, err := k.rpc.StageMedia(ctx, req.LastFrame, keffects.StageMediaOptions{
			MaxBytes:  klingMaxImageBytes,
			KeyPrefix: "capabilities/videos/kling/inputs",
		})
		if err != nil {
			return "", err
		}
		args = append(args, keffects.MediaArgument("tail_image", last.Bucket, last.Key))
	}
	if len(req.ReferImages) > 0 {
		media := make([]*pb.Media, 0, len(req.ReferImages))
		for _, uri := range req.ReferImages {
			reference, err := k.rpc.StageMedia(ctx, uri, keffects.StageMediaOptions{
				MaxBytes:  klingMaxImageBytes,
				KeyPrefix: "capabilities/videos/kling/inputs",
			})
			if err != nil {
				return "", err
			}
			media = append(media, reference)
		}
		args = append(args, keffects.MediaValuesArgument("ref_images", media))
	}
	if req.FeatureVideo != "" {
		video, err := k.rpc.StageMedia(ctx, req.FeatureVideo, keffects.StageMediaOptions{
			MaxBytes:  klingMaxVideoBytes,
			KeyPrefix: "capabilities/videos/kling/inputs",
		})
		if err != nil {
			return "", err
		}
		args = append(args,
			keffects.MediaValuesArgument("ref_videos", []*pb.Media{video}),
			keffects.StringArgument("model_name", klingOmniModel(req.Resolution, true)),
		)
	}
	return k.submit(ctx, args)
}

func (k klingWorkflowBasic) Submit(ctx context.Context, req Request) (string, error) {
	multiShot := true
	return klingWorkflowI2V(k).Submit(ctx, KlingI2VRequest{
		FirstFrame: req.FirstFrame, LastFrame: req.LastFrame, Prompt: req.Prompt,
		Resolution: req.Resolution, Duration: req.Duration, Audio: req.Audio,
		MultiShot: &multiShot,
	})
}

func (k klingWorkflowT2V) Poll(ctx context.Context, taskID string) (tool.PollResult, error) {
	return k.poll(ctx, taskID)
}

func (k klingWorkflowI2V) Poll(ctx context.Context, taskID string) (tool.PollResult, error) {
	return k.poll(ctx, taskID)
}

func (k klingWorkflowOmni) Poll(ctx context.Context, taskID string) (tool.PollResult, error) {
	return k.poll(ctx, taskID)
}

func (k klingWorkflowBasic) Poll(ctx context.Context, taskID string) (tool.PollResult, error) {
	return k.poll(ctx, taskID)
}

func (k *KlingWorkflow) submit(ctx context.Context, args []*pb.Argument) (string, error) {
	if k.rpc == nil {
		return "", klingWorkflowStorageError()
	}
	instanceID, err := k.rpc.Submit(ctx, keffects.SubmitInput{
		RequestID: requestID(ctx), WorkflowName: klingWorkflowName,
		Arguments: args,
	})
	if err != nil {
		if _, ok := err.(*keffects.ResponseError); ok {
			return "", temporalx.InvalidActivityInput("%v", err)
		}
		return "", err
	}
	return strconv.FormatInt(instanceID, 10), nil
}

func (k *KlingWorkflow) poll(ctx context.Context, taskID string) (tool.PollResult, error) {
	instanceID, err := keffects.ParseInstanceID(taskID)
	if err != nil {
		return tool.PollResult{}, temporalx.InvalidActivityInput("%v", err)
	}
	response, err := k.rpc.QueryResult(ctx, instanceID)
	if err != nil {
		return tool.PollResult{}, err
	}
	switch response.Status {
	case keffects.CodeProcessing:
		return tool.PollResult{StatusDetail: response.Message}, nil
	case keffects.CodeFailed:
		reason := response.Message
		if response.ErrorDetail != nil {
			reason = strings.TrimSpace(response.ErrorDetail.Reason + " " + response.ErrorDetail.Message)
		}
		return tool.PollResult{Failed: true, FailureReason: reason}, nil
	case keffects.CodeSuccess:
		media, err := keffects.FindMedia(response.Outputs, klingWorkflowOutputName)
		if err != nil {
			// Keffects can expose SUCCESS before outputs become visible. Treat
			// that short window as processing and query again.
			return tool.PollResult{StatusDetail: "waiting for workflow outputs"}, nil
		}
		uri, err := k.rpc.OutputURI(media)
		if err != nil {
			return tool.PollResult{}, err
		}
		result, err := json.Marshal(map[string]any{"artifacts": []tool.ResultArtifact{{
			Type: "video", URI: uri,
		}}})
		if err != nil {
			return tool.PollResult{}, err
		}
		return tool.PollResult{Done: true, Result: result}, nil
	default:
		return tool.PollResult{Failed: true, FailureReason: fmt.Sprintf(
			"unexpected workflow status %d: %s", response.Status, response.Message)}, nil
	}
}

func klingWorkflowStorageError() error {
	return temporalx.ConfigurationError(
		fmt.Errorf("kling_video requires AGENTK_ARTIFACT_STORE=blob"))
}

func klingOmniModel(resolution string, hasReferenceVideo bool) string {
	version := "Kling-v3_0"
	if hasReferenceVideo {
		version = "Kling-v3_0_1"
	}
	if resolution == "1080p" || resolution == "pro" {
		return version + "-aio-omni-1080p-prod"
	}
	return version + "-aio-omni-720p-prod"
}

func validateKlingWorkflowResolution(resolution string) error {
	if resolution != "720p" && resolution != "1080p" {
		return temporalx.InvalidActivityInput(
			"kling_video resolution must be 720p or 1080p, got %q", resolution)
	}
	return nil
}

func requestID(ctx context.Context) string {
	if activity.IsActivity(ctx) {
		info := activity.GetInfo(ctx)
		return fmt.Sprintf("agentk:%s:%s", info.WorkflowExecution.ID, info.ActivityID)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return fmt.Sprintf("agentk:e2e:%x", sum[:12])
}
