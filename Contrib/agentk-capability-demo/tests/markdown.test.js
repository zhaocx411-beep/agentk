const test = require("node:test");
const assert = require("node:assert/strict");

const { renderMarkdown, safeHref } = require("../src/agentk_capability_demo/static/markdown.js");

test("renders headings, emphasis, lists, and fenced code", () => {
  const html = renderMarkdown("## Title\n\n**bold** and `code`\n\n- one\n- two\n\n```js\nalert(1)\n```");
  assert.match(html, /<h2>Title<\/h2>/);
  assert.match(html, /<strong>bold<\/strong>/);
  assert.match(html, /<code>code<\/code>/);
  assert.match(html, /<ul><li>one<\/li><li>two<\/li><\/ul>/);
  assert.match(html, /<pre><code class="language-js">alert\(1\)<\/code><\/pre>/);
});

test("renders GitHub-style tables", () => {
  const html = renderMarkdown("| 项目 | 金额 |\n|---|---:|\n| **服务** | ¥5,000 |");
  assert.match(html, /<table>/);
  assert.match(html, /<th>项目<\/th>/);
  assert.match(html, /<th style="text-align:right">金额<\/th>/);
  assert.match(html, /<td><strong>服务<\/strong><\/td>/);
});

test("escapes raw HTML and rejects unsafe links", () => {
  const html = renderMarkdown('<img src=x onerror=alert(1)> [bad](javascript:alert(1)) [ok](https://example.com)');
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.equal(safeHref("javascript:alert(1)"), "#");
  assert.equal(safeHref("//example.com/path"), "#");
  assert.match(html, /href="https:\/\/example.com"/);
});

test("preserves safe URLs without double escaping query parameters", () => {
  const html = renderMarkdown("[search](https://example.com/?a=1&b=2)");
  assert.match(html, /href="https:\/\/example.com\/\?a=1&amp;b=2"/);
  assert.doesNotMatch(html, /&amp;amp;/);
});

test("bounds nested blockquote rendering", () => {
  const input = `${"> ".repeat(100)}safe <script>alert(1)</script>`;
  const html = renderMarkdown(input);
  assert.match(html, /safe/);
  assert.doesNotMatch(html, /<script>/);
});
