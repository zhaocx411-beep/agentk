# 本地页面自动化测试

这套测试使用 Playwright 驱动真实 Chromium，并自动启动：

- `127.0.0.1:18080`：comic-drama Go API Server（agent 模式）
- `127.0.0.1:18081`：本地 Fake AgentK

不会请求真实 AgentK、模型或 Temporal，也不会进入生产镜像。

## 首次安装

```bash
cd e2e
npm install
npx playwright install chromium
```

## 运行

```bash
npm test
```

需要观察浏览器操作时：

```bash
npm run test:headed
```

需要逐步调试时：

```bash
npm run test:debug
```

## 查看失败结果

终端会直接列出失败用例。HTML 报告位于 `playwright-report/`：

```bash
npm run report
```

失败用例的截图、录像和 Trace 位于 `test-results/`。这些目录和
`node_modules/` 都已加入 `.gitignore`。
