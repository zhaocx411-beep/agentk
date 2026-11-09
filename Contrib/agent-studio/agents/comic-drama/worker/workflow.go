// ComicDramaFilm 父 workflow：把一次漫剧成片固化为确定性流水线，阶段结构
// 与 agentq drama-workflow（qai-sdk）对齐：
//
//	剧本（含分集）→ 人物定妆描述 + 全剧固定音色 → 切集 → 定妆三视图（并行）
//	→ 逐集 child workflow（集间串行，见 episode.go）→ 全剧合成
//
// 每个阶段一个精调 prompt，顺序与校验由代码保证，模型只负责在模板里填内容
// ——这是与 agent loop 版本的本质区别。
//
// 失败语义：单集失败不判死整次执行，成功集按序合成；全部集失败才失败。
// 所有中间产物（含失败原因）都进资产清单。
package worker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	sdkinvocation "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// 上限与 qai 对齐：drama-script 最多 10 集；MAX_SHOTS_PER_EPISODE=10；
	// MAX_CHARACTERS_PER_SHOT=3；首帧参考图 MAX_REFS_PER_FRAME=4。
	defaultEpisodes     = 1
	hardMaxEpisodes     = 10
	defaultMaxShots     = 10
	hardMaxShots        = 10
	maxCharacters       = 10
	maxCharactersInShot = 3
	maxFrameRefs        = 4
	maxAssetsPerEpisode = 12
	minShotSeconds      = 5
	maxShotSeconds      = 10
)

var nonRetryableActivityErrorTypes = []string{
	"NotFound",
	"InvalidArgument",
	"Configuration",
	"PermissionDenied",
	"UpstreamRequestRejected",
}

func ComicDramaWorkflow(ctx workflow.Context, input DramaInput) (DramaOutput, error) {
	input, err := normalizeInput(input)
	if err != nil {
		return DramaOutput{}, err
	}
	styleAnchor := styleAnchorOrDefault(input.StylePrompt)
	output := DramaOutput{StyleAnchor: styleAnchor}

	// 阶段 1：完整分集剧本。
	script, err := generateScript(ctx, input)
	if err != nil {
		return DramaOutput{}, err
	}
	output.Title = script.Title
	output.Synopsis = script.Synopsis
	output.Script = script.Script
	reportProgress(ctx, "script", "剧本完成："+script.Title, nil)

	// 阶段 2：人物定妆描述 + 全剧固定音色（qai costume_desc + voice_desc）。
	costumes, voices, err := generateCostumes(ctx, input, script.Script, styleAnchor)
	if err != nil {
		return DramaOutput{}, err
	}
	output.Voices = voices
	reportProgress(ctx, "characters",
		fmt.Sprintf("人物设定完成，共 %d 人，音色 %d 条", len(costumes), len(voices)), nil)

	// 阶段 3：切集（确定性正则优先，LLM 兜底）。
	episodes, err := splitEpisodes(ctx, input, script.Script)
	if err != nil {
		return DramaOutput{}, err
	}
	reportProgress(ctx, "split", fmt.Sprintf("切集完成，共 %d 集", len(episodes)), nil)

	// 阶段 4：定妆三视图，人物间并行。单个人物失败不阻塞：
	// 资产图与首帧阶段少一张身份参考。
	output.Characters = generatePortraits(ctx, input, styleAnchor, costumes)

	// 阶段 5：逐集 child workflow，集间串行（后集依赖前集的剧本、资产与
	// 分镜连续性上下文，与 qai 一致）。
	for _, ep := range episodes {
		episode := runEpisode(ctx, input, styleAnchor, output, ep)
		output.Episodes = append(output.Episodes, episode)
		if episode.Error != "" {
			reportProgress(ctx, fmt.Sprintf("episode/%d", ep.Ep),
				fmt.Sprintf("第 %d 集失败：%s", ep.Ep, episode.Error), nil)
			continue
		}
		reportProgress(ctx, fmt.Sprintf("episode/%d", ep.Ep),
			fmt.Sprintf("第 %d 集完成，共 %d 个镜头", ep.Ep, episode.ClipCount),
			mustJSON(map[string]string{"film": episode.Film}))
	}

	// 阶段 6：全剧成片。单集直接用该集成片；多集把成功集按序再合成。
	films := make([]string, 0, len(output.Episodes))
	for _, episode := range output.Episodes {
		if episode.Film != "" {
			films = append(films, episode.Film)
			output.ClipCount += episode.ClipCount
		}
	}
	if len(films) == 0 {
		for _, episode := range output.Episodes {
			if episode.Error != "" {
				return DramaOutput{}, fmt.Errorf("comic-drama: all episodes failed: %s", episode.Error)
			}
		}
		return DramaOutput{}, fmt.Errorf("comic-drama: all episodes failed")
	}
	if len(films) == 1 {
		output.Film = films[0]
	} else {
		film, err := concatVideos(ctx, films)
		if err != nil {
			return DramaOutput{}, err
		}
		output.Film = film
	}
	reportProgress(ctx, "film",
		fmt.Sprintf("成片完成，共 %d 集 %d 个镜头", len(films), output.ClipCount),
		mustJSON(map[string]string{"film": output.Film}))
	return output, nil
}

