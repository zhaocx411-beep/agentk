"use strict";

const PIPELINES = new Set([
  "documentary_video",
  "documentary_script",
  "documentary_render",
]);

const TERMINAL_STATUSES = new Set(["completed", "failed", "cancelled"]);
const STAGE_ORDER = [
  "validate_input",
  "analyze_video",
  "generate_script",
  "synthesize_speech",
  "cut_segments",
  "compose_batches",
  "finalize",
  "complete",
];

const state = {
  pipeline: "documentary_video",
  sessionId: "",
  executionId: "",
  pollTimer: null,
  output: null,
  events: [],
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];

const elements = {
  systemState: $("#system-state"),
  systemStateLabel: $("#system-state-label"),
  videoFile: $("#video-file"),
  uploadZone: $("#upload-zone"),
  uploadTitle: $("#upload-title"),
  uploadButton: $("#upload-button"),
  videoUri: $("#video-uri"),
  theme: $("#theme"),
  instructions: $("#instructions"),
  voiceId: $("#voice-id"),
  aspect: $("#aspect"),
  volume: $("#volume"),
  volumeOutput: $("#volume-output"),
  subtitles: $("#subtitles"),
  scriptFile: $("#script-file"),
  scriptJson: $("#script-json"),
  sessionId: $("#session-id"),
  consoleToken: $("#console-token"),
  runButton: $("#run-button"),
  cancelButton: $("#cancel-button"),
  formError: $("#form-error"),
  requestJson: $("#request-json"),
  outputJson: $("#output-json"),
  eventLog: $("#event-log"),
  eventCount: $("#event-count"),
  activeSession: $("#active-session"),
  activeExecution: $("#active-execution"),
  executionStatus: $("#execution-status"),
  currentStage: $("#current-stage"),
  progressPercent: $("#progress-percent"),
  progressBar: $("#progress-bar"),
  artifactCard: $("#artifact-card"),
  artifactLink: $("#artifact-link"),
  artifactDescription: $("#artifact-description"),
  renderScriptButton: $("#render-script-button"),
  saveOutput: $("#save-output"),
  toast: $("#toast"),
};

function pretty(value) {
  return JSON.stringify(value, null, 2);
}

function showToast(message, type = "info") {
  elements.toast.textContent = message;
  elements.toast.classList.toggle("error", type === "error");
  elements.toast.classList.add("visible");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => elements.toast.classList.remove("visible"), 2600);
}

function setSystemState(kind, label) {
  elements.systemState.classList.remove("online", "offline");
  elements.systemState.classList.add(kind);
  elements.systemStateLabel.textContent = label;
}

async function request(url, options = {}) {
  const headers = new Headers(options.headers || {});
  const token = elements.consoleToken?.value.trim();
  if (token && url.startsWith("/v1/")) {
    headers.set("X-Narrato-Console-Token", token);
  }
  const response = await fetch(url, { ...options, headers });
  let body = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  if (!response.ok) {
    const detail = body?.detail || `${response.status} ${response.statusText}`;
    throw new Error(detail);
  }
  return body;
}

async function loadConfig() {
  try {
    const config = await request("/v1/ui/config");
    $("#vision-model").textContent = config.models.vision;
    $("#text-model").textContent = config.models.text;
    $("#tts-model").textContent = config.models.tts;
    setVoiceOptions(config.voices?.qwen || []);
    setSystemState("online", "控制台在线 · 模型由部署固定");
  } catch (error) {
    setSystemState("offline", "控制台连接失败");
    showToast(error.message, "error");
  }
}

function setVoiceOptions(voices) {
  elements.voiceId.replaceChildren();
  for (const voice of voices) {
    const option = document.createElement("option");
    option.value = voice.id;
    option.textContent = `${voice.description}（${voice.id}）`;
    elements.voiceId.append(option);
  }
  if (!voices.length) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "当前部署未提供可选音色";
    elements.voiceId.append(option);
    elements.voiceId.disabled = true;
  } else {
    elements.voiceId.disabled = false;
    elements.voiceId.value = voices.some((voice) => voice.id === "Cherry")
      ? "Cherry"
      : voices[0].id;
  }
  updateRequestPreview();
}

function setPipeline(pipeline) {
  if (!PIPELINES.has(pipeline)) return;
  state.pipeline = pipeline;
  $$(".pipeline-tab").forEach((button) => {
    const active = button.dataset.pipeline === pipeline;
    button.classList.toggle("active", active);
    button.setAttribute("aria-checked", String(active));
  });
  $$('[data-for]').forEach((node) => {
    const pipelines = node.dataset.for.split(",");
    node.classList.toggle("hidden", !pipelines.includes(pipeline));
  });
  elements.formError.textContent = "";
  updateRequestPreview();
}

