package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// fakeCore 在内存里模拟 agent-k 核心能力 Activity。textgen 按 prompt 内容
// 区分阶段（剧本 / 定妆音色 / 切集 / 资产描述 / 台词精简 / 分镜），与
// workflow 的调用顺序解耦。
type fakeCore struct {
	mu           sync.Mutex
	textInputs   []generateTextInput
	imageInputs  []generateImageInput
	editInputs   []editImageInput
	videoInputs  []klingOmniVideoInput
	concatInputs []concatVideosInput

	scriptJSON    string
	costumeJSON   string
	assetDescJSON string
	boardJSON     string
	failEditFor   string // prompt 包含该子串的 edit_image 调用返回失败
	failVideoFor  string // prompt 包含该子串的视频调用返回失败
}

func newFakeCore() *fakeCore {
	return &fakeCore{
		scriptJSON: `{
			"title": "猫咪开门记",
			"synopsis": "一只猫学会了开门",
			"script": "第1集\n1-1客厅 夜 内\n人物：小猫、主人\n【小猫用爪子扒动门把手。】\n旁白：这个夜晚注定不平静。\n主人：\"你怎么会开门？！\""
		}`,
		costumeJSON: `{
			"costumes": [
				{"name": "小猫", "variant": "", "prompt": "橘色短毛猫，圆眼睛，白色肚皮"},
				{"name": "主人", "variant": "", "prompt": "年轻男性，居家服，睡眼惺忪"}
			],
			"voices": [
				{"name": "小猫", "gender": "", "age": "少年", "timbre": "清亮细软的猫叫声线，语速轻快，气音明显"},
				{"name": "主人", "gender": "男", "age": "青年", "timbre": "中低音，厚实带些许沙哑，语速平缓，咬字松弛"},
				{"name": "旁白", "gender": "男", "age": "中年", "timbre": "低沉磁性，颗粒感强，语速沉稳，尾音干净"}
			]
		}`,
		assetDescJSON: `{
			"assets": [
				{"category": "人物状态", "name": "小猫（扒门）", "character_ref": "小猫", "prompt": "橘猫后腿站立扒住门把手", "reuse_from_ep": null, "reuse_asset_name": null},
				{"category": "场景", "name": "客厅", "character_ref": null, "prompt": "深夜客厅，一扇木门", "reuse_from_ep": null, "reuse_asset_name": null},
				{"category": "道具", "name": "门把手", "character_ref": null, "prompt": "金属球形门把手特写", "reuse_from_ep": null, "reuse_asset_name": null}
			]
		}`,
		boardJSON: `{
			"shots": [
				{"shot_id": "01", "duration_s": 6, "characters": ["小猫"], "scenes": ["客厅"], "props": ["门把手"], "first_frame": "小猫蹲在门前抬头看门把手", "storyboard": "【景别：中景】小猫跃起扒住门把手。旁白：这个夜晚注定不平静。", "speakers": ["旁白"]},
				{"shot_id": "02", "duration_s": 8, "characters": ["小猫", "主人"], "scenes": ["客厅"], "props": [], "first_frame": "门开了，主人站在门口惊讶", "storyboard": "【景别：全景】主人说：\"你怎么会开门？！\"", "speakers": ["主人"]}
			]
		}`,
	}
}

