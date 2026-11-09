package capability

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/artifact"
	"agentk/internal/capability/asr"
	"agentk/internal/capability/audioextract"
	"agentk/internal/capability/brandad"
	"agentk/internal/capability/frameextract"
	"agentk/internal/capability/httpcall"
	"agentk/internal/capability/imagegen"
	"agentk/internal/capability/mediaunderstand"
	"agentk/internal/capability/readfile"
	"agentk/internal/capability/textgen"
	"agentk/internal/capability/tts"
	"agentk/internal/capability/videocompose"
	"agentk/internal/capability/videogen"
	"agentk/internal/capability/videoprobe"
	"agentk/internal/capability/videoprocess"
	"agentk/internal/capability/webfetch"
	"agentk/internal/capability/websearch"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

// ModelClients 按 model catalog entry name 返回能力客户端。
// 实现不得读取 Session Profile 或提供隐式默认值。
// *modelcatalog.Active 通过结构化类型满足此接口，使两个包保持无环依赖。
type ModelClients interface {
	videogen.ModelLookup
	imagegen.ModelLookup
	mediaunderstand.ModelLookup
	textgen.ModelLookup
	websearch.ModelLookup
	webfetch.ProductDetailLookup
	tts.ModelLookup
	asr.ModelLookup
}

// Tools 构造所有内置能力工具；Session Profile 决定哪些工具对模型可用。
// 机制层工具（list_artifacts、ask_user）由上层单独装配。
func Tools() ([]tool.Spec, error) {
	webFetch, err := webfetch.BuildFetchTool(webfetch.ConfigFromEnv())
	if err != nil {
		return nil, err
	}
	return []tool.Spec{webFetch}, nil
}

func fixedMediaTools() []tool.Spec {
	return []tool.Spec{
		videocompose.BuildComposeTool(),
		videoprobe.BuildTool(),
		videoprocess.BuildTool(),
		audioextract.BuildTool(),
		frameextract.BuildTool(),
		readfile.BuildTool(),
	}
}

// RegisterAll 注册所有内置能力的 Temporal 执行体。
func RegisterAll(w temporalx.Registry, modelClients ModelClients, storage artifact.Storage) {
	videoActivity := videogen.NewActivity(modelClients)
	w.RegisterActivityWithOptions(videoActivity.Run,
		activity.RegisterOptions{Name: videogen.ActivityName})
	w.RegisterActivityWithOptions(videoActivity.ExecuteKlingT2V,
		activity.RegisterOptions{Name: videogen.KlingT2VExecutionActivityName})
	w.RegisterActivityWithOptions(videoActivity.ExecuteKlingI2V,
		activity.RegisterOptions{Name: videogen.KlingI2VExecutionActivityName})
	w.RegisterActivityWithOptions(videoActivity.ExecuteKlingOmni,
		activity.RegisterOptions{Name: videogen.KlingOmniExecutionActivityName})
	w.RegisterActivityWithOptions(videoActivity.ExecuteSeedance,
		activity.RegisterOptions{Name: videogen.SeedanceExecutionActivityName})
	w.RegisterWorkflowWithOptions(videogen.KlingT2VWorkflow,
		workflow.RegisterOptions{Name: videogen.KlingT2VWorkflowType})
	w.RegisterWorkflowWithOptions(videogen.KlingI2VWorkflow,
		workflow.RegisterOptions{Name: videogen.KlingI2VWorkflowType})
	w.RegisterWorkflowWithOptions(videogen.KlingOmniWorkflow,
		workflow.RegisterOptions{Name: videogen.KlingOmniWorkflowType})
	w.RegisterWorkflowWithOptions(videogen.SeedanceWorkflow,
		workflow.RegisterOptions{Name: videogen.SeedanceWorkflowType})
	imageActivity := imagegen.NewActivity(modelClients, storage)
	w.RegisterActivityWithOptions(imageActivity.Run,
		activity.RegisterOptions{Name: imagegen.ActivityName})
	w.RegisterActivityWithOptions(imageActivity.RunEdit,
		activity.RegisterOptions{Name: imagegen.EditActivityName})
	understandActivity := mediaunderstand.NewActivity(modelClients)
	w.RegisterActivityWithOptions(understandActivity.Run,
		activity.RegisterOptions{Name: mediaunderstand.ActivityName})
	w.RegisterActivityWithOptions(understandActivity.RunSegment,
		activity.RegisterOptions{Name: mediaunderstand.SegmentActivityName})
	w.RegisterActivityWithOptions(websearch.NewActivity(modelClients).Run,
		activity.RegisterOptions{Name: websearch.ActivityName})
	w.RegisterActivityWithOptions(webfetch.NewProductDetailActivity(modelClients).Run,
		activity.RegisterOptions{Name: webfetch.ProductDetailActivityName})
	w.RegisterActivityWithOptions(videocompose.NewActivity(storage).Run,
		activity.RegisterOptions{Name: videocompose.ActivityName})
	w.RegisterActivityWithOptions(videocompose.NewComposeActivity(storage).Run,
		activity.RegisterOptions{Name: videocompose.ComposeActivityName})
	w.RegisterActivityWithOptions(videoprocess.NewActivity(storage).Run,
		activity.RegisterOptions{Name: videoprocess.ActivityName})
	w.RegisterActivityWithOptions(audioextract.NewActivity(storage).Run,
		activity.RegisterOptions{Name: audioextract.ActivityName})
	w.RegisterActivityWithOptions(frameextract.NewActivity(storage).Run,
		activity.RegisterOptions{Name: frameextract.ActivityName})
	w.RegisterActivityWithOptions(videoprobe.NewActivity(storage).Run,
		activity.RegisterOptions{Name: videoprobe.ActivityName})
	w.RegisterActivityWithOptions(textgen.NewActivity(modelClients).Run,
		activity.RegisterOptions{Name: textgen.ActivityName})
	ttsActivity := tts.NewActivity(modelClients, storage)
	w.RegisterActivityWithOptions(ttsActivity.RunMiniMax,
		activity.RegisterOptions{Name: tts.MiniMaxActivityName})
	w.RegisterActivityWithOptions(ttsActivity.RunQwen,
		activity.RegisterOptions{Name: tts.QwenActivityName})
	w.RegisterActivityWithOptions(ttsActivity.RunCosyVoice,
		activity.RegisterOptions{Name: tts.CosyVoiceActivityName})
	w.RegisterActivityWithOptions(asr.NewActivity(modelClients).Run,
		activity.RegisterOptions{Name: asr.ActivityName})
	w.RegisterActivityWithOptions(readfile.NewActivity(modelClients, storage).Run,
		activity.RegisterOptions{Name: readfile.ActivityName})
	httpActivity := httpcall.NewActivity(nil)
	w.RegisterActivityWithOptions(httpActivity.Call,
		activity.RegisterOptions{Name: httpcall.CallActivityName})
	w.RegisterActivityWithOptions(httpActivity.Submit,
		activity.RegisterOptions{Name: httpcall.SubmitActivityName})
	// Workflow 和 activity 形式的能力统一在此注册。
	w.RegisterWorkflowWithOptions(brandad.Workflow,
		workflow.RegisterOptions{Name: brandad.WorkflowType})
}
