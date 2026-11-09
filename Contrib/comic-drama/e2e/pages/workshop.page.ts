import { expect, type Page } from "@playwright/test";

export class WorkshopPage {
  constructor(readonly page: Page) {}

  async open() {
    await this.page.goto("/agents/comic-drama/");
    await expect(this.page.getByRole("heading", { name: /把一个想法.*漫剧/ }))
      .toBeVisible();
  }

  async selectStyle(name: string) {
    await this.page.locator(".style-card", { hasText: name }).click();
  }

  async create(title: string, brief: string) {
    await this.page.locator("#titleInput").fill(title);
    await this.page.locator("#briefInput").fill(brief);
    const response = this.page.waitForResponse((candidate) =>
      candidate.url().endsWith("/v1/dramas") && candidate.request().method() === "POST",
    );
    await this.page.getByRole("button", { name: "开始创作" }).click();
    return response;
  }

  async reopenDrama(title: string) {
    await this.page.getByRole("button", { name: "新建漫剧" }).click();
    await this.page.locator(".drama-item", { hasText: title }).click();
  }

  async sendMessage(text: string) {
    await this.page.locator("#msgInput").fill(text);
    const response = this.page.waitForResponse((candidate) =>
      /\/v1\/dramas\/[^/]+\/messages$/.test(new URL(candidate.url()).pathname) &&
      candidate.request().method() === "POST",
    );
    await this.page.locator("#msgBtn").click();
    return response;
  }
}