func (f *fakeCore) register(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateTextInput) (generateTextOutput, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.textInputs = append(f.textInputs, input)
			switch {
			case strings.Contains(input.Prompt, "完整可拍摄剧本"):
				return generateTextOutput{Result: json.RawMessage(f.scriptJSON)}, nil
			case strings.Contains(input.Prompt, "人物/场景/图片资产图"):
				return generateTextOutput{Result: json.RawMessage(f.assetDescJSON)}, nil
			case strings.Contains(input.Prompt, "人物定妆照"):
				return generateTextOutput{Result: json.RawMessage(f.costumeJSON)}, nil
			case strings.Contains(input.Prompt, "超长对白"):
				return generateTextOutput{Result: json.RawMessage(`{"script": ""}`)}, nil
			case strings.Contains(input.Prompt, "分镜 JSON"):
				return generateTextOutput{Result: json.RawMessage(f.boardJSON)}, nil
			case strings.Contains(input.Prompt, "按集数拆分"):
				return generateTextOutput{Result: json.RawMessage(`{"episodes": []}`)}, nil
			}
			return generateTextOutput{}, fmt.Errorf("unexpected textgen prompt: %s", input.Prompt[:40])
		},
		activity.RegisterOptions{Name: generateTextActivity},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateImageInput) (generateImageOutput, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.imageInputs = append(f.imageInputs, input)
			return generateImageOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "image",
					URI:  fmt.Sprintf("https://image/gen-%d.png", len(f.imageInputs)),
				}},
			}, nil
		},
		activity.RegisterOptions{Name: generateImageActivity},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input editImageInput) (editImageOutput, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.editInputs = append(f.editInputs, input)
			if f.failEditFor != "" && strings.Contains(input.Prompt, f.failEditFor) {
				return editImageOutput{}, temporal.NewNonRetryableApplicationError(
					"upstream rejected", "UpstreamRequestRejected", nil)
			}
			return editImageOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "image",
					URI:  fmt.Sprintf("https://image/edit-%d.png", len(f.editInputs)),
				}},
			}, nil
		},
		activity.RegisterOptions{Name: editImageActivity},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input klingOmniVideoInput) (klingOmniVideoOutput, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.videoInputs = append(f.videoInputs, input)
			if f.failVideoFor != "" && strings.Contains(input.Prompt, f.failVideoFor) {
				return klingOmniVideoOutput{}, temporal.NewNonRetryableApplicationError(
					"upstream rejected", "UpstreamRequestRejected", nil)
			}
			return klingOmniVideoOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "video",
					URI:  fmt.Sprintf("https://video/clip-%d.mp4", len(f.videoInputs)),
				}},
			}, nil
		},
		activity.RegisterOptions{Name: klingOmniVideoActivity},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input concatVideosInput) (concatVideosOutput, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.concatInputs = append(f.concatInputs, input)
			return concatVideosOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "video",
					URI:  fmt.Sprintf("https://video/film-%d.mp4", len(f.concatInputs)),
				}},
				ClipCount: len(input.VideoURLs),
			}, nil
		},
		activity.RegisterOptions{Name: concatVideosActivity},
	)
}

func newEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *fakeCore) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(
		ComicDramaWorkflow,
		workflow.RegisterOptions{Name: WorkflowType},
	)
	env.RegisterWorkflowWithOptions(
		ComicDramaEpisodeWorkflow,
		workflow.RegisterOptions{Name: EpisodeWorkflowType},
	)
	core := newFakeCore()
	core.register(env)
	return env, core
}

func testInput() DramaInput {
	return DramaInput{
		Brief:      "一只猫学会了开门",
		ChatModel:  "chat-cap",
		ImageModel: "image-cap",
		EditModel:  "edit-cap",
		VideoModel: "video-cap",
	}
}

