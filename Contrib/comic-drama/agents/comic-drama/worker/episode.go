// ComicDramaEpisode 子 workflow：单集的完整生产线，阶段与 qai drama-workflow
// 的分集步骤一致：
//
//	资产描述（看前两集，判断复用）→ 台词精简 → 分镜（看前一集连续性）
//	→ 资产图（人物状态 / 场景 / 道具，并行）→ 逐镜首帧 + 视频（并行）→ 本集合成
//
// 只在父流程内部以 child workflow 启动。失败语义：资产描述与台词精简是软失败
// （缺资产退化为定妆参考 / 用原剧本）；分镜失败或全部镜头失败判死本集，
// 折叠进 EpisodeAsset.Error，由父流程决定是否继续后续集。
package worker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"
)

// narratorName 旁白在音色表与 speakers 里的固定名字（qai NARRATOR_NAME）。
const narratorName = "旁白"

func ComicDramaEpisodeWorkflow(ctx workflow.Context, input EpisodeInput) (EpisodeAsset, error) {
	episode := EpisodeAsset{
		Number: input.Number,
		Title:  input.Title,
		Script: strings.TrimSpace(input.Script),
	}
	if episode.Script == "" {
		episode.Error = "comic-drama: episode script is empty"
		return episode, nil
	}
	step := func(name string) string { return fmt.Sprintf("ep%d/%s", input.Number, name) }

	// 阶段 a：分集图片资产描述（软失败：没有资产时首帧退回定妆参考）。
	assets := generateAssetDesc(ctx, input)
	reportProgress(ctx, step("assets"),
		fmt.Sprintf("第 %d 集资产描述完成，共 %d 项", input.Number, len(assets)), nil)

	// 阶段 b：超长对白精简（软失败：用原剧本）。
	var simplified simplifyResult
	if err := runTextgen(ctx, input.ChatModel,
		buildSimplifyPrompt(episode.Script), simplifySchema(), &simplified); err == nil {
		if script := strings.TrimSpace(simplified.Script); script != "" {
			episode.Script = script
		}
	}

	// 阶段 c：分镜（硬失败：没有分镜就没有本集）。
	shots, err := generateStoryboard(ctx, input, episode.Script)
	if err != nil {
		episode.Error = "storyboard: " + err.Error()
		return episode, nil
	}
	reportProgress(ctx, step("board"),
		fmt.Sprintf("第 %d 集分镜完成，共 %d 镜", input.Number, len(shots)), nil)

	// 阶段 d：资产图，资产间并行。
	episode.Assets = generateAssetImages(ctx, input, assets)

	// 阶段 e+f：逐镜首帧 + 视频，镜头间并行、镜头内串行。
	episode.Shots = generateEpisodeShots(ctx, input, episode.Assets, shots)

	// 阶段 g：本集合成。
	successful := make([]string, 0, len(episode.Shots))
	for _, shot := range episode.Shots {
		if shot.VideoURL != "" {
			successful = append(successful, shot.VideoURL)
		}
	}
	if len(successful) == 0 {
		episode.Error = "comic-drama: all shots failed"
		for _, shot := range episode.Shots {
			if shot.Error != "" {
				episode.Error = "comic-drama: all shots failed: " + shot.Error
				break
			}
		}
		return episode, nil
	}
	film, err := concatVideos(ctx, successful)
	if err != nil {
		episode.Error = "compose: " + err.Error()
		return episode, nil
	}
	episode.Film = film
	episode.ClipCount = len(successful)
	reportProgress(ctx, step("film"),
		fmt.Sprintf("第 %d 集成片完成，共 %d 个镜头", input.Number, len(successful)),
		mustJSON(map[string]string{"film": film}))
	return episode, nil
}

/* ---------- 阶段 a：资产描述 ---------- */

