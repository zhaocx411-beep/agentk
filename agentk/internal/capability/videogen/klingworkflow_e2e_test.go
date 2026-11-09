//go:build e2e

package videogen

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/keffects"

	"agentk/internal/artifact"
	"agentk/internal/tool"
)

// TestKlingWorkflowT2VEndToEnd submits a real text-to-video job to the staging
// mainline keffects service. The host kess agent selects staging; KWS_LANE_ID
// must be empty. Required environment: AGENTK_BLOB_ENDPOINT and
// AGENTK_BLOB_BUCKET. AGENTK_KEFFECTS_WORK_BUCKET is optional and defaults to
// AGENTK_BLOB_BUCKET; AGENTK_BLOB_CDN_BASE is optional.
//
// Run:
//
//	env -u KWS_LANE_ID go test -tags=e2e \
//	  ./internal/capability/videogen -run TestKlingWorkflowT2VEndToEnd \
//	  -v -count=1 -timeout=25h
func TestKlingWorkflowT2VEndToEnd(t *testing.T) {
	if os.Getenv("KWS_LANE_ID") != "" {
		t.Fatal("KWS_LANE_ID must be empty for staging mainline E2E")
	}
	storage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	client := NewKlingWorkflow(storage, nil).T2V()
	multiShot := false
	request, err := (KlingT2VRequest{
		Prompt:      "夏日海边，海浪轻轻拍打礁石，夕阳缓缓落下，固定镜头，写实电影质感",
		Resolution:  "720p",
		Duration:    "5",
		AspectRatio: "16:9",
		Audio:       "off",
		MultiShot:   &multiShot,
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	taskID, err := client.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("submitted keffects workflow instance %s", taskID)
	awaitKlingE2E(t, ctx, storage, client, taskID)
}

// TestKlingWorkflowI2VEndToEnd uploads KLING_I2V_FIRST_FRAME_PATH to the
// configured agent-k BlobStore, then generates a video from that first frame.
func TestKlingWorkflowI2VEndToEnd(t *testing.T) {
	if os.Getenv("KWS_LANE_ID") != "" {
		t.Fatal("KWS_LANE_ID must be empty for staging mainline E2E")
	}
	imagePath := os.Getenv("KLING_I2V_FIRST_FRAME_PATH")
	if imagePath == "" {
		t.Fatal("KLING_I2V_FIRST_FRAME_PATH is required")
	}
	image, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	frame, err := storage.Put(context.Background(),
		fmt.Sprintf("capabilities/videos/kling/e2e/%x.png", sum[:16]), image, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	client := NewKlingWorkflow(storage, nil).I2V()
	multiShot := false
	request, err := (KlingI2VRequest{
		FirstFrame: frame.URL,
		Prompt:     "雨中的猫剑客缓缓抬起头，目光坚定，斗笠上的雨水自然滴落，镜头轻微推近",
		Resolution: "720p",
		Duration:   "5",
		Audio:      "off",
		MultiShot:  &multiShot,
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	taskID, err := client.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded first frame %s; submitted keffects workflow instance %s", frame.URL, taskID)
	awaitKlingE2E(t, ctx, storage, client, taskID)
}

// TestKlingWorkflowOmniEndToEnd uploads exactly three comma-separated
// KLING_OMNI_REFERENCE_PATHS and generates three corresponding shots.
func TestKlingWorkflowOmniEndToEnd(t *testing.T) {
	if os.Getenv("KWS_LANE_ID") != "" {
		t.Fatal("KWS_LANE_ID must be empty for staging mainline E2E")
	}
	paths := strings.Split(os.Getenv("KLING_OMNI_REFERENCE_PATHS"), ",")
	if len(paths) != 3 {
		t.Fatal("KLING_OMNI_REFERENCE_PATHS must contain exactly three comma-separated paths")
	}
	storage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	references := make([]string, 0, len(paths))
	for _, imagePath := range paths {
		image, err := os.ReadFile(strings.TrimSpace(imagePath))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(image)
		reference, err := storage.Put(context.Background(),
			fmt.Sprintf("capabilities/videos/kling/e2e/%x.png", sum[:16]), image, "image/png")
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference.URL)
	}
	client := NewKlingWorkflow(storage, nil).Omni()
	multiShot := true
	request, err := (KlingOmniRequest{
		Prompt: "镜头 1, 3, @image_1 中佩戴珍珠首饰的女子缓缓转头看向镜头，珠宝微微闪光，近景；" +
			"镜头 2, 3, @image_2 中摄影棚里的女子翻开桌上的剧本，身后的黑衣男子轻扶眼镜，中景；" +
			"镜头 3, 3, @image_3 中女孩与猫和熊猫一起举起爪子开心挥动，阳光温暖，广角",
		ReferImages: references,
		Resolution:  "720p",
		Duration:    "9",
		AspectRatio: "9:16",
		Audio:       "off",
		MultiShot:   &multiShot,
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	taskID, err := client.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded references %v; submitted keffects workflow instance %s", references, taskID)
	awaitKlingE2E(t, ctx, storage, client, taskID)
}

// TestKlingWorkflowOmniFeatureVideoEndToEnd uploads KLING_OMNI_FEATURE_VIDEO_PATH
// plus 1–4 comma-separated KLING_OMNI_REFERENCE_PATHS, then generates a video
// that uses the clip as feature reference and the stills as element references.
//
// Run:
//
//	env -u KWS_LANE_ID go test -tags=e2e \
//	  ./internal/capability/videogen -run TestKlingWorkflowOmniFeatureVideoEndToEnd \
//	  -v -count=1 -timeout=25h
func TestKlingWorkflowOmniFeatureVideoEndToEnd(t *testing.T) {
	if os.Getenv("KWS_LANE_ID") != "" {
		t.Fatal("KWS_LANE_ID must be empty for staging mainline E2E")
	}
	videoPath := os.Getenv("KLING_OMNI_FEATURE_VIDEO_PATH")
	if videoPath == "" {
		t.Fatal("KLING_OMNI_FEATURE_VIDEO_PATH is required")
	}
	paths := strings.Split(os.Getenv("KLING_OMNI_REFERENCE_PATHS"), ",")
	if n := len(paths); n < 1 || n > 4 {
		t.Fatal("KLING_OMNI_REFERENCE_PATHS must contain 1–4 comma-separated paths")
	}
	storage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	references := make([]string, 0, len(paths))
	for _, imagePath := range paths {
		references = append(references, uploadE2EBlob(t, storage, strings.TrimSpace(imagePath), "image/png"))
	}
	featureVideo := uploadE2EBlob(t, storage, videoPath, "video/mp4")
	prompt := "借鉴 @video_1 的运镜、节奏与动作"
	for i := range references {
		prompt += fmt.Sprintf("，@image_%d 作为人物与场景参考", i+1)
	}
	prompt += "。电影质感，人物自然微动。"
	client := NewKlingWorkflow(storage, nil).Omni()
	multiShot := false
	request, err := (KlingOmniRequest{
		Prompt:       prompt,
		ReferImages:  references,
		FeatureVideo: featureVideo,
		Resolution:   "720p",
		Duration:     "5",
		AspectRatio:  "9:16",
		Audio:        "off",
		MultiShot:    &multiShot,
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	taskID, err := client.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded feature video %s and references %v; submitted keffects workflow instance %s",
		featureVideo, references, taskID)
	awaitKlingE2E(t, ctx, storage, client, taskID)
}

// TestKlingWorkflowOmniFirstLastRefEndToEnd uploads a first frame, last frame,
// and 1–4 reference stills (no feature video), then generates a video that
// starts on the first frame, ends on the last, and keeps element consistency
// from the reference images.
//
// Run:
//
//	env -u KWS_LANE_ID go test -tags=e2e \
//	  ./internal/capability/videogen -run TestKlingWorkflowOmniFirstLastRefEndToEnd \
//	  -v -count=1 -timeout=25h
func TestKlingWorkflowOmniFirstLastRefEndToEnd(t *testing.T) {
	if os.Getenv("KWS_LANE_ID") != "" {
		t.Fatal("KWS_LANE_ID must be empty for staging mainline E2E")
	}
	firstPath := os.Getenv("KLING_OMNI_FIRST_FRAME_PATH")
	lastPath := os.Getenv("KLING_OMNI_LAST_FRAME_PATH")
	if firstPath == "" || lastPath == "" {
		t.Fatal("KLING_OMNI_FIRST_FRAME_PATH and KLING_OMNI_LAST_FRAME_PATH are required")
	}
	paths := strings.Split(os.Getenv("KLING_OMNI_REFERENCE_PATHS"), ",")
	if n := len(paths); n < 1 || n > 4 {
		t.Fatal("KLING_OMNI_REFERENCE_PATHS must contain 1–4 comma-separated paths")
	}
	storage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	firstFrame := uploadE2EBlob(t, storage, firstPath, "image/png")
	lastFrame := uploadE2EBlob(t, storage, lastPath, "image/png")
	references := make([]string, 0, len(paths))
	for _, imagePath := range paths {
		references = append(references, uploadE2EBlob(t, storage, strings.TrimSpace(imagePath), "image/png"))
	}
	prompt := "从首帧过渡到尾帧"
	for i := range references {
		prompt += fmt.Sprintf("，@image_%d 作为人物妆造与元素参考", i+1)
	}
	prompt += "。镜头缓慢推进，电影质感，人物自然微动。"
	client := NewKlingWorkflow(storage, nil).Omni()
	multiShot := false
	request, err := (KlingOmniRequest{
		Prompt:      prompt,
		FirstFrame:  firstFrame,
		LastFrame:   lastFrame,
		ReferImages: references,
		Resolution:  "720p",
		Duration:    "5",
		Audio:       "off",
		MultiShot:   &multiShot,
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	taskID, err := client.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded first=%s last=%s refs=%v; submitted keffects workflow instance %s",
		firstFrame, lastFrame, references, taskID)
	awaitKlingE2E(t, ctx, storage, client, taskID)
}

func uploadE2EBlob(t *testing.T, storage artifact.Storage, path, contentType string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	ext := ".bin"
	switch contentType {
	case "image/png", "image/jpeg":
		ext = ".png"
	case "video/mp4":
		ext = ".mp4"
	}
	object, err := storage.Put(context.Background(),
		fmt.Sprintf("capabilities/videos/kling/e2e/%x%s", sum[:16], ext), data, contentType)
	if err != nil {
		t.Fatal(err)
	}
	return object.URL
}

// TestKlingWorkflowSubmittedFirstFrame inspects a previously submitted
// keffects instance without creating another generation task.
func TestKlingWorkflowSubmittedFirstFrame(t *testing.T) {
	rawID := os.Getenv("KEFFECTS_WORKFLOW_INSTANCE_ID")
	instanceID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || instanceID <= 0 {
		t.Fatal("KEFFECTS_WORKFLOW_INSTANCE_ID must be a positive integer")
	}
	client := keffects.NewClient(keffects.Config{TenantName: klingWorkflowTenantName})
	submitted, err := client.QuerySubmitRequest(context.Background(), instanceID)
	if err != nil {
		t.Fatal(err)
	}
	args := argumentsByName(submitted.Arguments)
	first := args["first_frame"].GetMediaValue()
	if first == nil {
		t.Fatalf("server-side request has no first_frame: %+v", submitted.Arguments)
	}
	t.Logf("server-side first_frame: bucket=%q key=%q", first.Bucket, first.Key)
	if first.Bucket == "" || first.Key == "" {
		t.Fatal("server-side first_frame is empty")
	}
	result, err := client.QueryResult(context.Background(), instanceID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("server-side result: status=%d message=%q error=%+v",
		result.Status, result.Message, result.ErrorDetail)
}

// TestKlingWorkflowDownloadResult downloads a completed workflow output
// through the Kess-aware BlobStore SDK. It does not submit a new generation.
func TestKlingWorkflowDownloadResult(t *testing.T) {
	rawID := os.Getenv("KEFFECTS_WORKFLOW_INSTANCE_ID")
	instanceID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || instanceID <= 0 {
		t.Fatal("KEFFECTS_WORKFLOW_INSTANCE_ID must be a positive integer")
	}
	outputPath := os.Getenv("KLING_E2E_OUTPUT_PATH")
	if outputPath == "" {
		t.Fatal("KLING_E2E_OUTPUT_PATH is required")
	}
	storage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		t.Fatal(err)
	}
	client := NewKlingWorkflow(storage, nil)
	if client.rpc == nil {
		t.Fatal("keffects media client is required")
	}
	result, err := client.rpc.QueryResult(context.Background(), instanceID)
	if err != nil {
		t.Fatal(err)
	}
	media, err := keffects.FindMedia(result.Outputs, klingWorkflowOutputName)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := client.rpc.OutputURI(media)
	if err != nil {
		t.Fatal(err)
	}
	object, ok := storage.Resolve(uri)
	if !ok {
		t.Fatalf("unexpected output URI: %s", uri)
	}
	reader, err := storage.Open(context.Background(), object)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	file, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	size, err := io.Copy(file, reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("downloaded %d bytes to %s", size, outputPath)
}

func awaitKlingE2E(
	t *testing.T,
	ctx context.Context,
	storage artifact.Storage,
	poller tool.Poller,
	taskID string,
) {
	t.Helper()
	interval := 5 * time.Second
	consecutiveErrors := 0
	for {
		result, err := poller.Poll(ctx, taskID)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= 6 {
				t.Fatalf("query failed %d consecutive times: %v", consecutiveErrors, err)
			}
			t.Logf("transient query error (%d/6): %v", consecutiveErrors, err)
		} else {
			consecutiveErrors = 0
			if result.Failed {
				t.Fatalf("workflow failed: %s", result.FailureReason)
			}
			if result.Done {
				var output ActivityOutput
				if err := json.Unmarshal(result.Result, &output); err != nil {
					t.Fatal(err)
				}
				if len(output.Artifacts) != 1 {
					t.Fatalf("unexpected artifacts: %+v", output.Artifacts)
				}
				if _, ok := storage.Resolve(output.Artifacts[0].URI); !ok {
					t.Fatalf("result is not an agent-k blob artifact: %s", output.Artifacts[0].URI)
				}
				object, _ := storage.Resolve(output.Artifacts[0].URI)
				reader, err := storage.Open(ctx, object)
				if err != nil {
					t.Fatalf("open result through artifact storage: %v", err)
				}
				var firstByte [1]byte
				if _, err := io.ReadFull(reader, firstByte[:]); err != nil {
					_ = reader.Close()
					t.Fatalf("read result through artifact storage: %v", err)
				}
				_ = reader.Close()
				if outputPath := os.Getenv("KLING_E2E_OUTPUT_PATH"); outputPath != "" {
					reader, err = storage.Open(ctx, object)
					if err != nil {
						t.Fatalf("reopen result for download: %v", err)
					}
					file, err := os.Create(outputPath)
					if err != nil {
						_ = reader.Close()
						t.Fatalf("create %s: %v", outputPath, err)
					}
					size, err := io.Copy(file, reader)
					_ = file.Close()
					_ = reader.Close()
					if err != nil {
						t.Fatalf("write %s: %v", outputPath, err)
					}
					t.Logf("downloaded %d bytes to %s", size, outputPath)
				}
				t.Logf("video result: %s", result.Result)
				return
			}
			t.Logf("workflow status: %s", result.StatusDetail)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(interval):
		}
		if interval < 30*time.Second {
			interval = min(time.Duration(float64(interval)*1.5), 30*time.Second)
		}
	}
}
