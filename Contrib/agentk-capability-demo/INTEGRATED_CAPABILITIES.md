# 已接入 Capability

> 根据当前项目实现与 README 中的 E2E 记录整理。Capability Lab 动态读取 AgentK 的公开 Core catalog；“测试页已支持”表示表单、上传、校验和 SDK 参数透传已经覆盖，不等同于当前环境已完成真实 provider E2E。

| 名字 | 说明 | 状态 |
|---|---|---|
| `agentk.generate_text` | 文本生成，可按提示词生成、改写或总结文本 | 已经接入 |
| `agentk.web_search` | 联网搜索，返回带标题、链接和摘要的搜索结果 | 已经接入 |
| `agentk.read_file` | 读取并解析文档、表格和文本文件 | 已经接入 |
| `agentk.fetch_product_detail` | 提取淘宝、天猫和 Amazon 等商品详情 | 已经接入 |
| `agentk.generate_image` | 根据文字提示生成图片 | 已经接入 |
| `agentk.edit_image` | 根据提示词和参考图片编辑图片 | 已经接入 |
| `agentk.generate_video` | 通用文生视频或图生视频 | 已经接入 |
| `agentk.kling_text_to_video` | 可灵文生视频 | 已经接入 |
| `agentk.kling_image_to_video` | 可灵图生视频 | 已经接入 |
| `agentk.kling_omni_video` | 可灵 Omni 多参考素材视频生成 | 已经接入 |
| `agentk.create_voice_embedding` | 从真实人声音频提取自定义音色特征，供主体创建复用 | 测试页已支持；仅 PRT catalog 可见 |
| `agentk.create_kling_subject` | 从 1~3 张图片或一段视频创建可复用主体，可绑定自定义音色 | 测试页已支持 |
| `agentk.identify_faces` | 识别视频中的人脸并返回口型同步所需索引 | 测试页已支持 |
| `agentk.lip_sync_video` | 使用真实驱动音频为指定视频人脸生成口型同步结果 | 测试页已支持 |
| `agentk.seedance_generate_video` | Seedance 视频生成 | 已经接入 |
| `agentk.probe_video` | 探测视频编码、分辨率、帧率、时长和音轨等信息 | 已经接入 |
| `agentk.process_video` | 视频裁剪、转码及基础处理 | 已经接入 |
| `agentk.compose_video` | 多段视频、字幕和音轨合成 | 已经接入 |
| `agentk.extract_frame` | 从指定时间点提取视频画面 | 已经接入 |
| `agentk.extract_audio` | 从视频中提取音频 | 已经接入 |
| `agentk.understand_media` | 理解并分析图片、音频和视频等多媒体素材 | 已经接入 |
| `agentk.understand_video_segment` | 按指定时间区间分析视频内容 | 已经接入 |
| `agentk.transcribe_audio` | 音频语音识别与带时间戳转写 | 已经接入 |
| `agentk.generate_speech_qwen` | 使用 Qwen 进行语音合成 | 已经接入 |
| `agentk.generate_speech_minimax` | 使用 MiniMax 进行语音合成 | 已经接入 |