func TestComicDramaWorkflow(t *testing.T) {
	env, core := newEnv(t)
	env.ExecuteWorkflow(WorkflowType, testInput())
	require.NoError(t, env.GetWorkflowError())

	var output DramaOutput
	require.NoError(t, env.GetWorkflowResult(&output))

	require.Equal(t, "猫咪开门记", output.Title)
	require.Equal(t, "一只猫学会了开门", output.Synopsis)
	require.Contains(t, output.Script, "第1集")
	require.Equal(t, defaultStyleAnchor, output.StyleAnchor)

	// 人物定妆：2 人，prompt 带风格锚点与三视图规范，横版 16:9。
	require.Len(t, output.Characters, 2)
	require.Equal(t, "小猫", output.Characters[0].Name)
	require.NotEmpty(t, output.Characters[0].ImageURL)
	require.Contains(t, output.Characters[0].Prompt, defaultStyleAnchor)
	require.Contains(t, output.Characters[0].Prompt, "三视图")

	// 音色：全剧固定，含旁白。
	require.Len(t, output.Voices, 3)
	require.Equal(t, "旁白", output.Voices[2].Name)

	// 单集：切出 1 集，走完整分集子流程。
	require.Len(t, output.Episodes, 1)
	episode := output.Episodes[0]
	require.Empty(t, episode.Error)
	require.Equal(t, 1, episode.Number)

	// 分集资产：人物状态走 edit_image（一致性前缀 + 定妆参考），场景 / 道具
	// 走 generate_image。
	require.Len(t, episode.Assets, 3)
	stateAsset := episode.Assets[0]
	require.Equal(t, "小猫", stateAsset.CharacterRef)
	require.NotEmpty(t, stateAsset.ImageURL)
	var stateEditSeen bool
	for _, edit := range core.editInputs {
		if strings.Contains(edit.Prompt, "严格保持人物一致性") {
			stateEditSeen = true
			require.Len(t, edit.ReferenceImages, 1)
			require.Equal(t, output.Characters[0].ImageURL, edit.ReferenceImages[0])
		}
	}
	require.True(t, stateEditSeen, "人物状态图应走一致性 edit_image")

	// 分镜：2 镜，qai 形状（shot_id / speakers / storyboard）。
	require.Len(t, episode.Shots, 2)
	for _, shot := range episode.Shots {
		require.Empty(t, shot.Error)
		require.NotEmpty(t, shot.ShotID)
		require.NotEmpty(t, shot.FrameURL)
		require.NotEmpty(t, shot.VideoURL)
		require.Contains(t, shot.FramePrompt, "静止画面")
		require.Contains(t, shot.VideoPrompt, defaultStyleAnchor)
		// 音色注入：每镜说话人的固定嗓音写进视频 prompt。
		require.Contains(t, shot.VideoPrompt, "本片段说话人音色设定")
	}
	require.Contains(t, episode.Shots[0].VideoPrompt, "低沉磁性")   // 旁白
	require.Contains(t, episode.Shots[1].VideoPrompt, "中低音，厚实") // 主人

	// 首帧参考图：状态图 / 定妆 / 场景 / 道具按优先级注入，上限 4。
	var frameEdits []editImageInput
	for _, edit := range core.editInputs {
		if strings.Contains(edit.Prompt, "起始画面") {
			frameEdits = append(frameEdits, edit)
			require.Equal(t, "9:16", edit.AspectRatio)
			require.NotEmpty(t, edit.ReferenceImages)
			require.LessOrEqual(t, len(edit.ReferenceImages), maxFrameRefs)
		}
	}
	require.Len(t, frameEdits, 2)

	// 视频：走可灵 Omni 方言面，首帧 + 参考图 + 时长与音频参数透传。
	require.Len(t, core.videoInputs, 2)
	durations := map[string]bool{}
	for _, video := range core.videoInputs {
		require.Equal(t, "video-cap", video.Model)
		require.Equal(t, "1080p", video.Resolution)
		require.Equal(t, "native", video.Audio)
		require.NotEmpty(t, video.FirstFrame)
		require.NotEmpty(t, video.ReferImages, "本镜参考图应随视频调用传给可灵 Omni")
		require.LessOrEqual(t, len(video.ReferImages), 7)
		require.Contains(t, video.Prompt, "@image_1", "视频 prompt 应包含参考图逐图对应段")
		durations[video.Duration] = true
	}
	require.True(t, durations["6"] && durations["8"], "分镜时长应透传给视频")

	// 成片：单集只合成一次，全剧成片即该集成片。
	require.Len(t, core.concatInputs, 1)
	require.Len(t, core.concatInputs[0].VideoURLs, 2)
	require.Equal(t, episode.Film, output.Film)
	require.Equal(t, 2, output.ClipCount)
}

