import { expect, test } from "@playwright/test";
import { ChatPage } from "../pages/chat.page.js";

test.describe("通用 Agent Chat", () => {
  test("所有已注册 Chat Agent 都能发送 hi 并收到回复", async ({ page }) => {
    const chat = new ChatPage(page);
    const agents = [
      ["creative-studio", "完整创作助手"],
      ["media-creator", "图文视频创作"],
      ["web-research", "电商信息爬取"],
      ["document-research", "文档解析与研究"],
      ["video-understanding", "视频分镜理解"],
      ["audio", "语音合成与识别"],
      ["video-editor", "视频剪辑与生成"],
    ] as const;

    for (const [id, name] of agents) {
      await test.step(name, async () => {
        await chat.open(id, name);
        await expect(page.locator("#starterGrid .starter")).toHaveCount(3);
        await expect(page.locator("#composer")).toBeVisible();
        const response = await chat.send("hi");
        expect(response.status()).toBe(201);
        await expect(page.locator(".message.user .message-text")).toHaveText("hi");
        await expect(page.getByText("已收到：hi", { exact: true })).toBeVisible();
        await expect(page.locator("#runState")).toContainText("等待输入");
      });
    }
  });

  test("Agent 回复完成前禁止再次发送消息", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("media-creator", "图文视频创作");
    const response = await chat.send("测试发送锁");
    expect(response.status()).toBe(201);

    await expect(page.locator("#messageInput")).toBeDisabled();
    await expect(page.locator("#sendButton")).toBeDisabled();
    await expect(page.locator("#fileInput")).toBeDisabled();

    await expect(page.getByText("本地测试任务已完成。", { exact: true })).toBeVisible();
    await expect(page.locator("#runState")).toContainText("等待输入");
    await expect(page.locator("#messageInput")).toBeEnabled();
    await expect(page.locator("#sendButton")).toBeEnabled();
    await expect(page.locator("#fileInput")).toBeEnabled();
  });

  test("创建会话、接收 SSE 富内容、继续发送并打开历史会话", async ({ page }) => {
    const chat = new ChatPage(page);
    const firstMessage = "生成一张本地测试海报";
    await chat.open("media-creator", "图文视频创作");

    const starter = page.locator("#starterGrid .starter").first();
    const starterText = (await starter.locator("span").textContent()) || "";
    await starter.click();
    await expect(page.locator("#messageInput")).toHaveValue(starterText);

    const createResponse = await chat.send(firstMessage);
    expect(createResponse.status()).toBe(201);
    await expect(page.locator(".message.user .message-text")).toContainText(firstMessage);
    await expect(page.locator("#runState")).toContainText("处理中");

    await expect(page.getByText(`已收到：${firstMessage}`, { exact: true })).toBeVisible();
    await expect(page.getByText("本地测试任务已完成。", { exact: true })).toBeVisible();
    await expect(page.locator(".tool", { hasText: "生成图片" })).toContainText("已完成");
    await expect(page.locator("#artifactSidebar")).toBeVisible();
    await expect(page.locator("#artifactCount")).toHaveText("2");
    await expect(page.locator("#artifactList .artifact img")).toHaveCount(1);
    await expect(page.locator("#artifactList .artifact video")).toHaveCount(1);
    await expect(page.locator("#messages .tool-artifacts .artifact video")).toHaveCount(1);
    await expect(page.locator("#runState")).toContainText("等待输入");
    await expect(page.locator(".session-item", { hasText: firstMessage })).toBeVisible();

    const continueResponse = await chat.sendWithEnter("把主色改成红色");
    expect(continueResponse.status()).toBe(202);
    await expect(page.locator(".message.user .message-text").last())
      .toContainText("把主色改成红色");
    await expect(page.getByText("已收到：把主色改成红色", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "新对话" }).click();
    await expect(page.locator("#welcome")).toBeVisible();
    await expect(page.locator("#chatTitle")).toHaveText("新对话");
    await expect(page.locator("#artifactCount")).toHaveText("0");

    await page.locator(".session-item", { hasText: firstMessage }).click();
    await expect(page.locator("#chatTitle")).toContainText(firstMessage);
    await expect(page.locator(".message.user .message-text").first()).toContainText(firstMessage);
    await expect(page.locator("#artifactCount")).toHaveText("4");
  });

  test("历史产物使用 url 字段时仍能正确预览", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("media-creator", "图文视频创作");
    const response = await chat.send("查找历史产物");
    expect(response.status()).toBe(201);

    const historyTool = page.locator(".tool", { hasText: "查找历史产物" });
    await expect(historyTool).toContainText("已完成");
    const image = historyTool.locator(".artifact img");
    await expect(image).toHaveAttribute("src", /\/assets\/poster\.svg$/);
    await expect(image).not.toHaveAttribute("src", /\/undefined$/);
    await expect(page.locator("#artifactCount")).toHaveText("3");
  });

  test("Assistant 消息中的 Markdown 表格会渲染为表格", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("media-creator", "图文视频创作");
    const response = await chat.send("展示 Markdown 表格");
    expect(response.status()).toBe(201);

    const table = page.locator(".message.assistant .markdown-table table");
    await expect(table.locator("thead th")).toHaveCount(3);
    await expect(table.locator("tbody tr")).toHaveCount(1);
    await expect(table.locator("tbody td")).toHaveText([
      "generate_image",
      "z-image-turbo",
      "小鸡吃米",
    ]);
  });

  test("ask_user 会显示选择卡并在提交后恢复 Agent", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("media-creator", "图文视频创作");
    const response = await chat.send("这个任务需要选择");
    expect(response.status()).toBe(201);

    const card = page.locator(".ask-card");
    await expect(card).toBeVisible();
    await expect(card.locator("fieldset")).toHaveCount(2);
    await expect(page.locator(".tool", { hasText: "请求用户选择" }))
      .toContainText("等待选择");
    await expect(page.locator("#messageInput")).toBeDisabled();
    await expect(card.getByRole("button", { name: "确认选择" })).toBeEnabled();

    await card.getByLabel("写实").check();
    await card.getByLabel("环境音").check();
    await card.getByLabel("鼓点").check();
    const answerResponse = page.waitForResponse((candidate) =>
      candidate.request().method() === "POST" &&
      /\/v1\/chats\/[^/]+\/answers$/.test(new URL(candidate.url()).pathname));
    await card.getByRole("button", { name: "确认选择" }).click();
    const answer = await answerResponse;
    expect(answer.status()).toBe(202);
    expect(answer.request().postDataJSON()).toMatchObject({
      answers: [
        { questionId: "q1", selected: ["写实"] },
        { questionId: "q2", selected: ["环境音", "鼓点"] },
      ],
    });

    await expect(card).toHaveCount(0);
    await expect(page.locator(".tool", { hasText: "请求用户选择" }))
      .toContainText("已完成");
    await expect(page.getByText("已收到你的选择，继续执行。", { exact: true }))
      .toBeVisible();
    await expect(page.locator("#runState")).toContainText("等待输入");
  });

  test("本地素材上传后可发送，切换会话时不会串用附件", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("media-creator", "图文视频创作");
    await chat.send("切换会话基线");
    await expect(page.locator(".session-item", { hasText: "切换会话基线" })).toBeVisible();
    await page.getByRole("button", { name: "新对话" }).click();

    const png = Buffer.from("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "binary");
    const firstUpload = page.waitForResponse((response) =>
      response.url().endsWith("/v1/uploads") && response.request().method() === "POST");
    await page.locator("#fileInput").setInputFiles({
      name: "draft.png",
      mimeType: "image/png",
      buffer: png,
    });
    expect((await firstUpload).status()).toBe(201);
    await expect(page.locator("#attachments")).toContainText("已上传");

    await page.locator(".session-item", { hasText: "切换会话基线" }).click();
    await expect(page.locator("#attachments")).toBeHidden();
    await page.getByRole("button", { name: "新对话" }).click();

    const secondUpload = page.waitForResponse((response) =>
      response.url().endsWith("/v1/uploads") && response.request().method() === "POST");
    await page.locator("#fileInput").setInputFiles({
      name: "send.png",
      mimeType: "image/png",
      buffer: png,
    });
    expect((await secondUpload).status()).toBe(201);

    const createChat = page.waitForResponse((response) =>
      /\/v1\/agents\/media-creator\/chats$/.test(new URL(response.url()).pathname) &&
      response.request().method() === "POST");
    await page.locator("#sendButton").click();
    const sent = await createChat;
    expect(sent.status()).toBe(201);
    const body = sent.request().postDataJSON();
    expect(body.message).toContain("send.png");
    expect(body.message).toContain("/assets/uploads/comic-drama/uploads/");
  });

  test("文档解析 Agent 可以上传并发送表格文件", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("document-research", "文档解析与研究");
    await expect(page.locator("#fileInput")).toHaveAttribute("accept", /\.pdf/);
    await expect(page.locator("#fileInput")).toHaveAttribute("accept", /\.xlsx/);

    const upload = page.waitForResponse((response) =>
      response.url().endsWith("/v1/uploads") && response.request().method() === "POST");
    await page.locator("#fileInput").setInputFiles({
      name: "metrics.csv",
      mimeType: "text/csv",
      buffer: Buffer.from("month,revenue\n7,100\n"),
    });
    const uploadResponse = await upload;
    expect(uploadResponse.status()).toBe(201);
    expect(await uploadResponse.json()).toMatchObject({
      type: "spreadsheet",
      mime: "text/csv; charset=utf-8",
    });

    const createChat = page.waitForResponse((response) =>
      /\/v1\/agents\/document-research\/chats$/.test(new URL(response.url()).pathname) &&
      response.request().method() === "POST");
    await page.locator("#sendButton").click();
    const sent = await createChat;
    expect(sent.status()).toBe(201);
    expect(sent.request().postDataJSON().message)
      .toContain("metrics.csv（spreadsheet）");
  });

  test("Markdown 标记前的裸链接仍会自动变成链接", async ({ page }) => {
    const chat = new ChatPage(page);
    await chat.open("media-creator", "图文视频创作");
    await chat.send("参考 https://example.com **已完成**");

    const reply = page.locator(".message.assistant .message-text").first();
    await expect(reply.getByRole("link", { name: "https://example.com" }))
      .toHaveAttribute("href", "https://example.com");
    await expect(reply.locator("strong")).toHaveText("已完成");
  });
});
