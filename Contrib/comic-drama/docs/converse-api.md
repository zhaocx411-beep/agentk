# Converse 对话接口接入文档

一个无状态的 Chat 接口：**调用方持有完整对话记录**，每次请求把全部历史消息
连同用户最新输入发过来，服务端返回助手回复，调用方把回复追加进自己的记录。
接口支持真正的 SSE 文本增量流（推荐），并保留原 JSON 长轮询模式兼容旧客户端。
调用方不需要感知 AgentK Session、Execution 等服务端内部标识。

## 接口

```
POST {BASE}/v1/agents/assistant/converse
Content-Type: application/json
Accept: text/event-stream
```

- 测试环境 BASE：`https://comic-drama.test.gifshow.com`
- 路径里的 agent 固定用 `assistant`（统一对话入口，由模型路由到各能力）。

### 请求体

```json
{
  "conversationId": "cnv_xxx",
  "selectedAgent": "comic-drama",
  "stream": true,
  "messages": [
    { "role": "user",      "content": "第一句" },
    { "role": "assistant", "content": "上一轮的 reply 原文" },
    { "role": "user",      "content": "用户本次输入" }
  ]
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `conversationId` | 首轮可不传 | 首轮不传时服务端生成并在响应里返回，之后每轮必须带上。也可由调用方自己生成一个稳定的随机串。 |
| `messages` | 是 | 完整对话记录，按时间顺序。`role` 只能是 `user` / `assistant`，最后一条必须是 `user`（用户本次输入），`content` 不能为空白。 |
| `selectedAgent` | 否 | 用户指定的能力 ID（见下方列表）。不传 = 模型按意图自动路由。未知 ID 返回 400。 |
| `stream` | 否 | `true` 时返回 `text/event-stream`。也可只发送 `Accept: text/event-stream`。推荐开启。 |

可选 query 参数 `?wait=秒数` 只对非流式 JSON 模式生效：本次请求同步等待回复的
最长秒数（默认 25，上限 110）。SSE 模式会保持连接直到本轮收口，中间每 15 秒
发送一个注释心跳。

## SSE 流式响应（推荐）

请求体设置 `"stream": true`，或请求头设置 `Accept: text/event-stream`。响应头为：

```text
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache
X-Accel-Buffering: no
```

服务端依次发送以下事件：

### `meta`：连接建立

```text
event: meta
data: {"conversationId":"cnv_1a2b3c","status":"running","rebuilt":false}
```

首轮必须立即保存这里返回的 `conversationId`。

### `delta`：实时文本增量

```text
event: delta
data: {"roundId":"rnd_xxx","generation":"gen_xxx","index":12,"block":0,"text":"你好"}
```

`text` 是可以立刻展示的助手文本片段。服务端只转发正文，不会转发模型 reasoning。
`delta` 是临时预览，不要直接写入永久消息历史：Activity 重试时，同一个
`roundId + block` 可能出现新的 `generation`，这时应清空该块旧 generation 的预览；
同一 `generation + index` 应去重。

### `done`：本轮最终结果

```text
event: done
data: {"conversationId":"cnv_1a2b3c","status":"completed","reply":"助手回复全文","artifacts":[],"rebuilt":false}
```

`done` 的 data 与原 JSON 响应结构完全相同，并以持久化账本为准。收到后丢弃
delta 预览，用 `reply` 替换展示；`reply` 非空时逐字节原样追加进本地消息记录。

### `error`：流传输异常

```text
event: error
data: {"error":"AgentK stream ended before turn completed"}
```

这是传输异常，连接随后结束。不要追加 assistant 消息；携带相同
`conversationId` 和完全相同的 messages 重新请求即可。Agent 本轮自身失败则会以
`done` 事件返回 `status: "error"`，按下方状态机处理。

在 SSE 响应头发出前发生的参数错误、鉴权错误或 AgentK 创建失败，仍使用普通 HTTP
错误码和 JSON 错误体返回。

## JSON 响应（兼容模式）

不传 `stream` 且不接受 `text/event-stream` 时，接口保持原行为，一次返回 JSON：

```json
{
  "conversationId": "cnv_1a2b3c",
  "status": "completed",
  "reply": "助手回复全文",
  "artifacts": [ { "type": "image", "uri": "https://…/poster.png" } ],
  "pendingQuestions": []
}
```

| 字段 | 说明 |
|---|---|
| `status` | `completed` / `awaiting_user` / `running` / `error`，见下方状态机。 |
| `reply` | 本轮助手回复全文。 |
| `artifacts` | 本轮产出的媒体资产（图片、视频、音频的 URL），用于渲染。 |
| `pendingQuestions` | `status == "awaiting_user"` 时 agent 向用户提出的结构化问题（含可选项），可渲染成按钮。问题文本同时已包含在 `reply` 里，纯文本渲染不丢信息。 |
| `error` | `status == "error"` 时的错误信息。 |

响应里还有一个 `rebuilt` 字段，仅供服务端观测，调用方忽略即可。

## status 状态机

| status | 含义 | 调用方动作 |
|---|---|---|
| `completed` | 本轮结束 | `reply` 非空时追加 `{"role":"assistant","content":reply}` 到记录；渲染 `artifacts`。 |
| `awaiting_user` | agent 向用户提问 | 同上追加 `reply`；用户的回答作为下一条普通 `user` 消息发下一轮即可，无需特殊接口。 |
| `running` | 还在执行（只会出现在 JSON 兼容模式） | 不追加任何消息，间隔 2~5 秒把**完全相同的请求体**原样重发继续等。接口幂等，重发不会重复投递消息。 |
| `error` | 本轮失败 | 不追加任何消息；原样重发同一请求体即为重试，或提示用户稍后再试。 |

三条铁律（违反会导致对话上下文错乱、token 成本上升）：

1. **`reply` 逐字节原样保存**：不 trim、不改写换行、不截断。渲染时怎么处理
   都行，但存进 `messages` 的必须是原文；`reply` 为空串时不追加 assistant 消息。
2. **历史 append-only**：不修改、不删除、不重排已有消息，每轮只在末尾追加。
3. **同一 `conversationId` 的请求串行发送**：必须等本次 SSE 结束或 JSON 请求返回
   后再发下一轮，勿并发。

## 可用能力（selectedAgent 取值）

动态列表可从 `GET {BASE}/v1/agents` 获取（去掉 `assistant` 自身）。当前有：

| ID | 名称 | 能做什么 |
|---|---|---|
| `comic-drama` | 漫剧工坊 | 题材/梗概/剧本 → 人物定妆、分镜、视频生成、成片合成 |
| `creative-studio` | 完整创作助手 | 从想法出发的文本、图片、视频整套创作 |
| `media-creator` | 图文视频创作 | 图片生成编辑、视频制作处理、媒体理解 |
| `web-research` | 电商信息爬取 | 淘宝/天猫/Amazon 商品详情提取、品牌与市场检索 |
| `document-research` | 文档解析与研究 | PDF/Word/表格解析 + 联网核验 |
| `video-understanding` | 视频分镜理解 | 按时间轴分析视频内容、分镜、叙事 |
| `audio` | 语音合成与识别 | 文字转语音、语音转写、翻译 |
| `video-editor` | 视频剪辑与生成 | 视频生成、裁剪、合成、字幕、音频提取 |

`selectedAgent` 只影响本轮路由，不会写进对话历史；每轮想指定就带、不指定
就不带。不传时模型按意图自动路由，简单问答会直接回答、不转交。

## 调用示例

### 0. 推荐：SSE 流式调用

```bash
curl -N -X POST "$BASE/v1/agents/assistant/converse" \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d '{
    "stream": true,
    "messages": [
      {"role": "user", "content": "你好，介绍一下你自己"}
    ]
  }'
