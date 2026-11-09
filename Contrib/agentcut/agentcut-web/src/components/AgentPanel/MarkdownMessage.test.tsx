// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { MarkdownMessage } from "./MarkdownMessage";

afterEach(() => cleanup());

describe("MarkdownMessage", () => {
  it("renders common assistant Markdown and GFM structures", () => {
    render(
      <MarkdownMessage
        text={[
          "## 剪辑结果",
          "",
          "- **保留** 主镜头",
          "- 删除停顿",
          "",
          "| 轨道 | 状态 |",
          "| --- | --- |",
          "| V1 | 完成 |",
          "",
          "`30fps`",
        ].join("\n")}
      />,
    );

    expect(screen.getByRole("heading", { name: "剪辑结果" })).toBeTruthy();
    expect(screen.getByText("保留").tagName).toBe("STRONG");
    expect(screen.getByRole("table")).toBeTruthy();
    expect(screen.getByText("30fps").tagName).toBe("CODE");
  });

  it("does not execute or inject raw HTML from model output", () => {
    const { container } = render(
      <MarkdownMessage text={'<script>alert("x")</script>\n\n[危险链接](javascript:alert(1))'} />,
    );

    expect(container.querySelector("script")).toBeNull();
    expect(screen.getByText("危险链接").closest("a")?.getAttribute("href")).toBe("");
  });
});