// generateAssetDesc 本集 + 前两集剧本与资产描述 → 本集资产清单
// （qai steps/asset-desc.ts）。软失败返回空。
func generateAssetDesc(ctx workflow.Context, input EpisodeInput) []assetDesc {
	prev2Script, prev2Assets := previousBlocks(input.Prev, 1)
	prev1Script, prev1Assets := previousBlocks(input.Prev, 0)
	currentScript := fmt.Sprintf("【本集（第%d集）剧本全文】\n%s", input.Number, input.Script)

	var result assetDescResult
	if err := runTextgen(ctx, input.ChatModel,
		buildAssetDescPrompt(prev2Script, prev2Assets, prev1Script, prev1Assets,
			currentScript, input.StyleAnchor),
		assetDescSchema(), &result); err != nil {
		workflow.GetLogger(ctx).Warn("comic-drama: asset desc stage failed",
			"episode", input.Number, "error", err)
		return nil
	}

	assets := make([]assetDesc, 0, len(result.Assets))
	for _, asset := range result.Assets {
		asset.Category = strings.TrimSpace(asset.Category)
		asset.Name = strings.TrimSpace(asset.Name)
		asset.Prompt = strings.TrimSpace(asset.Prompt)
		asset.CharacterRef = strings.TrimSpace(asset.CharacterRef)
		if asset.Name == "" {
			continue
		}
		// 人物类推断 character_ref：去掉括号内状态，取人物基本名（qai parseOutput）。
		if asset.CharacterRef == "" && isCharacterCategory(asset.Category) {
			asset.CharacterRef = baseCharacterName(asset.Name)
		}
		if asset.ReuseFromEp == nil && asset.Prompt == "" {
			continue
		}
		assets = append(assets, asset)
		if len(assets) == maxAssetsPerEpisode {
			break
		}
	}
	return assets
}

// previousBlocks 第 index 个前集（0=上一集，1=上上集）的剧本块与资产描述块；
// 没有对应集时两块都为空（qai epBlock：剧本与资产必须成对出现）。
func previousBlocks(prev []EpisodeContext, index int) (scriptBlock, assetBlock string) {
	if index >= len(prev) {
		return "", ""
	}
	episode := prev[index]
	if episode.Script == "" || len(episode.Assets) == 0 {
		return "", ""
	}
	raw, err := json.Marshal(episodeAssetDescs(episode.Assets))
	if err != nil {
		return "", ""
	}
	return fmt.Sprintf("【第%d集剧本全文】\n%s", episode.Number, episode.Script),
		fmt.Sprintf("【第%d集资产图描述】\n%s", episode.Number, string(raw))
}

// episodeAssetDescs 把前集资产投影回模型可读的 qai snake_case 形状。
func episodeAssetDescs(assets []EpisodeImageAsset) []map[string]any {
	items := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		items = append(items, map[string]any{
			"category":      asset.Category,
			"name":          asset.Name,
			"character_ref": asset.CharacterRef,
			"prompt":        asset.Prompt,
		})
	}
	return items
}

func isCharacterCategory(category string) bool {
	return strings.Contains(category, "人物") || strings.Contains(category, "角色")
}

// baseCharacterName 去掉「名字（状态）」的括号后缀，取角色基本名。
func baseCharacterName(name string) string {
	for _, sep := range []string{"（", "(", "【"} {
		if idx := strings.Index(name, sep); idx > 0 {
			return strings.TrimSpace(name[:idx])
		}
	}
	return strings.TrimSpace(name)
}

/* ---------- 阶段 c：分镜 ---------- */

