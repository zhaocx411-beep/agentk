import { expect, type Page } from "@playwright/test";

export class MarketplacePage {
  constructor(readonly page: Page) {}

  async open() {
    await this.page.goto("/");
    await expect(this.page.getByRole("heading", { name: "精选 Agent" })).toBeVisible();
  }

  cards() {
    return this.page.locator(".agent-card");
  }

  async search(value: string) {
    await this.page.getByRole("searchbox", { name: "搜索 Agent" }).fill(value);
  }

  async selectCategory(name: string) {
    await this.page.getByRole("button", { name, exact: true }).click();
  }

  async openAgent(name: string) {
    const navigation = this.page.waitForNavigation();
    await this.page.getByRole("link", { name: `打开${name}`, exact: true }).click();
    return navigation;
  }
}