function buildArgs({ validate = false } = {}) {
  const args = {};
  if (state.pipeline !== "documentary_render") {
    const videoUri = elements.videoUri.value.trim();
    if (validate && !videoUri) throw new Error("请先上传视频或填写 videoUri");
    if (videoUri) args.videoUri = videoUri;
    args.theme = elements.theme.value.trim();
    args.instructions = elements.instructions.value.trim();
  } else {
    const rawScript = elements.scriptJson.value.trim();
    if (validate && !rawScript) throw new Error("请粘贴或导入脚本 JSON");
    if (rawScript) {
      try {
        args.script = JSON.parse(rawScript);
      } catch (error) {
        if (validate) throw new Error(`脚本 JSON 无法解析：${error.message}`);
        args.script = "<等待合法 JSON>";
      }
    }
  }

  if (state.pipeline !== "documentary_script") {
    const voiceId = elements.voiceId.value.trim();
    if (validate && !voiceId) throw new Error("voiceId 是渲染阶段必填项");
    if (voiceId) args.voiceId = voiceId;
    args.aspect = elements.aspect.value;
    args.narrationVolume = Number(elements.volume.value);
    args.subtitles = elements.subtitles.checked;
  }
  return args;
}

function buildRequestPreview() {
  const sessionId = elements.sessionId.value.trim();
  return {
    pipeline: state.pipeline,
    args: buildArgs(),
    ...(sessionId ? { sessionId } : {}),
    title: "Narrato Browser Test",
  };
}

function updateRequestPreview() {
  elements.requestJson.textContent = pretty(buildRequestPreview());
}

function setSelectedFile(file) {
  if (!file) return;
  elements.videoFile.files = createFileList(file);
  elements.uploadTitle.textContent = file.name;
  updateRequestPreview();
}

function createFileList(file) {
  const transfer = new DataTransfer();
  transfer.items.add(file);
  return transfer.files;
}

async function uploadVideo() {
  const file = elements.videoFile.files[0];
  if (!file) {
    showToast("请先选择一个视频文件", "error");
    return;
  }
  const form = new FormData();
  form.append("file", file);
  elements.uploadButton.disabled = true;
  elements.uploadButton.textContent = "正在流式上传…";
  try {
    const result = await request("/v1/media", { method: "POST", body: form });
    elements.videoUri.value = result.uri;
    updateRequestPreview();
    showToast(`上传完成 · ${formatBytes(result.sizeBytes)}`);
  } catch (error) {
    showToast(error.message, "error");
  } finally {
    elements.uploadButton.disabled = false;
    elements.uploadButton.textContent = "上传并生成 Artifact URI";
  }
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes)) return "";
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}

function setExecutionStatus(status) {
  const normalized = status || "idle";
  elements.executionStatus.className = `execution-status ${normalized}`;
  elements.executionStatus.textContent = normalized.toUpperCase();
  const running = normalized === "running";
  elements.cancelButton.disabled = !running;
  elements.runButton.disabled = running;
}

function resetExecutionView() {
  state.events = [];
  state.output = null;
  elements.eventCount.textContent = "0";
  elements.eventLog.innerHTML = '<p class="empty-state">等待 AgentK 进度事件…</p>';
  elements.outputJson.textContent = "等待执行结果…";
  elements.saveOutput.disabled = true;
  elements.artifactCard.classList.add("hidden");
  elements.renderScriptButton.classList.add("hidden");
  updateProgress([], "running");
}