// generateStoryboard 本集剧本 → 分镜（qai steps/storyboard.ts），带上一集的
// 剧本与分镜连续性块。时长夹进 5~10 秒，画面人物截断到 3 人，speakers 只保留
// 本镜 characters 与旁白（qai parseSpeakers 同规则）。
func generateStoryboard(
	ctx workflow.Context,
	input EpisodeInput,
	script string,
) ([]storyboardShot, error) {
	prev1ScriptBlock, prev1StoryboardBlock := "", ""
	if len(input.Prev) > 0 && input.Prev[0].Script != "" {
		prevEpisode := input.Prev[0]
		prev1ScriptBlock = fmt.Sprintf("【前1集（第%d集）剧本全文】\n%s",
			prevEpisode.Number, prevEpisode.Script)
		if len(prevEpisode.Shots) > 0 {
			if raw, err := json.Marshal(previousStoryboard(prevEpisode.Shots)); err == nil {
				prev1StoryboardBlock = fmt.Sprintf("【前1集（第%d集）分镜描述】\n%s",
					prevEpisode.Number, string(raw))
			}
		}
	}

	var result storyboardResult
	if err := runTextgen(ctx, input.ChatModel,
		buildStoryboardPrompt(script, prev1ScriptBlock, prev1StoryboardBlock,
			input.StyleAnchor, input.MaxShots),
		storyboardSchema(), &result); err != nil {
		return nil, err
	}

	shots := make([]storyboardShot, 0, len(result.Shots))
	for _, shot := range result.Shots {
		shot.FirstFrame = strings.TrimSpace(shot.FirstFrame)
		shot.Storyboard = strings.TrimSpace(shot.Storyboard)
		if shot.FirstFrame == "" || shot.Storyboard == "" {
			continue
		}
		if shot.DurationS < minShotSeconds {
			shot.DurationS = minShotSeconds
		}
		if shot.DurationS > maxShotSeconds {
			shot.DurationS = maxShotSeconds
		}
		shot.Characters = cleanNames(shot.Characters, maxCharactersInShot)
		shot.Scenes = cleanNames(shot.Scenes, 0)
		shot.Props = cleanNames(shot.Props, 0)
		shot.Speakers = filterSpeakers(shot.Speakers, shot.Characters)
		shot.ShotID = fmt.Sprintf("%02d", len(shots)+1)
		shots = append(shots, shot)
		if len(shots) == input.MaxShots {
			break
		}
	}
	if len(shots) == 0 {
		return nil, fmt.Errorf("comic-drama: storyboard stage returned no usable shots")
	}
	return shots, nil
}

// previousStoryboard 把上一集分镜投影回模型可读的 qai snake_case 形状。
func previousStoryboard(shots []ShotAsset) []map[string]any {
	items := make([]map[string]any, 0, len(shots))
	for _, shot := range shots {
		items = append(items, map[string]any{
			"shot_id":     shot.ShotID,
			"duration_s":  shot.DurationSeconds,
			"characters":  shot.Characters,
			"scenes":      shot.Scenes,
			"props":       shot.Props,
			"first_frame": shot.FirstFrame,
			"storyboard":  shot.Storyboard,
			"speakers":    shot.Speakers,
		})
	}
	return items
}

func cleanNames(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
		if limit > 0 && len(names) == limit {
			break
		}
	}
	return names
}