func normalizeInput(input DramaInput) (DramaInput, error) {
	input.Brief = strings.TrimSpace(input.Brief)
	input.Title = strings.TrimSpace(input.Title)
	input.StylePrompt = strings.TrimSpace(input.StylePrompt)
	input.ChatModel = strings.TrimSpace(input.ChatModel)
	input.ImageModel = strings.TrimSpace(input.ImageModel)
	input.EditModel = strings.TrimSpace(input.EditModel)
	input.VideoModel = strings.TrimSpace(input.VideoModel)
	if input.Brief == "" || input.ChatModel == "" || input.ImageModel == "" ||
		input.EditModel == "" || input.VideoModel == "" {
		return DramaInput{}, temporal.NewNonRetryableApplicationError(
			"comic-drama: brief, chatModel, imageModel, editModel and videoModel are required",
			"InvalidArgument",
			nil,
		)
	}
	if input.Episodes == 0 {
		input.Episodes = defaultEpisodes
	}
	if input.Episodes < 1 || input.Episodes > hardMaxEpisodes {
		return DramaInput{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("comic-drama: episodes must be between 1 and %d", hardMaxEpisodes),
			"InvalidArgument",
			nil,
		)
	}
	if input.MaxShots == 0 {
		input.MaxShots = defaultMaxShots
	}
	if input.MaxShots < 1 || input.MaxShots > hardMaxShots {
		return DramaInput{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("comic-drama: maxShots must be between 1 and %d", hardMaxShots),
			"InvalidArgument",
			nil,
		)
	}
	if input.AspectRatio == "" {
		input.AspectRatio = "9:16"
	}
	if input.Resolution == "" {
		input.Resolution = "1080p"
	}
	if input.Audio == "" {
		input.Audio = "native"
	}
	return input, nil
}

func generateScript(ctx workflow.Context, input DramaInput) (scriptResult, error) {
	var result scriptResult
	if err := runTextgen(ctx, input.ChatModel,
		buildScriptPrompt(input.Brief, input.Title, input.Episodes),
		scriptSchema(), &result); err != nil {
		return scriptResult{}, err
	}
	result.Script = strings.TrimSpace(result.Script)
	if result.Script == "" {
		return scriptResult{}, fmt.Errorf("comic-drama: script stage returned empty script")
	}
	// 用户显式给了标题时不让模型改名。
	if input.Title != "" {
		result.Title = input.Title
	}
	if strings.TrimSpace(result.Title) == "" {
		result.Title = truncateRunes(input.Brief, 24)
	}
	return result, nil
}

// generateCostumes 阶段 2：全剧人物定妆描述 + 固定音色（qai costume.ts）。
func generateCostumes(
	ctx workflow.Context,
	input DramaInput,
	script, styleAnchor string,
) ([]costumeDesc, []VoiceAsset, error) {
	var result costumeResult
	if err := runTextgen(ctx, input.ChatModel,
		buildCostumePrompt(script, styleAnchor), costumeSchema(), &result); err != nil {
		return nil, nil, err
	}
	costumes := make([]costumeDesc, 0, len(result.Costumes))
	seen := make(map[string]struct{})
	for _, c := range result.Costumes {
		c.Name = strings.TrimSpace(c.Name)
		c.Variant = strings.TrimSpace(c.Variant)
		c.Prompt = strings.TrimSpace(c.Prompt)
		if c.Name == "" || c.Prompt == "" {
			continue
		}
		key := c.Name + "\x00" + c.Variant
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		costumes = append(costumes, c)
		if len(costumes) == maxCharacters {
			break
		}
	}
	voices := make([]VoiceAsset, 0, len(result.Voices))
	seenVoice := make(map[string]struct{})
	for _, v := range result.Voices {
		v.Name = strings.TrimSpace(v.Name)
		v.Timbre = strings.TrimSpace(v.Timbre)
		if v.Name == "" || v.Timbre == "" {
			continue
		}
		if _, dup := seenVoice[v.Name]; dup {
			continue
		}
		seenVoice[v.Name] = struct{}{}
		voices = append(voices, v)
	}
	// 允许零人物（纯空镜叙事）：首帧阶段退化为纯文生图。
	return costumes, voices, nil
}

// episodeHeaderRe 匹配「第X集」标题行（qai split-script 的分集标记约定）。
var episodeHeaderRe = regexp.MustCompile(`(?m)^[ \t]*第([0-9０-９一二三四五六七八九十百]+)[集话回][^\n]*$`)