async function runPipeline() {
  elements.formError.textContent = "";
  let args;
  try {
    args = buildArgs({ validate: true });
  } catch (error) {
    elements.formError.textContent = error.message;
    showToast(error.message, "error");
    return;
  }

  stopPolling();
  resetExecutionView();
  setExecutionStatus("running");
  elements.runButton.querySelector("span").textContent = "正在创建执行";
  const body = {
    pipeline: state.pipeline,
    args,
    title: "Narrato Browser Test",
  };
  const existingSession = elements.sessionId.value.trim();
  if (existingSession) body.sessionId = existingSession;
  elements.requestJson.textContent = pretty(body);

  try {
    const started = await request("/v1/ui/executions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    state.sessionId = started.sessionId;
    state.executionId = started.executionId;
    elements.activeSession.textContent = state.sessionId;
    elements.activeExecution.textContent = state.executionId;
    elements.sessionId.value = state.sessionId;
    setExecutionStatus(started.status);
    showToast("Pipeline 已启动，开始监看事件");
    await pollExecution();
  } catch (error) {
    setExecutionStatus("failed");
    elements.formError.textContent = error.message;
    showToast(error.message, "error");
  } finally {
    elements.runButton.querySelector("span").textContent = "启动 Pipeline";
  }
}

async function pollExecution() {
  if (!state.sessionId || !state.executionId) return;
  state.pollTimer = null;
  try {
    const execution = await request(
      `/v1/ui/executions/${encodeURIComponent(state.sessionId)}/${encodeURIComponent(state.executionId)}`,
    );
    setExecutionStatus(execution.status);
    renderEvents(execution.events || []);
    updateProgress(execution.events || [], execution.status);
    if (execution.output !== null && execution.output !== undefined) {
      renderOutput(execution.output);
    }
    if (execution.error) {
      elements.formError.textContent = execution.error;
    }
    if (!TERMINAL_STATUSES.has(execution.status)) {
      state.pollTimer = window.setTimeout(pollExecution, 1500);
    } else {
      const message =
        execution.status === "completed"
          ? "执行完成"
          : `执行结束：${execution.status}`;
      showToast(message, execution.status === "completed" ? "info" : "error");
    }
  } catch (error) {
    showToast(`状态刷新失败：${error.message}`, "error");
    state.pollTimer = window.setTimeout(pollExecution, 3000);
  }
}

function stopPolling() {
  if (state.pollTimer) window.clearTimeout(state.pollTimer);
  state.pollTimer = null;
}

function renderEvents(events) {
  state.events = events;
  elements.eventCount.textContent = String(events.length);
  if (!events.length) {
    elements.eventLog.innerHTML = '<p class="empty-state">等待 AgentK 进度事件…</p>';
    return;
  }
  elements.eventLog.replaceChildren(
    ...events.map((event) => {
      const row = document.createElement("div");
      row.className = "event-row";
      const step = document.createElement("span");
      step.className = "event-step";
      step.textContent = event.step || event.type;
      const message = document.createElement("span");
      message.className = "event-message";
      message.textContent = event.message || event.status || "—";
      const data = document.createElement("span");
      data.className = "event-data";
      data.textContent = compactEventData(event.data);
      row.append(step, message, data);
      return row;
    }),
  );
  elements.eventLog.scrollTop = elements.eventLog.scrollHeight;
}

function compactEventData(data) {
  if (!data || typeof data !== "object") return "";
  const parts = [];
  if (Number.isFinite(data.percent)) parts.push(`${data.percent}%`);
  if (Number.isFinite(data.completed) && Number.isFinite(data.total)) {
    parts.push(`${data.completed}/${data.total}`);
  }
  if (data.segmentId) parts.push(data.segmentId);
  return parts.join(" · ");
}

function updateProgress(events, status) {
  const progressEvents = events.filter((event) => event.type === "progress");
  const latest = progressEvents.at(-1);
  let percent = Number(latest?.data?.percent || 0);
  if (status === "completed") percent = 100;
  percent = Math.max(0, Math.min(100, percent));
  elements.progressPercent.textContent = `${percent}%`;
  elements.progressBar.style.width = `${percent}%`;
  elements.currentStage.textContent = latest
    ? `${latest.step} / ${latest.message}`
    : status === "running"
      ? "EXECUTION ACCEPTED"
      : "WAITING FOR INPUT";

  const activeStage = latest?.step;
  const activeIndex = STAGE_ORDER.indexOf(activeStage);
  $$("#stage-rail li").forEach((item, index) => {
    item.classList.toggle("active", index === activeIndex && status === "running");
    item.classList.toggle("done", status === "completed" || (activeIndex >= 0 && index < activeIndex));
  });
}

function renderOutput(output) {
  state.output = output;
  elements.outputJson.textContent = pretty(output);
  elements.saveOutput.disabled = false;
  elements.artifactCard.classList.add("hidden");
  elements.artifactLink.removeAttribute("href");
  const artifacts = Array.isArray(output?.artifacts) ? output.artifacts : [];
  const video = artifacts.find((item) => item?.type === "video" && item?.uri);
  const artifactUri = safeHttpUrl(video?.uri);
  if (video && artifactUri) {
    elements.artifactLink.href = artifactUri;
    elements.artifactDescription.textContent = video.description || "纪录片解说成片";
    elements.artifactCard.classList.remove("hidden");
  }
  const script = output?.script || (output?.version === "1" ? output : null);
  elements.renderScriptButton.classList.toggle("hidden", !script);
}

function safeHttpUrl(value) {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value);
    if (url.protocol === "http:" || url.protocol === "https:") return url.href;
  } catch {
    return null;
  }
  return null;
}

async function cancelExecution() {
  if (!state.sessionId) return;
  stopPolling();
  elements.cancelButton.disabled = true;
  try {
    await request(`/v1/ui/sessions/${encodeURIComponent(state.sessionId)}/cancel`, {
      method: "POST",
    });
    showToast("取消请求已发送；等待 AgentK 确认终态");
    await pollExecution();
  } catch (error) {
    showToast(error.message, "error");
    elements.cancelButton.disabled = false;
  }
}