// filterSpeakers 说话人只保留本镜画面人物与旁白，保持首次开口顺序
// （qai parseSpeakers：防止模型把"众人"、未登场角色写进来）。
func filterSpeakers(speakers, characters []string) []string {
	allowed := make(map[string]struct{}, len(characters)+1)
	for _, name := range characters {
		allowed[name] = struct{}{}
	}
	allowed[narratorName] = struct{}{}
	return func() []string {
		seen := make(map[string]struct{}, len(speakers))
		result := make([]string, 0, len(speakers))
		for _, value := range speakers {
			name := strings.TrimSpace(value)
			if name == "" {
				continue
			}
			if _, ok := allowed[name]; !ok {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
		return result
	}()
}

/* ---------- 阶段 d：资产图 ---------- */

// generateAssetImages 资产间并行出图（qai steps/asset-images.ts 三条路径）：
// ① 复用前集同名资产（直接引用 URL）② 人物状态 → edit_image（一致性前缀 +
// 定妆三视图参考）③ 场景 / 道具 → generate_image。
func generateAssetImages(
	ctx workflow.Context,
	input EpisodeInput,
	descs []assetDesc,
) []EpisodeImageAsset {
	portraitByName := portraitURLs(input.Characters)
	assets := make([]EpisodeImageAsset, len(descs))
	group := workflow.NewWaitGroup(ctx)
	for i, desc := range descs {
		i, desc := i, desc
		assets[i] = EpisodeImageAsset{
			Category:     desc.Category,
			Name:         desc.Name,
			CharacterRef: desc.CharacterRef,
		}

		// 路径 ①：复用前集资产。找不到就降级为重新生成（qai 同语义）。
		if desc.ReuseFromEp != nil && *desc.ReuseFromEp > 0 {
			assets[i].ReuseFromEp = *desc.ReuseFromEp
			if url := findReuseURL(input.Prev, *desc.ReuseFromEp, desc); url != "" {
				assets[i].ImageURL = url
				continue
			}
			assets[i].ReuseFromEp = 0
			if desc.Prompt == "" {
				assets[i].Error = "comic-drama: reusable asset not found and no prompt to regenerate"
				continue
			}
		}

		assets[i].Prompt = withVisualStyle(desc.Prompt, input.StyleAnchor)
		group.Add(1)
		workflow.Go(ctx, func(gctx workflow.Context) {
			defer group.Done()
			var url string
			var err error
			if isCharacterCategory(desc.Category) && portraitByName[desc.CharacterRef] != "" {
				// 路径 ②：人物状态图，参考定妆三视图锁身份。
				url, err = runEditImage(gctx, input.EditModel,
					consistencyPrefix+assets[i].Prompt,
					[]string{portraitByName[desc.CharacterRef]}, "")
			} else {
				// 路径 ③：场景 / 道具 / 无定妆人物，纯文生图。
				url, err = runGenerateImage(gctx, input.ImageModel, assets[i].Prompt, "")
			}
			if err != nil {
				assets[i].Error = err.Error()
				return
			}
			assets[i].ImageURL = url
			reportProgress(gctx, fmt.Sprintf("ep%d/asset/%d", input.Number, i),
				fmt.Sprintf("第 %d 集资产完成：%s", input.Number, assets[i].Name),
				mustJSON(map[string]string{"name": assets[i].Name, "imageUrl": url}))
		})
	}
	group.Wait(ctx)
	return assets
}

// findReuseURL 在前集产物里按集号 + 资产名精确匹配可复用的图。
func findReuseURL(prev []EpisodeContext, fromEp int, desc assetDesc) string {
	wanted := desc.Name
	if desc.ReuseAssetName != "" {
		wanted = strings.TrimSpace(desc.ReuseAssetName)
	}
	for _, episode := range prev {
		if episode.Number != fromEp {
			continue
		}
		for _, asset := range episode.Assets {
			if asset.Name == wanted && asset.ImageURL != "" {
				return asset.ImageURL
			}
		}
	}
	return ""
}

// portraitURLs 角色名 → 定妆三视图 URL。只有唯一变体时才可用
// （qai findCostume：多变体无法确定用哪张，宁缺毋滥）。
func portraitURLs(characters []CharacterAsset) map[string]string {
	urls := make(map[string]string, len(characters))
	conflicted := make(map[string]bool)
	for _, character := range characters {
		if character.ImageURL == "" || conflicted[character.Name] {
			continue
		}
		if _, dup := urls[character.Name]; dup {
			delete(urls, character.Name)
			conflicted[character.Name] = true
			continue
		}
		urls[character.Name] = character.ImageURL
	}
	return urls
}

/* ---------- 阶段 e+f：逐镜首帧 + 视频 ---------- */

// generateEpisodeShots 镜头间并行：每镜先出首帧（参考人物状态图 / 定妆 /
// 场景 / 道具，≤4 张），再以首帧提交视频。首帧或视频失败只记录在该镜的
// Error 上，不打断其他镜头。
func generateEpisodeShots(
	ctx workflow.Context,
	input EpisodeInput,
	episodeAssets []EpisodeImageAsset,
	shots []storyboardShot,
) []ShotAsset {
	portraitByName := portraitURLs(input.Characters)
	stateByCharacter := make(map[string]string)
	sceneByName := make(map[string]string)
	propByName := make(map[string]string)
	for _, asset := range episodeAssets {
		if asset.ImageURL == "" {
			continue
		}
		switch {
		case isCharacterCategory(asset.Category) && asset.CharacterRef != "":
			if _, dup := stateByCharacter[asset.CharacterRef]; !dup {
				stateByCharacter[asset.CharacterRef] = asset.ImageURL
			}
		case strings.Contains(asset.Category, "场景"):
			sceneByName[asset.Name] = asset.ImageURL
		case strings.Contains(asset.Category, "道具") || strings.Contains(asset.Category, "物件"):
			propByName[asset.Name] = asset.ImageURL
		}
	}

	assets := make([]ShotAsset, len(shots))
	group := workflow.NewWaitGroup(ctx)
	for i, shot := range shots {
		i, shot := i, shot
		refs, labels := frameReferences(shot, stateByCharacter, portraitByName, sceneByName, propByName)
		voiceSection := buildVoiceSection(shot.Speakers, input.Voices)
		assets[i] = ShotAsset{
			Index:           i,
			ShotID:          shot.ShotID,
			Characters:      shot.Characters,
			Scenes:          shot.Scenes,
			Props:           shot.Props,
			Speakers:        shot.Speakers,
			FirstFrame:      shot.FirstFrame,
			Storyboard:      shot.Storyboard,
			FramePrompt:     composeFramePrompt(shot.FirstFrame, labels, input.StyleAnchor, input.AspectRatio),
			VideoPrompt:     composeVideoPrompt(shot.Storyboard, buildVideoRefSection(labels), voiceSection, input.StyleAnchor),
			DurationSeconds: shot.DurationS,
		}
		group.Add(1)
		workflow.Go(ctx, func(gctx workflow.Context) {
			defer group.Done()
			var frameURL string
			var err error
			if len(refs) > 0 {
				frameURL, err = runEditImage(gctx, input.EditModel,
					assets[i].FramePrompt, refs, input.AspectRatio)
			} else {
				frameURL, err = runGenerateImage(gctx, input.ImageModel,
					assets[i].FramePrompt, input.AspectRatio)
			}
			if err != nil {
				assets[i].Error = "first frame: " + err.Error()
				return
			}
			assets[i].FrameURL = frameURL

			// 最多 3 次：吸收提交/轮询期的瞬时故障（重试经 heartbeat 续轮同一
			// 远端任务，不重复提交），持续性故障快速失败，不再拖满单集超时。
			// 单次执行上限与单集兜底超时一致：正常片段几分钟出结果，远端任务
			// 迟迟不给终态时不值得等更久。
			videoCtx := workflow.WithActivityOptions(gctx, workflow.ActivityOptions{
				TaskQueue:           coreTaskQueue(gctx),
				StartToCloseTimeout: 2 * time.Hour,
				HeartbeatTimeout:    time.Minute,
				RetryPolicy:         activityRetryPolicy(3),
				WaitForCancellation: true,
			})
			// 首帧 + 参考图一起交给可灵 Omni（qai video-clips 语义）：
			// 首帧定构图与画幅，refs 与 prompt 里的 @image_n 段一一对应。
			var clip klingOmniVideoOutput
			if err := workflow.ExecuteActivity(videoCtx, klingOmniVideoActivity, klingOmniVideoInput{
				Model:       input.VideoModel,
				Prompt:      assets[i].VideoPrompt,
				FirstFrame:  frameURL,
				ReferImages: refs,
				Resolution:  input.Resolution,
				Duration:    strconv.Itoa(assets[i].DurationSeconds),
				Audio:       input.Audio,
			}).Get(videoCtx, &clip); err != nil {
				assets[i].Error = "video: " + err.Error()
				return
			}
			videoURL := firstArtifactURI(clip.Artifacts)
			if videoURL == "" {
				assets[i].Error = "video: comic-drama: kling_omni_video returned no artifacts"
				return
			}
			assets[i].VideoURL = videoURL
			reportProgress(gctx, fmt.Sprintf("ep%d/shot/%d", input.Number, i),
				fmt.Sprintf("第 %d 集镜头 %s 完成", input.Number, shot.ShotID),
				mustJSON(map[string]string{"frameUrl": frameURL, "videoUrl": videoURL}))
		})
	}
	group.Wait(ctx)
	return assets
}

// frameReferences 为一镜收集首帧参考图（qai findRefImages，顺序即优先级）：
// 人物状态图（无则定妆三视图）→ 场景 → 道具，上限 maxFrameRefs，按 URL 去重。
// 一张都没有时退化为纯文生图，不注入可能串错的图片。
func frameReferences(
	shot storyboardShot,
	stateByCharacter, portraitByName, sceneByName, propByName map[string]string,
) (refs []string, labels []string) {
	seen := make(map[string]struct{}, maxFrameRefs)
	add := func(url, label string) {
		if url == "" || len(refs) >= maxFrameRefs {
			return
		}
		if _, dup := seen[url]; dup {
			return
		}
		seen[url] = struct{}{}
		refs = append(refs, url)
		labels = append(labels, label)
	}
	for _, name := range shot.Characters {
		if url := stateByCharacter[name]; url != "" {
			add(url, name+"融合了稳定身份、服装和当前状态的单人物参考图")
		} else {
			add(portraitByName[name], name+"的官方定妆三视图（同一人物的正面/侧面/背面并排）")
		}
	}
	for _, name := range shot.Scenes {
		add(sceneByName[name], name+"的场景参考图")
	}
	for _, name := range shot.Props {
		add(propByName[name], name+"的道具参考图")
	}
	return refs, labels
}

/* ---------- 生图 activity 的公共封装 ---------- */

func imageActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:              coreTaskQueue(ctx),
		StartToCloseTimeout:    10 * time.Minute,
		ScheduleToCloseTimeout: 30 * time.Minute,
		RetryPolicy:            activityRetryPolicy(2),
		WaitForCancellation:    true,
	})
}

