import { expect, test } from "@playwright/test";
import { MarketplacePage } from "../pages/marketplace.page.js";

test.describe("Agent 集市", () => {
  test("搜索、分类和空状态可以直接操作", async ({ page }) => {
    const market = new MarketplacePage(page);
    await market.open();

    await expect(market.cards()).toHaveCount(9);
    await expect(page.locator("#agentCount")).toHaveText("9 个可用");

    await market.search("语音");
    await expect(market.cards()).toHaveCount(1);
    await expect(page.getByRole("heading", { name: "语音合成与识别" })).toBeVisible();

    await market.search("不存在的能力");
    await expect(market.cards()).toHaveCount(0);
    await expect(page.getByText("没有找到匹配的 Agent")).toBeVisible();

    await market.search("");
    await market.selectCategory("视觉创作");
    await expect(market.cards()).toHaveCount(2);

    await market.selectCategory("全部");
    await expect(market.cards()).toHaveCount(9);
  });

  test("每张 Agent 卡片都能打开有效页面", async ({ page }) => {
    const market = new MarketplacePage(page);
    await market.open();
    const names = await market.cards().locator(".agent-name").allTextContents();

    for (const name of names) {
      await test.step(name, async () => {
        const response = await market.openAgent(name);
        expect.soft(response?.status(), `${name} 的入口返回了错误状态`).toBeLessThan(400);
        await market.open();
      });
    }
  });
});