// splitEpisodes 阶段 3：确定性正则按「第X集」标题行切集；剧本没有标记且
// 期望多集时用 LLM 兜底（qai split-script.ts 的策略）。结果截断到硬上限。
func splitEpisodes(
	ctx workflow.Context,
	input DramaInput,
	script string,
) ([]splitEpisode, error) {
	episodes := splitByHeaders(script)
	if len(episodes) < 2 && input.Episodes > 1 {
		var result splitResult
		if err := runTextgen(ctx, input.ChatModel,
			buildSplitPrompt(script), splitSchema(), &result); err == nil {
			parsed := make([]splitEpisode, 0, len(result.Episodes))
			for _, ep := range result.Episodes {
				ep.Content = strings.TrimSpace(ep.Content)
				if ep.Content == "" {
					continue
				}
				parsed = append(parsed, ep)
			}
			if len(parsed) >= 2 {
				episodes = parsed
			}
		}
	}
	if len(episodes) == 0 {
		episodes = []splitEpisode{{Ep: 1, Title: "第1集", Content: script}}
	}
	// 集号按出现顺序重排：模型或剧本的集号可能缺失、重复。
	for i := range episodes {
		episodes[i].Ep = i + 1
		if strings.TrimSpace(episodes[i].Title) == "" {
			episodes[i].Title = fmt.Sprintf("第%d集", i+1)
		}
	}
	if len(episodes) > hardMaxEpisodes {
		episodes = episodes[:hardMaxEpisodes]
	}
	return episodes, nil
}

// splitByHeaders 确定性切集：每个「第X集」标题行开启新的一集，标题行本身
// 保留在该集内容里（qai 同约定：content 含集标题行）。
func splitByHeaders(script string) []splitEpisode {
	matches := episodeHeaderRe.FindAllStringIndex(script, -1)
	if len(matches) == 0 {
		return nil
	}
	episodes := make([]splitEpisode, 0, len(matches))
	for i, match := range matches {
		end := len(script)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		content := strings.TrimSpace(script[match[0]:end])
		if content == "" {
			continue
		}
		title := strings.TrimSpace(script[match[0]:match[1]])
		episodes = append(episodes, splitEpisode{Title: title, Content: content})
	}
	return episodes
}

// generatePortraits 人物间并行生成定妆三视图。三视图正/侧/背并排，必须横版
// 出图（qai costume-images：方图会把三个视角挤扁），与成片画幅无关。
func generatePortraits(
	ctx workflow.Context,
	input DramaInput,
	styleAnchor string,
	costumes []costumeDesc,
) []CharacterAsset {
	assets := make([]CharacterAsset, len(costumes))
	group := workflow.NewWaitGroup(ctx)
	for i, costume := range costumes {
		i, costume := i, costume
		assets[i] = CharacterAsset{
			Name:    costume.Name,
			Variant: costume.Variant,
			Prompt:  composePortraitPrompt(costume.Prompt, styleAnchor),
		}
		group.Add(1)
		workflow.Go(ctx, func(gctx workflow.Context) {
			defer group.Done()
			imageCtx := workflow.WithActivityOptions(gctx, workflow.ActivityOptions{
				TaskQueue:              coreTaskQueue(gctx),
				StartToCloseTimeout:    10 * time.Minute,
				ScheduleToCloseTimeout: 30 * time.Minute,
				RetryPolicy:            activityRetryPolicy(2),
				WaitForCancellation:    true,
			})
			var portrait generateImageOutput
			err := workflow.ExecuteActivity(imageCtx, generateImageActivity, generateImageInput{
				Model:       input.ImageModel,
				Prompt:      assets[i].Prompt,
				AspectRatio: "16:9",
			}).Get(imageCtx, &portrait)
			if err != nil {
				assets[i].Error = err.Error()
				return
			}
			portraitURL := firstArtifactURI(portrait.Artifacts)
			if portraitURL == "" {
				assets[i].Error = "comic-drama: generate_image returned no artifacts"
				return
			}
			assets[i].ImageURL = portraitURL
			reportProgress(gctx, fmt.Sprintf("portrait/%d", i),
				"定妆完成："+displayCharacterName(assets[i]),
				mustJSON(map[string]string{"name": assets[i].Name, "imageUrl": portraitURL}))
		})
	}
	group.Wait(ctx)
	return assets
}

func displayCharacterName(asset CharacterAsset) string {
	if asset.Variant != "" {
		return asset.Name + "（" + asset.Variant + "）"
	}
	return asset.Name
}