func runGenerateImage(ctx workflow.Context, model, prompt, aspectRatio string) (string, error) {
	imageCtx := imageActivityOptions(ctx)
	var output generateImageOutput
	if err := workflow.ExecuteActivity(imageCtx, generateImageActivity, generateImageInput{
		Model:       model,
		Prompt:      prompt,
		AspectRatio: aspectRatio,
	}).Get(imageCtx, &output); err != nil {
		return "", err
	}
	if url := firstArtifactURI(output.Artifacts); url != "" {
		return url, nil
	}
	return "", fmt.Errorf("comic-drama: generate_image returned no artifacts")
}

func runEditImage(
	ctx workflow.Context,
	model, prompt string,
	referenceImages []string,
	aspectRatio string,
) (string, error) {
	imageCtx := imageActivityOptions(ctx)
	var output editImageOutput
	if err := workflow.ExecuteActivity(imageCtx, editImageActivity, editImageInput{
		Model:           model,
		Prompt:          prompt,
		ReferenceImages: referenceImages,
		AspectRatio:     aspectRatio,
	}).Get(imageCtx, &output); err != nil {
		return "", err
	}
	if url := firstArtifactURI(output.Artifacts); url != "" {
		return url, nil
	}
	return "", fmt.Errorf("comic-drama: edit_image returned no artifacts")
}
