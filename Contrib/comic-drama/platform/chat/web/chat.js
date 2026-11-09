(() => {
  "use strict";

  const state = {
    agent: null,
    chats: [],
    current: null,
    stream: null,
    sending: false,
    uploading: false,
    uploads: [],
    answeringAsks: new Set(),
  };

  const $ = (id) => document.getElementById(id);
  const agentID = parseAgentID();

  function parseAgentID() {
    const match = location.pathname.match(/^\/agents\/([^/]+)\/chat\/?$/);
    return match ? decodeURIComponent(match[1]) : "";
  }

  async function api(path, options) {
    const response = await fetch(path, options);
    let body = null;
    try { body = await response.json(); } catch { /* non-json response */ }
    if (!response.ok) {
      const error = new Error((body && body.error) || `请求失败（${response.status}）`);
      error.status = response.status;
      throw error;
    }
    return body;
  }

  async function initialize() {
    try {
      const catalog = await api("/v1/agents");
      state.agent = (catalog.agents || []).find((agent) => agent.id === agentID);
      if (!state.agent) {
        showNotFound();
        return;
      }
      applyAgent();
      await loadChats();
      bindEvents();
      $("messageInput").focus();
    } catch (error) {
      toast("页面初始化失败：" + error.message);
      $("welcomeTitle").textContent = "暂时无法连接服务";
      $("welcomeMessage").textContent = error.message;
    }
  }

  function applyAgent() {
    document.title = state.agent.name + " · Agent 集市";
    document.documentElement.style.setProperty("--accent", state.agent.accent);
    $("agentMark").textContent = state.agent.icon;
    $("agentName").textContent = state.agent.name;
    $("agentDescription").textContent = state.agent.description;
    $("agentCategory").textContent = state.agent.category.toUpperCase();
    $("welcomeTitle").textContent = "今天想完成什么？";
    $("welcomeMessage").textContent = state.agent.welcomeMessage;

    const grid = $("starterGrid");
    grid.replaceChildren();
    (state.agent.starterPrompts || []).forEach((prompt, index) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "starter";
      const number = document.createElement("b");
      number.textContent = "0" + (index + 1);
      const text = document.createElement("span");
      text.textContent = prompt;
      button.append(number, text);
      button.addEventListener("click", () => {
        $("messageInput").value = prompt;
        resizeComposer();
        $("messageInput").focus();
      });
      grid.appendChild(button);
    });
  }

  function showNotFound() {
    document.title = "Agent 不存在";
    $("agentName").textContent = "未知 Agent";
    $("welcomeTitle").textContent = "这个 Agent 不在集市中";
    $("welcomeMessage").textContent = "请返回 Agent 集市，选择一个当前可用的入口。";
    $("starterGrid").replaceChildren();
    $("composer").hidden = true;
  }

  async function loadChats() {
    const response = await api("/v1/chats");
    state.chats = (response.chats || []).filter((chat) => chat.agentId === agentID);
    renderSessions();
  }

  function renderSessions() {
    const list = $("sessionList");
    list.replaceChildren();
    if (!state.chats.length) {
      const empty = document.createElement("div");
      empty.className = "session-empty";
      empty.append("还没有会话。", document.createElement("br"), "从一个问题开始。");
      list.appendChild(empty);
      return;
    }
    state.chats.forEach((chat) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "session-item" + (state.current && state.current.id === chat.id ? " active" : "");
      const title = document.createElement("span");
      title.className = "session-title";
      title.textContent = chat.title;
      const meta = document.createElement("span");
      meta.className = "session-meta";
      const status = document.createElement("span");
      status.textContent = statusLabel(chat.status);
      const time = document.createElement("span");
      time.textContent = formatRelative(chat.updatedAt);
      meta.append(status, time);
      button.append(title, meta);
      button.addEventListener("click", () => openChat(chat.id));
      list.appendChild(button);
    });
  }

  async function openChat(chatID) {
    try {
      const chat = await api("/v1/chats/" + encodeURIComponent(chatID));
      if (chat.agentId !== agentID) {
        throw new Error("这个会话属于另一个 Agent");
      }
      if (!state.current || state.current.id !== chat.id) {
        state.uploads = [];
        renderUploads();
      }
      state.current = chat;
      renderCurrent();
      connectStream();
      renderSessions();
      closeSidebar();
    } catch (error) {
      toast("打开会话失败：" + error.message);
    }
  }

  function startNewChat() {
    closeStream();
    state.current = null;
    state.uploads = [];
    renderUploads();
    $("chatTitle").textContent = "新对话";
    $("welcome").hidden = false;
    $("messages").replaceChildren();
    renderArtifacts();
    setRunState("", "等待输入");
    renderSessions();
    refreshComposerControls();
    closeSidebar();
    $("messageInput").focus();
  }

  function renderCurrent() {
    if (!state.current) return;
    $("welcome").hidden = true;
    $("chatTitle").textContent = state.current.title;
    renderMessages();
    renderArtifacts();
    setRunState(state.current.status, statusLabel(state.current.status));
    refreshComposerControls();
  }

  function renderMessages() {
    const box = $("messages");
    const wasNearBottom = $("conversation").scrollHeight - $("conversation").scrollTop -
      $("conversation").clientHeight < 180;
    box.replaceChildren();
    (state.current.messages || []).forEach((message) => {
      box.appendChild(renderMessage(message));
    });
    if (state.current.error) {
      const error = document.createElement("div");
      error.className = "error-note";
      error.textContent = state.current.error;
      box.appendChild(error);
    }
    if (wasNearBottom || state.sending) {
      requestAnimationFrame(() => {
        $("conversation").scrollTop = $("conversation").scrollHeight;
      });
    }
  }

  function renderMessage(message) {
    const article = document.createElement("article");
    article.className = "message " + (message.role === "user" ? "user" : "assistant");

    const avatar = document.createElement("div");
    avatar.className = "avatar";
    avatar.textContent = message.role === "user" ? "你" : state.agent.icon;

    const main = document.createElement("div");
    main.className = "message-main";
    const meta = document.createElement("div");
    meta.className = "message-meta";
    const author = document.createElement("span");
    author.textContent = message.role === "user" ? "YOU / 用户" : state.agent.name;
    const time = document.createElement("span");
    time.textContent = formatTime(message.createdAt);
    meta.append(author, time);
    main.appendChild(meta);

    if (message.reasoning) {
      const reasoning = document.createElement("details");
      reasoning.className = "reasoning";
      const summary = document.createElement("summary");
      summary.textContent = "查看思考过程";
      const pre = document.createElement("pre");
      pre.textContent = message.reasoning;
      reasoning.append(summary, pre);
      main.appendChild(reasoning);
    }

    const text = document.createElement("div");
    text.className = "message-text";
    appendRichText(text, message.text || (message.role === "assistant" ? " " : ""));
    if (message.status === "streaming") {
      const caret = document.createElement("i");
      caret.className = "stream-caret";
      text.appendChild(caret);
    }
    main.appendChild(text);

    if (message.tools && message.tools.length) {
      const tools = document.createElement("div");
      tools.className = "tools";
      message.tools.forEach((tool) => tools.appendChild(renderTool(tool)));
      main.appendChild(tools);
    }
    article.append(avatar, main);
    return article;
  }

  function appendRichText(container, value) {
    const lines = String(value).split("\n");
    let paragraph = [];
    let list = null;
    let code = null;
    const flushParagraph = () => {
      if (!paragraph.length) return;
      const p = document.createElement("p");
      paragraph.forEach((line, index) => {
        appendInlineMarkdown(p, line);
        if (index < paragraph.length - 1) p.appendChild(document.createElement("br"));
      });
      container.appendChild(p);
      paragraph = [];
    };
    const flushList = () => {
      if (list) container.appendChild(list);
      list = null;
    };
    for (let index = 0; index < lines.length; index += 1) {
      const line = lines[index];
      if (code) {
        if (/^\s*```/.test(line)) {
          container.appendChild(code);
          code = null;
        } else {
          code.querySelector("code").textContent +=
            (code.querySelector("code").textContent ? "\n" : "") + line;
        }
        continue;
      }
      if (/^\s*```/.test(line)) {
        flushParagraph();
        flushList();
        code = document.createElement("pre");
        code.appendChild(document.createElement("code"));
        continue;
      }
      if (!line.trim()) {
        flushParagraph();
        flushList();
        continue;
      }
      if (line.includes("|") && isTableDivider(lines[index + 1])) {
        flushParagraph();
        flushList();
        const headers = tableCells(line);
        const dividers = tableCells(lines[index + 1]);
        const wrapper = document.createElement("div");
        wrapper.className = "markdown-table";
        const table = document.createElement("table");
        const head = document.createElement("thead");
        const headRow = document.createElement("tr");
        headers.forEach((header, column) => {
          const cell = document.createElement("th");
          cell.style.textAlign = tableAlignment(dividers[column]);
          appendInlineMarkdown(cell, header);
          headRow.appendChild(cell);
        });
        head.appendChild(headRow);
        table.appendChild(head);
        const body = document.createElement("tbody");
        index += 2;
        while (index < lines.length && lines[index].includes("|") && lines[index].trim()) {
          const row = document.createElement("tr");
          const cells = tableCells(lines[index]);
          headers.forEach((_, column) => {
            const cell = document.createElement("td");
            cell.style.textAlign = tableAlignment(dividers[column]);
            appendInlineMarkdown(cell, cells[column] || "");
            row.appendChild(cell);
          });
          body.appendChild(row);
          index += 1;
        }
        table.appendChild(body);
        wrapper.appendChild(table);
        container.appendChild(wrapper);
        index -= 1;
        continue;
      }
      const heading = line.match(/^(#{1,4})\s+(.+)$/);
      if (heading) {
        flushParagraph();
        flushList();
        const element = document.createElement("h" + heading[1].length);
        appendInlineMarkdown(element, heading[2]);
        container.appendChild(element);
        continue;
      }
      const item = line.match(/^\s*(?:[-*+]|\d+[.)])\s+(.+)$/);
      if (item) {
        flushParagraph();
        if (!list) list = document.createElement(/^\s*\d/.test(line) ? "ol" : "ul");
        const li = document.createElement("li");
        appendInlineMarkdown(li, item[1]);
        list.appendChild(li);
        continue;
      }
      if (/^\s*>\s?/.test(line)) {
        flushParagraph();
        flushList();
        const quote = document.createElement("blockquote");
        appendInlineMarkdown(quote, line.replace(/^\s*>\s?/, ""));
        container.appendChild(quote);
        continue;
      }
      if (/^\s*(?:---+|\*\*\*+)\s*$/.test(line)) {
        flushParagraph();
        flushList();
        container.appendChild(document.createElement("hr"));
        continue;
      }
      flushList();
      paragraph.push(line);
    }
    if (code) container.appendChild(code);
    flushParagraph();
    flushList();
  }

  function isTableDivider(line) {
    if (typeof line !== "string") return false;
    const cells = tableCells(line);
    return cells.length > 0 && cells.every((cell) => /^:?-{3,}:?$/.test(cell));
  }

  function tableCells(line) {
    const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
    return trimmed.split("|").map((cell) => cell.trim());
  }

  function tableAlignment(divider) {
    if (/^:-+:$/.test(divider || "")) return "center";
    if (/-+:$/.test(divider || "")) return "right";
    return "left";
  }

  function appendInlineMarkdown(parent, text) {
    const pattern = /!\[([^\]]*)\]\((https?:\/\/[^)\s]+)\)|\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)|`([^`]+)`|\*\*([^*]+)\*\*|~~([^~]+)~~/g;
    let cursor = 0;
    for (const match of text.matchAll(pattern)) {
      appendAutoLinkedText(parent, text.slice(cursor, match.index));
      if (match[1] !== undefined) {
        const url = safeURL(match[2]);
        if (url) {
          const figure = document.createElement("figure");
          figure.className = "markdown-image";
          const image = document.createElement("img");
          image.src = url;
          image.alt = match[1] || "Agent 生成的图片";
          image.loading = "lazy";
          figure.appendChild(image);
          if (match[1]) {
            const caption = document.createElement("figcaption");
            caption.textContent = match[1];
            figure.appendChild(caption);
          }
          parent.appendChild(figure);
        } else {
          parent.append(document.createTextNode(match[0]));
        }
      } else if (match[3] !== undefined) {
        const link = document.createElement("a");
        link.href = safeURL(match[4]);
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        link.textContent = match[3];
        parent.appendChild(link);
      } else if (match[5] !== undefined) {
        const code = document.createElement("code");
        code.textContent = match[5];
        parent.appendChild(code);
      } else if (match[6] !== undefined) {
        const strong = document.createElement("strong");
        strong.textContent = match[6];
        parent.appendChild(strong);
      } else {
        const deleted = document.createElement("del");
        deleted.textContent = match[7];
        parent.appendChild(deleted);
      }
      cursor = match.index + match[0].length;
    }
    appendAutoLinkedText(parent, text.slice(cursor));
  }

  function appendAutoLinkedText(parent, text) {
    const rawURLPattern = /https?:\/\/[^\s<>"')\]]+/g;
    let rawCursor = 0;
    for (const match of text.matchAll(rawURLPattern)) {
      parent.append(document.createTextNode(text.slice(rawCursor, match.index)));
      const link = document.createElement("a");
      link.href = match[0];
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = match[0];
      parent.appendChild(link);
      rawCursor = match.index + match[0].length;
    }
    parent.append(document.createTextNode(text.slice(rawCursor)));
  }

  function renderTool(tool) {
    const details = document.createElement("details");
    details.className = "tool " + tool.status;
    const artifacts = toolResultArtifacts(tool.result);
    const pendingAsk = (state.current.pendingAsks || [])
      .find((ask) => ask.toolCallId === tool.callId);
    details.open = artifacts.length > 0 || Boolean(pendingAsk);
    const summary = document.createElement("summary");
    const dot = document.createElement("i");
    dot.className = "tool-dot";
    const name = document.createElement("span");
    name.className = "tool-name";
    name.textContent = humanToolName(tool.name);
    const status = document.createElement("span");
    status.className = "tool-status";
    status.textContent = pendingAsk ? "等待选择" : toolStatusLabel(tool.status);
    summary.append(dot, name, status);
    details.appendChild(summary);

    if (artifacts.length) {
      const previews = document.createElement("div");
      previews.className = "tool-artifacts";
      artifacts.forEach((artifact) => {
        const card = renderArtifact(artifact);
        if (card) previews.appendChild(card);
      });
      details.appendChild(previews);
    }
    if (pendingAsk) details.appendChild(renderAskCard(pendingAsk));
    const payload = tool.error || prettyJSON(tool.result) || prettyJSON(tool.arguments);
    if (payload) {
      const raw = document.createElement("details");
      raw.className = "tool-payload";
      const rawSummary = document.createElement("summary");
      rawSummary.textContent = tool.error ? "查看错误详情" : "查看调用详情";
      const pre = document.createElement("pre");
      pre.textContent = payload;
      raw.append(rawSummary, pre);
      details.appendChild(raw);
    }
    return details;
  }

  function renderAskCard(ask) {
    const form = document.createElement("form");
    form.className = "ask-card";
    const busy = state.answeringAsks.has(ask.toolCallId);
    (ask.questions || []).forEach((question, questionIndex) => {
      const fieldset = document.createElement("fieldset");
      const legend = document.createElement("legend");
      if (question.label) {
        const label = document.createElement("span");
        label.className = "ask-label";
        label.textContent = question.label;
        legend.appendChild(label);
      }
      legend.append(document.createTextNode(question.question));
      fieldset.appendChild(legend);

      const options = document.createElement("div");
      options.className = "ask-options";
      (question.options || []).forEach((option, optionIndex) => {
        const choice = document.createElement("label");
        choice.className = "ask-option";
        const input = document.createElement("input");
        input.type = question.multiSelect ? "checkbox" : "radio";
        input.name = `ask-${ask.toolCallId}-${question.id}`;
        input.value = option;
        input.disabled = busy;
        input.id = `ask-${ask.toolCallId}-${questionIndex}-${optionIndex}`;
        const text = document.createElement("span");
        text.textContent = option;
        choice.append(input, text);
        options.appendChild(choice);
      });
      fieldset.appendChild(options);

      const custom = document.createElement("input");
      custom.type = "text";
      custom.className = "ask-custom";
      custom.placeholder = "其他答案（可填写）";
      custom.setAttribute("aria-label", `${question.question}的其他答案`);
      custom.dataset.questionId = question.id;
      custom.disabled = busy;
      fieldset.appendChild(custom);
      form.appendChild(fieldset);
    });

    const error = document.createElement("p");
    error.className = "ask-error";
    error.hidden = true;
    const submit = document.createElement("button");
    submit.type = "submit";
    submit.className = "ask-submit";
    submit.disabled = busy;
    submit.textContent = busy ? "正在提交…" : "确认选择";
    form.append(error, submit);
    form.addEventListener("submit", (event) => submitAskAnswers(event, ask));
    return form;
  }

  async function submitAskAnswers(event, ask) {
    event.preventDefault();
    const form = event.currentTarget;
    const answers = [];
    for (const question of ask.questions || []) {
      const escapedName = CSS.escape(`ask-${ask.toolCallId}-${question.id}`);
      let selected = [...form.querySelectorAll(`input[name="${escapedName}"]:checked`)]
        .map((input) => input.value);
      const custom = [...form.querySelectorAll(".ask-custom")]
        .find((input) => input.dataset.questionId === question.id);
      const customValue = custom ? custom.value.trim() : "";
      if (customValue) {
        selected = question.multiSelect ? [...selected, customValue] : [customValue];
      }
      if (!selected.length) {
        const error = form.querySelector(".ask-error");
        error.textContent = "请回答每个问题后再提交。";
        error.hidden = false;
        (custom || form.querySelector(`input[name="${escapedName}"]`)).focus();
        return;
      }
      answers.push({ questionId: question.id, selected });
    }

    state.answeringAsks.add(ask.toolCallId);
    renderCurrent();
    try {
      const chat = await api(
        `/v1/chats/${encodeURIComponent(state.current.id)}/answers`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ toolCallId: ask.toolCallId, answers }),
        },
      );
      state.current = chat;
      renderCurrent();
      updateChatSummary(chat);
    } catch (error) {
      if (error.status === 409 && state.current) {
        toast("这个问题已被回答或跳过，正在刷新会话。");
        await openChat(state.current.id);
      } else {
        toast("提交选择失败：" + error.message);
      }
    } finally {
      state.answeringAsks.delete(ask.toolCallId);
      renderCurrent();
    }
  }

  function toolResultArtifacts(result) {
    if (!result) return [];
    if (typeof result === "string") {
      try { result = JSON.parse(result); } catch { return []; }
    }
    const artifacts = Array.isArray(result.artifacts)
      ? result.artifacts.map(normalizeArtifact).filter(Boolean)
      : [];
    const candidates = [
      ["image", result.imageUrl],
      ["video", result.videoUrl],
      ["audio", result.audioUrl],
    ];
    const seen = new Set(artifacts.map((item) => item && item.uri).filter(Boolean));
    candidates.forEach(([type, uri]) => {
      if (uri && !seen.has(uri)) artifacts.push({ type, uri });
    });
    return artifacts;
  }

  function normalizeArtifact(artifact) {
    if (!artifact || typeof artifact !== "object") return null;
    const typedURLs = [
      ["image", artifact.imageUrl],
      ["video", artifact.videoUrl],
      ["audio", artifact.audioUrl],
    ];
    const typedURL = typedURLs.find(([, value]) =>
      typeof value === "string" && value.trim());
    const uri = firstString(artifact.uri, artifact.url, typedURL && typedURL[1]);
    if (!uri) return null;
    return {
      ...artifact,
      type: firstString(artifact.type, typedURL && typedURL[0]),
      uri,
    };
  }

  function firstString(...values) {
    return values.find((value) => typeof value === "string" && value.trim()) || "";
  }

  function renderArtifact(artifact) {
    const uri = safeURL(artifact.uri);
    if (!uri) return null;
    const card = document.createElement("figure");
    card.className = "artifact";
    if (artifact.type === "image") {
      const image = document.createElement("img");
      image.src = uri;
      image.alt = artifact.description || "Agent 生成的图片";
      image.loading = "lazy";
      card.appendChild(image);
    } else if (artifact.type === "video") {
      const video = document.createElement("video");
      video.src = uri;
      video.controls = true;
      video.preload = "metadata";
      card.appendChild(video);
    } else if (artifact.type === "audio") {
      const audio = document.createElement("audio");
      audio.src = uri;
      audio.controls = true;
      audio.preload = "metadata";
      card.appendChild(audio);
    }
    const foot = document.createElement("figcaption");
    foot.className = "artifact-foot";
    const label = document.createElement("span");
    label.textContent = artifact.description || artifact.type || "产物";
    const open = document.createElement("a");
    open.href = uri;
    open.target = "_blank";
    open.rel = "noopener noreferrer";
    open.textContent = "打开 ↗";
    foot.append(label, open);
    card.appendChild(foot);
    return card;
  }

  function renderArtifacts() {
    const list = $("artifactList");
    const artifacts = currentArtifacts();
    $("artifactCount").textContent = String(artifacts.length);
    $("artifactButtonCount").textContent = String(artifacts.length);
    list.replaceChildren();
    if (!artifacts.length) {
      const empty = document.createElement("div");
      empty.className = "artifact-empty";
      const title = document.createElement("b");
      title.textContent = "暂无产物";
      const detail = document.createElement("span");
      detail.textContent = state.current
        ? "Agent 生成或引用的媒体与文档会集中显示在这里。"
        : "开始一个任务后，生成结果会集中显示在这里。";
      empty.append(title, detail);
      list.appendChild(empty);
      return;
    }
    artifacts.forEach((artifact, index) => {
      const item = renderArtifact(artifact);
      if (!item) return;
      item.classList.add("artifact-sidebar-item");
      item.style.setProperty("--artifact-index", String(index));
      list.appendChild(item);
    });
  }

  function currentArtifacts() {
    if (!state.current) return [];
    const found = [];
    const seen = new Set();
    (state.current.messages || []).forEach((message) => {
      (message.artifacts || []).forEach((artifact) => {
        const uri = safeURL(artifact.uri);
        if (!uri) return;
        const key = `${message.id || ""}\u0000${artifact.source || ""}\u0000` +
          `${artifact.type || ""}\u0000${uri}`;
        if (seen.has(key)) return;
        seen.add(key);
        found.push(artifact);
      });
    });
    return found;
  }

  function renderUploads() {
    const box = $("attachments");
    box.replaceChildren();
    box.hidden = state.uploads.length === 0;
    state.uploads.forEach((item, index) => {
      const chip = document.createElement("div");
      chip.className = "attachment" + (item.error ? " error" : "");
      const name = document.createElement("span");
      name.className = "attachment-name";
      name.textContent = item.name;
      const status = document.createElement("span");
      status.className = "attachment-state";
      status.textContent = item.error ? "失败" : item.url ? "已上传" : "上传中…";
      const remove = document.createElement("button");
      remove.type = "button";
      remove.textContent = "×";
      remove.title = "移除";
      remove.disabled = !item.url && !item.error;
      remove.addEventListener("click", () => {
        state.uploads.splice(index, 1);
        renderUploads();
      });
      chip.append(name, status, remove);
      box.appendChild(chip);
    });
  }

  function refreshComposerControls() {
    const blocked = composerBlocked();
    $("sendButton").disabled = blocked;
    $("fileInput").disabled = blocked;
    $("messageInput").disabled = blocked;
    $("attachButton").classList.toggle("disabled", blocked);
  }

  function composerBlocked() {
    return state.sending || state.uploading ||
      Boolean(state.current && state.current.status === "running");
  }

  async function uploadFiles(files) {
    if (!files || !files.length || composerBlocked()) return;
    const added = [...files].map((file) => ({
      file,
      name: file.name,
      url: "",
      type: "",
      error: "",
    }));
    state.uploads.push(...added);
    state.uploading = true;
    refreshComposerControls();
    renderUploads();
    await Promise.all(added.map(async (item) => {
      const form = new FormData();
      form.append("file", item.file);
      try {
        const result = await api("/v1/uploads", { method: "POST", body: form });
        item.url = result.url;
        item.type = result.type;
      } catch (error) {
        item.error = error.message;
        toast("上传失败：" + item.name + "：" + error.message);
      } finally {
        delete item.file;
        renderUploads();
      }
    }));
    state.uploading = false;
    $("fileInput").value = "";
    refreshComposerControls();
  }

  function messageWithUploads(text, uploads) {
    const lines = uploads.map((item) =>
      `- ${item.name}（${item.type}）：${item.url}`);
    const prompt = text || "请处理以下本地素材。";
    return prompt + "\n\n用户上传的本地素材（均为共享对象存储 URL）：\n" +
      lines.join("\n");
  }

  async function sendMessage(event) {
    event.preventDefault();
    const input = $("messageInput");
    const text = input.value.trim();
    const readyUploads = state.uploads.filter((item) => item.url);
    if ((!text && !readyUploads.length) ||
        composerBlocked() || !state.agent) return;
    const message = readyUploads.length ? messageWithUploads(text, readyUploads) : text;
    state.sending = true;
    refreshComposerControls();
    input.value = "";
    resizeComposer();
    try {
      let chat;
      if (!state.current) {
        chat = await api(`/v1/agents/${encodeURIComponent(agentID)}/chats`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ message }),
        });
      } else {
        chat = await api(`/v1/chats/${encodeURIComponent(state.current.id)}/messages`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ text: message }),
        });
      }
      state.uploads = state.uploads.filter((item) => item.error);
      renderUploads();
      state.current = chat;
      renderCurrent();
      await loadChats();
      connectStream();
    } catch (error) {
      input.value = text;
      resizeComposer();
      toast("发送失败：" + error.message);
    } finally {
      state.sending = false;
      refreshComposerControls();
      input.focus();
    }
  }

  function connectStream() {
    closeStream();
    if (!state.current) return;
    const stream = new EventSource(
      `/v1/chats/${encodeURIComponent(state.current.id)}/stream`,
    );
    state.stream = stream;
    stream.addEventListener("snapshot", (event) => {
      const snapshot = JSON.parse(event.data);
      if (!state.current || snapshot.id !== state.current.id) return;
      state.current = snapshot;
      renderCurrent();
      updateChatSummary(snapshot);
    });
    stream.addEventListener("error", (event) => {
      if (event.data) {
        try {
          const payload = JSON.parse(event.data);
          toast(payload.error || "实时连接中断");
        } catch { /* EventSource transport error */ }
      }
    });
  }

  function closeStream() {
    if (state.stream) state.stream.close();
    state.stream = null;
  }

  function updateChatSummary(chat) {
    const index = state.chats.findIndex((item) => item.id === chat.id);
    const summary = {
      id: chat.id,
      agentId: chat.agentId,
      title: chat.title,
      status: chat.status,
      updatedAt: chat.updatedAt,
    };
    if (index >= 0) state.chats[index] = summary;
    else state.chats.unshift(summary);
    renderSessions();
  }

  function bindEvents() {
    $("newChat").addEventListener("click", startNewChat);
    $("composer").addEventListener("submit", sendMessage);
    $("messageInput").addEventListener("input", resizeComposer);
    $("messageInput").addEventListener("keydown", (event) => {
      if (event.key === "Enter" && !event.shiftKey && !event.metaKey &&
          !event.ctrlKey && !event.altKey && !event.isComposing) {
        event.preventDefault();
        $("composer").requestSubmit();
      }
    });
    $("menuButton").addEventListener("click", () => document.body.classList.add("sidebar-open"));
    $("closeSidebar").addEventListener("click", closeSidebar);
    $("sidebarScrim").addEventListener("click", closeSidebar);
    $("artifactButton").addEventListener("click", openArtifacts);
    $("closeArtifacts").addEventListener("click", closeArtifacts);
    $("artifactScrim").addEventListener("click", closeArtifacts);
    $("fileInput").addEventListener("change", () => uploadFiles($("fileInput").files));
    const carriesFiles = (event) =>
      [...(event.dataTransfer && event.dataTransfer.types || [])].includes("Files");
    document.addEventListener("dragenter", (event) => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      document.body.classList.add("drag-over");
    });
    document.addEventListener("dragover", (event) => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
    });
    document.addEventListener("dragleave", (event) => {
      if (event.relatedTarget === null) document.body.classList.remove("drag-over");
    });
    document.addEventListener("drop", (event) => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      document.body.classList.remove("drag-over");
      uploadFiles(event.dataTransfer.files);
    });
    window.addEventListener("blur", () => document.body.classList.remove("drag-over"));
    window.addEventListener("beforeunload", closeStream);
  }

  function closeSidebar() {
    document.body.classList.remove("sidebar-open");
  }

  function openArtifacts() {
    document.body.classList.add("artifacts-open");
    $("artifactButton").setAttribute("aria-expanded", "true");
  }

  function closeArtifacts() {
    document.body.classList.remove("artifacts-open");
    $("artifactButton").setAttribute("aria-expanded", "false");
  }

  function resizeComposer() {
    const input = $("messageInput");
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 180) + "px";
  }

  function setRunState(status, label) {
    $("runState").className = "run-state " + (status || "");
    $("runState").querySelector("span").textContent = label;
  }

  function statusLabel(status) {
    return ({ running: "处理中", idle: "等待输入", error: "需要处理" })[status] || "等待输入";
  }

  function toolStatusLabel(status) {
    return ({ pending: "等待", running: "执行中", completed: "已完成", failed: "失败" })[status] || status;
  }

  function humanToolName(name) {
    const names = {
      generate_image: "生成图片",
      edit_image: "编辑图片",
      understand_media: "理解媒体",
      understand_video_segment: "理解视频区间",
      kling_text_to_video: "可灵文生视频",
      kling_image_to_video: "可灵图生视频",
      kling_omni_video: "可灵视频生成",
      seedance_generate_video: "Seedance 视频生成",
      generate_brand_ad: "生成品牌广告",
      concat_videos: "拼接视频",
      process_video: "处理视频",
      compose_video: "合成视频",
      extract_audio: "提取音频",
      extract_frame: "提取视频帧",
      probe_video: "探测视频信息",
      generate_speech: "生成语音",
      transcribe_audio: "语音识别",
      job_status: "查询任务",
      list_artifacts: "查找历史产物",
      read_file: "读取文档",
      web_search: "联网检索",
      web_fetch: "读取网页",
      fetch_product_detail: "提取商品详情",
      ask_user: "请求用户选择",
      echo: "回显",
      current_time: "查询当前时间",
    };
    return names[name] || name || "工具调用";
  }

  function prettyJSON(value) {
    if (!value) return "";
    if (typeof value === "string") {
      try { return JSON.stringify(JSON.parse(value), null, 2); } catch { return value; }
    }
    try { return JSON.stringify(value, null, 2); } catch { return ""; }
  }

  function safeURL(value) {
    if (typeof value !== "string" || !value.trim()) return "";
    try {
      const parsed = new URL(value.trim(), location.origin);
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "";
      return parsed.href;
    } catch {
      return "";
    }
  }

  function formatTime(value) {
    if (!value || String(value).startsWith("0001-")) return "";
    return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" })
      .format(new Date(value));
  }

  function formatRelative(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "";
    const today = new Date();
    if (date.toDateString() === today.toDateString()) return formatTime(value);
    return `${date.getMonth() + 1}/${date.getDate()}`;
  }

  let toastTimer = null;
  function toast(message) {
    $("toast").textContent = message;
    $("toast").classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => $("toast").classList.remove("show"), 3000);
  }

  initialize();
})();