// runEpisode 启动一集的 child workflow。失败折叠成 EpisodeAsset.Error，
// 不打断后续集。
func runEpisode(
	ctx workflow.Context,
	input DramaInput,
	styleAnchor string,
	output DramaOutput,
	ep splitEpisode,
) EpisodeAsset {
	episodeInput := EpisodeInput{
		Number:      ep.Ep,
		Title:       ep.Title,
		Script:      ep.Content,
		StyleAnchor: styleAnchor,
		MaxShots:    input.MaxShots,
		Characters:  output.Characters,
		Voices:      output.Voices,
		Prev:        previousContexts(output.Episodes),
		ChatModel:   input.ChatModel,
		ImageModel:  input.ImageModel,
		EditModel:   input.EditModel,
		VideoModel:  input.VideoModel,
		AspectRatio: input.AspectRatio,
		Resolution:  input.Resolution,
		Audio:       input.Audio,
	}
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: fmt.Sprintf("%s/ep%02d",
			workflow.GetInfo(ctx).WorkflowExecution.ID, ep.Ep),
		TaskQueue: WorkerTaskQueue,
		// 单集兜底超时：正常一集几十分钟内完成，2 小时足以覆盖生成高峰的
		// 排队，又不会让异常执行长时间占着资源。
		WorkflowRunTimeout: 2 * time.Hour,
	})
	var episode EpisodeAsset
	if err := workflow.ExecuteChildWorkflow(
		childCtx, EpisodeWorkflowType, episodeInput,
	).Get(childCtx, &episode); err != nil {
		return EpisodeAsset{
			Number: ep.Ep,
			Title:  ep.Title,
			Script: ep.Content,
			Error:  err.Error(),
		}
	}
	return episode
}

// previousContexts 取最近两集成功产物作为下一集的连续性上下文（时间倒序）：
// 资产描述看前两集，分镜看前一集（qai steps 同取材）。
func previousContexts(episodes []EpisodeAsset) []EpisodeContext {
	var prev []EpisodeContext
	for i := len(episodes) - 1; i >= 0 && len(prev) < 2; i-- {
		episode := episodes[i]
		if episode.Error != "" {
			continue
		}
		prev = append(prev, EpisodeContext{
			Number: episode.Number,
			Script: episode.Script,
			Assets: episode.Assets,
			Shots:  episode.Shots,
		})
	}
	return prev
}

// concatVideos 按序合成一组视频，返回成片 URL。
func concatVideos(ctx workflow.Context, videoURLs []string) (string, error) {
	composeCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:              coreTaskQueue(ctx),
		StartToCloseTimeout:    30 * time.Minute,
		ScheduleToCloseTimeout: time.Hour,
		HeartbeatTimeout:       time.Minute,
		RetryPolicy:            activityRetryPolicy(2),
		WaitForCancellation:    true,
	})
	var composed concatVideosOutput
	if err := workflow.ExecuteActivity(composeCtx, concatVideosActivity, concatVideosInput{
		VideoURLs: videoURLs,
	}).Get(composeCtx, &composed); err != nil {
		return "", err
	}
	filmURL := firstArtifactURI(composed.Artifacts)
	if filmURL == "" {
		return "", fmt.Errorf("comic-drama: concat_videos returned no artifacts")
	}
	return filmURL, nil
}

func runTextgen(
	ctx workflow.Context,
	model, prompt string,
	schema []byte,
	target any,
) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:              coreTaskQueue(ctx),
		StartToCloseTimeout:    15 * time.Minute,
		ScheduleToCloseTimeout: 45 * time.Minute,
		HeartbeatTimeout:       30 * time.Second,
		RetryPolicy:            activityRetryPolicy(3),
		WaitForCancellation:    true,
	})
	var output generateTextOutput
	if err := workflow.ExecuteActivity(activityCtx, generateTextActivity, generateTextInput{
		Model:        model,
		Prompt:       prompt,
		OutputSchema: schema,
	}).Get(activityCtx, &output); err != nil {
		return err
	}
	if err := json.Unmarshal(output.Result, target); err != nil {
		return fmt.Errorf("comic-drama: decode textgen result: %w", err)
	}
	return nil
}

func coreTaskQueue(ctx workflow.Context) string {
	if meta, ok := sdkinvocation.MetaFromWorkflow(ctx); ok && meta.CallbackTaskQueue != "" {
		return meta.CallbackTaskQueue
	}
	return DefaultCoreTaskQueue
}

func reportProgress(ctx workflow.Context, stepID, message string, data []byte) {
	if err := sdkinvocation.ReportProgress(ctx, stepID, message, data); err != nil {
		workflow.GetLogger(ctx).Warn("comic-drama: report progress failed",
			"stepId", stepID, "error", err)
	}
}

func activityRetryPolicy(maximumAttempts int32) *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:        time.Second,
		BackoffCoefficient:     2,
		MaximumInterval:        10 * time.Second,
		MaximumAttempts:        maximumAttempts,
		NonRetryableErrorTypes: nonRetryableActivityErrorTypes,
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}
