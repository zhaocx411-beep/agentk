package websearch

import (
	"context"
	"fmt"

	"agentk/internal/temporalx"
)

// ActivityName 是联网检索能力的公开 Temporal Activity 名（对外 ABI：
// agent 工具派发、Pipeline 与第三方 Workflow 按名直调）。
const ActivityName = "agentk.capability.web_search"

// CapabilityInput 是公开 payload，只包含检索业务参数。Model 是 models catalog
// 中的 model entry name，必须由调用方显式选择。
type CapabilityInput struct {
	Model string `json:"model"`
	// Query 检索词，必填
	Query string `json:"query"`
	// TimeRange 时效性档位（day/week/month/year）；空表示不限
	TimeRange string `json:"timeRange,omitempty"`
}

// CapabilityOutput 返回一页结果、实际选用的模型条目和生效的检索参数。
// Query 与 TimeRange 原样回带，让结果明确记录"实际搜索了什么"：
// 检索词被截断或档位没生效时，只看结果列表是看不出来的。
type CapabilityOutput struct {
	Results   []Result `json:"results"`
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	TimeRange string   `json:"timeRange,omitempty"`
}

// ModelLookup 按显式 model entry name 解析检索客户端。
type ModelLookup interface {
	SearchClient(name string) (Client, bool)
}

// Activity 实现联网检索 Activity：同步执行，没有任务 id，也不可轮询。
type Activity struct {
	models ModelLookup
}

func NewActivity(models ModelLookup) *Activity {
	return &Activity{models: models}
}

func (a *Activity) Run(ctx context.Context, in CapabilityInput) (CapabilityOutput, error) {
	const op = "capability web_search"
	if a.models == nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("websearch model lookup is required")))
	}
	if in.Model == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model is required"))
	}
	req := Request{Query: in.Query, TimeRange: in.TimeRange}
	// 入参校验放在调用供应商之前：检索词超长或档位非法属于模型可以修正的确定性
	// 错误；交给上游只会得到含糊的 10400，并额外重试三次。
	if err := validateRequest(req); err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%s", err.Error()))
	}
	client, ok := a.models.SearchClient(in.Model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("search capability %q is not available", in.Model)))
	}
	results, err := client.Search(ctx, req)
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": search", err)
	}
	if results == nil {
		results = []Result{} // 空结果序列化成 [] 而不是 null；模型对 null 的反应不稳定。
	}
	return CapabilityOutput{
		Results: results, Model: in.Model, Query: in.Query, TimeRange: in.TimeRange,
	}, nil
}
