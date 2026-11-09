import { expect, type Page } from "@playwright/test";

export class ChatPage {
  constructor(readonly page: Page) {}

  async open(agentID: string, agentName: string) {
    await this.page.goto(`/agents/${agentID}/chat/`);
    await expect(this.page.locator("#agentName")).toHaveText(agentName);
  }

  async send(text: string) {
    await this.page.locator("#messageInput").fill(text);
    const response = this.page.waitForResponse((candidate) =>
      candidate.request().method() === "POST" &&
      (/\/v1\/agents\/[^/]+\/chats$/.test(new URL(candidate.url()).pathname) ||
        /\/v1\/chats\/[^/]+\/messages$/.test(new URL(candidate.url()).pathname)),
    );
    await this.page.locator("#sendButton").click();
    return response;
  }

  async sendWithEnter(text: string) {
    await this.page.locator("#messageInput").fill(text);
    const response = this.page.waitForResponse((candidate) =>
      candidate.request().method() === "POST" &&
      /\/v1\/chats\/[^/]+\/messages$/.test(new URL(candidate.url()).pathname),
    );
    await this.page.locator("#messageInput").press("Enter");
    return response;
  }
}
