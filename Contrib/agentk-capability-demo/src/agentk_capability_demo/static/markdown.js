(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  if (root) root.AgentKMarkdown = api;
})(typeof window !== "undefined" ? window : globalThis, function () {
  "use strict";

  function escapeHtml(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function safeHref(value) {
    const href = String(value ?? "").trim();
    const sameOriginPath = href.startsWith("/") && !href.startsWith("//");
    if (/^(https?:|mailto:)/i.test(href) || sameOriginPath || href.startsWith("#")) {
      return escapeHtml(href);
    }
    return "#";
  }

  function renderInline(value) {
    const tokens = [];
    const stash = html => {
      tokens.push(html);
      return `\u0000TOKEN${tokens.length - 1}\u0000`;
    };
    let text = String(value ?? "").replaceAll("\u0000", "\uFFFD");
    text = text.replace(/`([^`\n]+)`/g, (_, body) => stash(`<code>${escapeHtml(body)}</code>`));
    text = text.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (_, label, href) => {
      const safeLabel = escapeHtml(label)
        .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
        .replace(/__([^_\n]+)__/g, "<strong>$1</strong>");
      return stash(`<a href="${safeHref(href)}" target="_blank" rel="noreferrer noopener">${safeLabel}</a>`);
    });
    text = escapeHtml(text);
    text = text
      .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
      .replace(/__([^_\n]+)__/g, "<strong>$1</strong>")
      .replace(/~~([^~\n]+)~~/g, "<del>$1</del>")
      .replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, "$1<em>$2</em>")
      .replace(/(^|[^_])_([^_\n]+)_(?!_)/g, "$1<em>$2</em>");
    return text.replace(/\u0000TOKEN(\d+)\u0000/g, (_, index) => tokens[Number(index)]);
  }

  function tableCells(line) {
    let value = line.trim();
    if (value.startsWith("|")) value = value.slice(1);
    if (value.endsWith("|")) value = value.slice(0, -1);
    return value.split("|").map(cell => cell.trim());
  }

  function isTableDivider(line) {
    const cells = tableCells(line);
    return cells.length > 1 && cells.every(cell => /^:?-{3,}:?$/.test(cell));
  }

  function startsBlock(lines, index) {
    const line = lines[index] || "";
    const next = lines[index + 1] || "";
    return /^\s*```/.test(line) || /^\s{0,3}#{1,6}\s+/.test(line) ||
      /^\s{0,3}>\s?/.test(line) || /^\s{0,3}([-+*])\s+/.test(line) ||
      /^\s{0,3}\d+[.)]\s+/.test(line) || /^\s{0,3}([-*_])(?:\s*\1){2,}\s*$/.test(line) ||
      (line.includes("|") && isTableDivider(next));
  }

  function renderMarkdown(value, depth = 0) {
    const lines = String(value ?? "").replace(/\r\n?/g, "\n").split("\n");
    const html = [];
    let index = 0;

    while (index < lines.length) {
      const line = lines[index];
      if (!line.trim()) { index += 1; continue; }

      const fence = line.match(/^\s*```\s*([\w-]*)\s*$/);
      if (fence) {
        const body = [];
        index += 1;
        while (index < lines.length && !/^\s*```\s*$/.test(lines[index])) body.push(lines[index++]);
        if (index < lines.length) index += 1;
        const language = fence[1] ? ` class="language-${escapeHtml(fence[1])}"` : "";
        html.push(`<pre><code${language}>${escapeHtml(body.join("\n"))}</code></pre>`);
        continue;
      }

      const heading = line.match(/^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
      if (heading) {
        const level = heading[1].length;
        html.push(`<h${level}>${renderInline(heading[2])}</h${level}>`);
        index += 1;
        continue;
      }

      if (/^\s{0,3}([-*_])(?:\s*\1){2,}\s*$/.test(line)) {
        html.push("<hr>"); index += 1; continue;
      }

      if (line.includes("|") && isTableDivider(lines[index + 1] || "")) {
        const headers = tableCells(line);
        const divider = tableCells(lines[index + 1]);
        const aligns = divider.map(cell => cell.startsWith(":") && cell.endsWith(":") ? "center" : cell.endsWith(":") ? "right" : cell.startsWith(":") ? "left" : "");
        index += 2;
        const rows = [];
        while (index < lines.length && lines[index].includes("|") && lines[index].trim()) rows.push(tableCells(lines[index++]));
        const headerHtml = headers.map((cell, i) => `<th${aligns[i] ? ` style="text-align:${aligns[i]}"` : ""}>${renderInline(cell)}</th>`).join("");
        const bodyHtml = rows.map(row => `<tr>${headers.map((_, i) => `<td${aligns[i] ? ` style="text-align:${aligns[i]}"` : ""}>${renderInline(row[i] || "")}</td>`).join("")}</tr>`).join("");
        html.push(`<div class="markdown-table-wrap"><table><thead><tr>${headerHtml}</tr></thead><tbody>${bodyHtml}</tbody></table></div>`);
        continue;
      }

      if (/^\s{0,3}>\s?/.test(line)) {
        const quoted = [];
        while (index < lines.length && /^\s{0,3}>\s?/.test(lines[index])) quoted.push(lines[index++].replace(/^\s{0,3}>\s?/, ""));
        const quoteBody = depth < 8
          ? renderMarkdown(quoted.join("\n"), depth + 1)
          : `<p>${quoted.map(renderInline).join("<br>")}</p>`;
        html.push(`<blockquote>${quoteBody}</blockquote>`);
        continue;
      }

      const listMatch = line.match(/^\s{0,3}([-+*]|\d+[.)])\s+(.+)$/);
      if (listMatch) {
        const ordered = /^\d/.test(listMatch[1]);
        const items = [];
        const pattern = ordered ? /^\s{0,3}\d+[.)]\s+(.+)$/ : /^\s{0,3}[-+*]\s+(.+)$/;
        while (index < lines.length) {
          const match = lines[index].match(pattern);
          if (!match) break;
          items.push(`<li>${renderInline(match[1])}</li>`); index += 1;
        }
        const tag = ordered ? "ol" : "ul";
        html.push(`<${tag}>${items.join("")}</${tag}>`);
        continue;
      }

      const paragraph = [line];
      index += 1;
      while (index < lines.length && lines[index].trim() && !startsBlock(lines, index)) paragraph.push(lines[index++]);
      html.push(`<p>${paragraph.map(renderInline).join("<br>")}</p>`);
    }

    return html.join("");
  }

  return { escapeHtml, renderMarkdown, safeHref };
});
