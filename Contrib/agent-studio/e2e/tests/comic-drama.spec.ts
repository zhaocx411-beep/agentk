import { expect, test } from "@playwright/test";
import { WorkshopPage } from "../pages/workshop.page.js";

test.describe("漫剧工坊", () => {
  test("校验必填项，并允许选择和取消视觉风格", async ({ page }) => {
    const workshop = new WorkshopPage(page);
    await workshop.open();

    await expect(page.locator(".style-card")).toHaveCount(9);
    await workshop.selectStyle("新中式水墨风");
    await expect(page.locator(".style-card", { hasText: "新中式水墨风" }))
      .toHaveClass(/selected/);
    await workshop.selectStyle("新中式水墨风");
    await expect(page.locator(".style-card", { hasText: "新中式水墨风" }))
      .not.toHaveClass(/selected/);

    let createRequests = 0;
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().endsWith("/v1/dramas")) {
        createRequests++;
      }
    });
    await page.getByRole("button", { name: "开始创作" }).click();
    await expect(page.locator("#toast")).toHaveText("请先填写故事梗概或剧本");
    expect(createRequests).toBe(0);
  });

  test("创建漫剧、查看本地产物并继续对话", async ({ page }) => {
    const workshop = new WorkshopPage(page);
    const title = "E2E 本地漫剧";
    const brief = "一只猫发现家里的门会在午夜通往不同的城市。";
    await workshop.open();
    await workshop.selectStyle("新中式水墨风");

    const response = await workshop.create(title, brief);
    expect(response.status()).toBe(202);
    const requestBody = response.request().postDataJSON();
    expect(requestBody).toMatchObject({ title, brief });
    expect(requestBody.stylePrompt).toContain("新中式水墨风");

    await expect(page.locator("#dTitle")).toHaveText(title);
    await expect(page.locator("#dStatus")).toContainText("创作中");
    await expect(page.locator("#briefInput")).toHaveValue("");
    await expect(page.locator("#titleInput")).toHaveValue("");

    const created = await response.json();
    await expect.poll(async () => {
      const detail = await page.request.get(`/v1/dramas/${created.id}`);
      return (await detail.json()).status;
    }).toBe("idle");

    await workshop.reopenDrama(title);
    await expect(page.locator("#dStatus")).toContainText("已完成");
    await expect(page.locator("#dCounts")).toContainText("图片 1 · 视频 1 · 成片 1");

    // 默认视图是创作画布：血缘 DAG——顶部五个工序 chip，资产按列分布
    // （fake 里 generate_image 产出海报、compose_video 产出成片并引用
    // 海报作为输入，因此有一条 poster → film 的血缘连线）。
    await expect(page.locator("#dTabs")).toBeVisible();
    await expect(page.locator("#dCanvas .stage-chip")).toHaveCount(5);
    await expect(page.locator("#dCanvas .stage-chip.done")).toHaveCount(3);
    await expect(page.locator("#dCanvas .gnode img")).toHaveCount(1);
    await expect(page.locator("#dCanvas .gcol-film video")).toHaveCount(1);
    await expect(page.locator("#dCanvas .board-links path.link.done")).toHaveCount(1);

    // 切到 Agent 输出：完整的对话历史（用户指令 + 模型正文），不只最后一段。
    await page.locator("#tabOutput").click();
    await expect(page.locator("#dMsgs .msg.user")).toContainText(brief);
    await expect(page.locator("#dMsgs .msg.assistant").last())
      .toContainText("本地测试任务已完成。");
    await expect(page.locator("#dComposerCard")).toBeVisible();

    const messageResponse = await workshop.sendMessage("把下一集改成雨夜背景");
    expect(messageResponse.status()).toBe(202);
    await expect(page.locator("#toast")).toHaveText("已发送");
    await expect(page.locator("#msgInput")).toHaveValue("");
  });

  test("资产库汇总漫剧产物并提供下载", async ({ page }) => {
    const workshop = new WorkshopPage(page);
    const title = "E2E 资产库漫剧";
    await workshop.open();
    const response = await workshop.create(title, "验证资产库的漫剧任务。");
    expect(response.status()).toBe(202);
    const created = await response.json();
    await expect.poll(async () => {
      const detail = await page.request.get(`/v1/dramas/${created.id}`);
      return (await detail.json()).status;
    }).toBe("idle");

    await page.goto("/assets/");
    const project = page.locator(".project", { hasText: title });
    await expect(project).toBeVisible();
    await expect(project.locator(".group", { hasText: "图片" }).locator("img"))
      .toHaveCount(1);
    await expect(project.locator(".group", { hasText: "视频" }).locator("video"))
      .toHaveCount(1);

    // 下载代理只放行已登记的 URI。
    const downloadLink = project.locator('a[href^="/v1/assets/download"]').first();
    const href = await downloadLink.getAttribute("href");
    const download = await page.request.get(href!);
    expect(download.status()).toBe(200);
    expect(download.headers()["content-disposition"]).toContain("attachment");

    const rejected = await page.request.get(
      "/v1/assets/download?uri=" + encodeURIComponent("https://evil.example.com/x.png"));
    expect(rejected.status()).toBe(404);
  });

  test("标题留空时使用梗概前 24 个字符", async ({ page }) => {
    const workshop = new WorkshopPage(page);
    const brief = "这是一个用于验证中文标题自动截断逻辑的漫剧故事梗概，后面的内容不应进入标题";
    const expectedTitle = Array.from(brief).slice(0, 24).join("");
    await workshop.open();

    const response = await workshop.create("", brief);
    expect(response.status()).toBe(202);
    await expect(page.locator("#dTitle")).toHaveText(expectedTitle);
    await expect(page.locator(".drama-item", { hasText: expectedTitle })).toBeVisible();
  });
});