function switchConsolePanel(panel) {
  $$(".console-tab").forEach((tab) => tab.classList.toggle("active", tab.dataset.panel === panel));
  $$(".console-panel").forEach((node) => node.classList.toggle("active", node.id === `panel-${panel}`));
}

function loadOutputForRender() {
  if (!state.output) return;
  const script = state.output.script || state.output;
  elements.scriptJson.value = pretty(script);
  setPipeline("documentary_render");
  updateRequestPreview();
  window.scrollTo({ top: $(".workbench").offsetTop - 16, behavior: "smooth" });
  showToast("脚本已载入渲染台");
}

function loadSampleScript() {
  elements.scriptJson.value = pretty({
    version: "1",
    sourceVideoUri: "https://cdn.example/source.mp4",
    sourceDurationSeconds: 20,
    theme: "自然纪录片",
    segments: [
      {
        id: "segment-001",
        sourceStartSeconds: 0,
        sourceEndSeconds: 8,
        narration: "晨雾沿着山谷缓慢散开。",
        ost: 2,
      },
      {
        id: "segment-002",
        sourceStartSeconds: 8,
        sourceEndSeconds: 14,
        narration: "溪流穿过仍在沉睡的森林。",
        ost: 1,
      },
    ],
  });
  updateRequestPreview();
}

function saveOutput() {
  if (!state.output) return;
  const blob = new Blob([pretty(state.output)], { type: "application/json;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = state.pipeline === "documentary_script" ? "script.json" : "narrato-output.json";
  anchor.click();
  URL.revokeObjectURL(url);
}

async function copyText(elementId) {
  const value = document.getElementById(elementId)?.textContent || "";
  try {
    await navigator.clipboard.writeText(value);
    showToast("已复制到剪贴板");
  } catch {
    showToast("浏览器未允许剪贴板访问", "error");
  }
}

function bindEvents() {
  $$(".pipeline-tab").forEach((button) =>
    button.addEventListener("click", () => setPipeline(button.dataset.pipeline)),
  );
  $$("input, textarea, select").forEach((input) => {
    input.addEventListener("input", updateRequestPreview);
    input.addEventListener("change", updateRequestPreview);
  });
  elements.volume.addEventListener("input", () => {
    elements.volumeOutput.textContent = Number(elements.volume.value).toFixed(1);
  });
  elements.consoleToken.addEventListener("change", () => {
    sessionStorage.setItem("narratoConsoleToken", elements.consoleToken.value);
    loadConfig();
  });
  elements.videoFile.addEventListener("change", () => {
    const file = elements.videoFile.files[0];
    if (file) elements.uploadTitle.textContent = file.name;
  });
  ["dragenter", "dragover"].forEach((name) =>
    elements.uploadZone.addEventListener(name, (event) => {
      event.preventDefault();
      elements.uploadZone.classList.add("dragging");
    }),
  );
  ["dragleave", "drop"].forEach((name) =>
    elements.uploadZone.addEventListener(name, (event) => {
      event.preventDefault();
      elements.uploadZone.classList.remove("dragging");
    }),
  );
  elements.uploadZone.addEventListener("drop", (event) => {
    const file = event.dataTransfer.files[0];
    if (file) setSelectedFile(file);
  });
  elements.uploadButton.addEventListener("click", uploadVideo);
  elements.runButton.addEventListener("click", runPipeline);
  elements.cancelButton.addEventListener("click", cancelExecution);
  $("#sample-script").addEventListener("click", loadSampleScript);
  elements.scriptFile.addEventListener("change", async () => {
    const file = elements.scriptFile.files[0];
    if (!file) return;
    elements.scriptJson.value = await file.text();
    updateRequestPreview();
  });
  $$(".console-tab").forEach((button) =>
    button.addEventListener("click", () => switchConsolePanel(button.dataset.panel)),
  );
  $$('[data-copy]').forEach((button) =>
    button.addEventListener("click", () => copyText(button.dataset.copy)),
  );
  $("#clear-events").addEventListener("click", () => {
    elements.eventLog.innerHTML = '<p class="empty-state">显示已清空；下次轮询会重新同步事件。</p>';
  });
  elements.saveOutput.addEventListener("click", saveOutput);
  elements.renderScriptButton.addEventListener("click", loadOutputForRender);
  window.addEventListener("beforeunload", stopPolling);
}

bindEvents();
elements.consoleToken.value = sessionStorage.getItem("narratoConsoleToken") || "";
setPipeline(state.pipeline);
loadConfig();
