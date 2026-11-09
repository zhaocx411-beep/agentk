# 多 Timeline 版本改动与风险粗评

本次不实现。

不建议让一个 `TimelineEntityWorkflow` 同时持有多份大 Doc；更稳妥的方向是“一个 Timeline/分支一个 Workflow”，再新增项目级索引 Workflow 保存 `timelineId、名称、父版本、创建 revision、activeTimelineId`。HTTP 路径、渲染、Agent Tool 上下文、分享链接都需要显式携带 `timelineId`。

主要改动面包括：项目索引和迁移、Timeline fork/复制、每分支独立 revision、前端标签切换及 pending queue 隔离、agent-k Session 当前分支路由、render key、分享链接绑定版本，以及素材引用计数。最后一项风险最高：当前从单 Timeline 删除 media 会直接触发 Blob GC；多 Timeline 下必须先引入项目级媒体注册表/引用计数，否则一个分支删除素材会破坏其他分支。

还需明确“版本”是可继续编辑的分支，还是不可变快照；二者在合并、命名、删除和分享语义上差异很大。若只做分支创建/切换、不做合并，预计后端与前端合计约 3–5 周；加入分支合并、差异展示和可靠迁移后通常需要 6–10 周。主要风险是老项目迁移、跨 Workflow 的一致性、Agent 正在执行时切换分支，以及历史分享/渲染结果应固定到哪个 revision。
