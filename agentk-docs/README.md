# AgentK Documentation

AgentK 的内部文档站，使用 Docusaurus 构建。

公开地址为 `https://agentk.staging.kuaishou.com/docs/`；生产环境对应
`https://agentk.corp.kuaishou.com/docs/`。AccessProxy 将公开路径原样转发给
Docs Service；`/mcp`、`/oauth/*` 和 `/.well-known/*` 保持产品级根路径。

## 本地运行

```bash
pnpm install
pnpm start
```

需要 Node.js 20 或更高版本。

```bash
AGENTK_DOCS_PUBLIC_URL=https://agentk.staging.kuaishou.com/docs/ pnpm build
```