func TestComicDramaWorkflowMultiEpisode(t *testing.T) {
	env, core := newEnv(t)
	core.scriptJSON = `{
		"title": "猫咪开门记",
		"synopsis": "一只猫学会了开门",
		"script": "第1集\n1-1客厅 夜 内\n【小猫扒门。】\n旁白：夜晚。\n\n第2集\n2-1客厅 日 内\n【主人装上了新门锁。】\n主人：\"看你还怎么开！\""
	}`
	input := testInput()
	input.Episodes = 2
	env.ExecuteWorkflow(WorkflowType, input)
	require.NoError(t, env.GetWorkflowError())

	var output DramaOutput
	require.NoError(t, env.GetWorkflowResult(&output))

	// 确定性切集：两集，各自跑完子流程。
	require.Len(t, output.Episodes, 2)
	require.Equal(t, 1, output.Episodes[0].Number)
	require.Equal(t, 2, output.Episodes[1].Number)
	require.Contains(t, output.Episodes[0].Script, "小猫扒门")
	require.Contains(t, output.Episodes[1].Script, "新门锁")
	for _, episode := range output.Episodes {
		require.Empty(t, episode.Error)
		require.NotEmpty(t, episode.Film)
		require.Equal(t, 2, episode.ClipCount)
	}

	// 第二集的资产描述 prompt 带第一集的剧本与资产上下文（复用判断）。
	var prevContextSeen bool
	for _, text := range core.textInputs {
		if strings.Contains(text.Prompt, "人物/场景/图片资产图") &&
			strings.Contains(text.Prompt, "【第1集资产图描述】") {
			prevContextSeen = true
		}
	}
	require.True(t, prevContextSeen, "第二集资产描述应携带第一集上下文")

	// 合成：每集一次 + 全剧一次。
	require.Len(t, core.concatInputs, 3)
	final := core.concatInputs[len(core.concatInputs)-1]
	require.Equal(t, []string{output.Episodes[0].Film, output.Episodes[1].Film}, final.VideoURLs)
	require.Equal(t, final.VideoURLs[0], output.Episodes[0].Film)
	require.NotEmpty(t, output.Film)
	require.Equal(t, 4, output.ClipCount)
}

func TestComicDramaWorkflowUserTitleAndStyleWin(t *testing.T) {
	env, _ := newEnv(t)
	input := testInput()
	input.Title = "我的猫剧"
	input.StylePrompt = "新中式水墨动画风格"
	env.ExecuteWorkflow(WorkflowType, input)
	require.NoError(t, env.GetWorkflowError())

	var output DramaOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, "我的猫剧", output.Title)
	require.Equal(t, "新中式水墨动画风格", output.StyleAnchor)
	require.Contains(t, output.Characters[0].Prompt, "新中式水墨动画风格")
	require.Contains(t, output.Episodes[0].Shots[0].FramePrompt, "新中式水墨动画风格")
}

func TestComicDramaWorkflowAllowsPartialShotFailure(t *testing.T) {
	env, core := newEnv(t)
	// 第二镜的视频失败（prompt 里含"你怎么会开门"）。
	core.failVideoFor = "你怎么会开门"
	env.ExecuteWorkflow(WorkflowType, testInput())
	require.NoError(t, env.GetWorkflowError())

	var output DramaOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	episode := output.Episodes[0]
	require.Empty(t, episode.Error)
	require.Equal(t, 1, episode.ClipCount)
	require.Equal(t, 1, output.ClipCount)
	require.NotEmpty(t, output.Film)

	require.Empty(t, episode.Shots[0].Error)
	require.NotEmpty(t, episode.Shots[0].VideoURL)
	require.Contains(t, episode.Shots[1].Error, "video:")
	require.Empty(t, episode.Shots[1].VideoURL)
	// 失败镜头的首帧仍保留在清单里。
	require.NotEmpty(t, episode.Shots[1].FrameURL)
}

func TestComicDramaWorkflowFailsWhenAllShotsFail(t *testing.T) {
	env, core := newEnv(t)
	core.failEditFor = "画出一部漫剧" // 所有首帧都失败
	env.ExecuteWorkflow(WorkflowType, testInput())
	require.Error(t, env.GetWorkflowError())
}

func TestComicDramaWorkflowRejectsMissingInput(t *testing.T) {
	env, _ := newEnv(t)
	input := testInput()
	input.Brief = ""
	env.ExecuteWorkflow(WorkflowType, input)
	require.Error(t, env.GetWorkflowError())
}

func TestSplitByHeaders(t *testing.T) {
	episodes := splitByHeaders("第1集 麒麟坠落\n内容A\n第2集\n内容B")
	require.Len(t, episodes, 2)
	require.Equal(t, "第1集 麒麟坠落", episodes[0].Title)
	require.Contains(t, episodes[0].Content, "内容A")
	require.Contains(t, episodes[1].Content, "内容B")

	require.Nil(t, splitByHeaders("没有分集标记的剧本"))
}