```

浏览器不能用只支持 GET 的 `EventSource` 发这个 POST 请求，应使用 `fetch` 读取
`response.body`。最小示例：

```js
const response = await fetch(`${BASE}/v1/agents/assistant/converse`, {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "Accept": "text/event-stream",
  },
  body: JSON.stringify({ stream: true, conversationId, messages }),
});

const reader = response.body.getReader();
const decoder = new TextDecoder();
let buffer = "";
for (;;) {
  const { value, done } = await reader.read();
  buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
  // 按空行切分 SSE packet，再读取其中的 event: 与 data: 行。
  // delta 只做预览；done.data.reply 才追加进 messages。
  if (done) break;
}
```

### 1. 自动路由（不指定能力）

```bash
curl -X POST "$BASE/v1/agents/assistant/converse" \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [
      {"role": "user", "content": "帮我搜一下最近 AI 视频生成领域的新动态"}
    ]
  }'
# → {"conversationId":"cnv_1a2b3c","status":"running"}
# 收到 running：带上返回的 conversationId，原样重发直到 completed
```

### 2. 用户指定能力：漫剧

```bash
curl -X POST "$BASE/v1/agents/assistant/converse" \
  -H 'Content-Type: application/json' \
  -d '{
    "selectedAgent": "comic-drama",
    "messages": [
      {"role": "user", "content": "把这个梗概做成一部一分钟的动态漫：少年在末日废墟中捡到一只会说话的机器猫……"}
    ]
  }'
# 成片类任务耗时长，会先返回 running，按状态机轮询即可；
# 中途 agent 可能返回 awaiting_user 向用户确认风格/集数。
```

### 3. 用户指定能力：图片生成

```bash
curl -X POST "$BASE/v1/agents/assistant/converse" \
  -H 'Content-Type: application/json' \
  -d '{
    "selectedAgent": "media-creator",
    "messages": [
      {"role": "user", "content": "生成一张赛博朋克风格的城市夜景海报，竖版"}
    ]
  }'
# completed 后 artifacts 里是图片 URL：
# {"status":"completed","reply":"…","artifacts":[{"type":"image","uri":"https://…"}]}
```

### 4. 多轮对话（带全量历史）

```bash
curl -X POST "$BASE/v1/agents/assistant/converse" \
  -H 'Content-Type: application/json' \
  -d '{
    "conversationId": "cnv_1a2b3c",
    "messages": [
      {"role": "user",      "content": "生成一张赛博朋克风格的城市夜景海报，竖版"},
      {"role": "assistant", "content": "…上一轮 reply 原文，逐字节保存…"},
      {"role": "user",      "content": "换成红色调，再加一行标题文字"}
    ]
  }'
```

### 5. JSON 兼容模式的长任务轮询

```bash
# 收到 running 后，用更长的 wait 减少往返；请求体与上一次完全相同
curl -X POST "$BASE/v1/agents/assistant/converse?wait=55" \
  -H 'Content-Type: application/json' \
  -d @same_request.json
```
