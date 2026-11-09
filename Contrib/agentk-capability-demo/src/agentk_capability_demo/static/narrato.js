(() => {
  "use strict";

  const PIPELINES = new Set(["documentary_video", "documentary_script", "documentary_render"]);
  const TERMINAL = new Set(["completed", "failed", "cancelled"]);
  const POLL_INTERVAL_MS = 10000;
  const HISTORY_POLL_INTERVAL_MS = 10000;
  const state = { pipeline: "documentary_video", sessionId: "", executionId: "", timer: null, historyTimer: null, output: null, taskInput: null, uploading: false, submitting: false, pollFailures: 0, entryRequest: null, requestTimes: new Map(), ttsProviders: {}, styles: [], style: "documentary", originalVolumeTouched: false };
  const $ = id => document.getElementById(id);
  const $$ = selector => [...document.querySelectorAll(selector)];
  const pretty = value => JSON.stringify(value, null, 2);

  function formatTimestamp(value) {
    const time = new Date(value);
    if (Number.isNaN(time.valueOf())) return "时间未知";
    const parts = new Intl.DateTimeFormat("zh-CN", {
      year: "numeric", month: "2-digit", day: "2-digit",
      hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
    }).formatToParts(time).reduce((result, part) => ({ ...result, [part.type]: part.value }), {});
    return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`;
  }

  async function api(path, options = {}) {
    const response = await fetch(path, { cache: "no-store", ...options });
    let body = null;
    try { body = await response.json(); } catch { body = null; }
    if (!response.ok) throw new Error(body?.detail || `HTTP ${response.status}`);
    return body;
  }

  function toast(message, error = false) {
    const node = document.createElement("div");
    node.className = `toast${error ? " error" : ""}`;
    node.textContent = message;
    $("toasts").append(node);
    setTimeout(() => node.remove(), 4500);
  }

  function setSelect(id, values = [], selected = "") {
    const select = $(id); select.replaceChildren();
    const candidates = values.length ? values : (selected ? [selected] : []);
    candidates.forEach(value => select.append(new Option(value, value)));
    if (!candidates.length) select.append(new Option("当前部署无候选", ""));
    select.value = candidates.includes(selected) ? selected : (candidates[0] || "");
  }

  function selectTtsProvider(provider) {
    const spec = state.ttsProviders[provider] || {};
    setSelect("narratoTtsModel", spec.models || [], spec.defaultModel || "");
    const voice = $("narratoVoice"); voice.replaceChildren();
    (spec.voices || []).forEach(item => voice.append(new Option(`${item.description}（${item.id}）`, item.id)));
    if (!voice.options.length) voice.append(new Option("当前 Provider 无可用音色", ""));
    voice.value = (spec.voices || []).some(item => item.id === spec.defaultVoiceId) ? spec.defaultVoiceId : (voice.options[0]?.value || "");
    updatePreview();
  }

  function renderStyles(styles) {
    state.styles = styles;
    const root = $("narratoStyles"); root.replaceChildren();
    styles.forEach(item => {
      const card = document.createElement("button");
      card.type = "button"; card.className = "narrato-style"; card.dataset.styleId = item.id;
      card.setAttribute("role", "radio");
      const title = document.createElement("b"); title.textContent = item.displayName;
      const note = document.createElement("small"); note.textContent = item.description;
      card.append(title, note);
      card.addEventListener("click", () => selectStyle(item.id));
      root.append(card);
    });
    selectStyle(styles.some(item => item.id === state.style) ? state.style : (styles[0]?.id || ""));
  }

  function selectStyle(styleId) {
    state.style = styleId;
    $$('[data-style-id]').forEach(card => {
      const active = card.dataset.styleId === styleId;
      card.classList.toggle("active", active);
      card.setAttribute("aria-checked", String(active));
    });
    // 原声音量跟随风格，除非用户已经手动调过——手动值优先于预设。
    const spec = state.styles.find(item => item.id === styleId);
    if (spec && !state.originalVolumeTouched) {
      $("narratoOriginalVolume").value = String(spec.defaultOriginalAudioVolume);
      $("narratoOriginalVolumeOutput").textContent = Number(spec.defaultOriginalAudioVolume).toFixed(2);
    }
    updatePreview();
  }

  function selectPipeline(pipeline) {
    if (!PIPELINES.has(pipeline)) return;
    state.pipeline = pipeline;
    $$('[data-narrato-pipeline]').forEach(button => button.classList.toggle("active", button.dataset.narratoPipeline === pipeline));
    $$('[data-narrato-for]').forEach(node => node.classList.toggle("hidden", !node.dataset.narratoFor.split(",").includes(pipeline)));
    hideError(); updatePreview();
  }

  function buildArgs(validate = false) {
    const args = {};
    if (state.pipeline !== "documentary_render") {
      const videoUri = $("narratoVideoUri").value.trim();
      if (validate && !videoUri) throw new Error("请先上传视频或填写 videoUri");
      if (videoUri) args.videoUri = videoUri;
      args.instructions = $("narratoInstructions").value.trim();
      if (state.style) args.style = state.style;
      args.models = {
        vision: $("narratoVisionModel").value, asr: $("narratoAsrModel").value,
        story: $("narratoStoryModel").value, script: $("narratoScriptModel").value,
      };
      args.analysisOptions = {
        asrEnabled: $("narratoAsrEnabled").checked,
        asrLanguage: $("narratoAsrLanguage").value.trim(),
      };
    } else {
      const source = $("narratoScriptJson").value.trim();
      if (validate && !source) throw new Error("请粘贴或导入脚本 JSON");
      if (source) {
        try { args.script = JSON.parse(source); }
        catch (error) { if (validate) throw new Error(`脚本 JSON 无法解析：${error.message}`); args.script = "<等待合法 JSON>"; }
      }
    }
    if (state.pipeline !== "documentary_script") {
      const provider = $("narratoTtsProvider").value;
      const model = $("narratoTtsModel").value;
      const voiceId = $("narratoVoice").value;
      if (validate && (!provider || !model || !voiceId)) throw new Error("TTS Provider、模型和 voiceId 都是渲染阶段必填项");
      if (provider || model || voiceId) args.tts = { provider, model, voiceId };
      args.aspect = $("narratoAspect").value;
      args.narrationVolume = Number($("narratoVolume").value);
      args.originalAudioVolume = Number($("narratoOriginalVolume").value);
      args.subtitles = $("narratoSubtitles").checked;
    }
    return args;
  }

  function requestBody(validate = false) {
    return { pipeline: state.pipeline, args: buildArgs(validate), title: "Narrato Browser Test" };
  }

  function updatePreview() {
    if (state.executionId) return;
    try { renderRequests(requestBody(false), []); }
    catch { renderRequests(null, []); }
  }

  function renderRequests(entryRequest, events, entryRequestedAt = "") {
    const root = $("narratoRequestJson"); root.replaceChildren();
    const entries = window.AgentKRequestLedger.collectRequestEntries(entryRequest, events, entryRequestedAt);
    if (!entries.length) {
      const empty = document.createElement("p"); empty.className = "request-ledger-empty"; empty.textContent = "等待合法输入…"; root.append(empty); return;
    }
    const head = document.createElement("div"); head.className = "request-ledger-head";
    const label = document.createElement("span"); label.textContent = "EXECUTION REQUEST LEDGER";
    const count = document.createElement("b"); count.textContent = `${entries.length} CALL${entries.length === 1 ? "" : "S"}`;
    head.append(label, count); root.append(head);
    entries.forEach((entry, index) => {
      const details = document.createElement("details"); details.className = "request-entry"; details.open = index === 0;
      const summary = document.createElement("summary");
      const number = document.createElement("span"); number.className = "request-index"; number.textContent = String(index + 1).padStart(2, "0");
      const kind = document.createElement("span"); kind.className = "request-kind"; kind.textContent = entry.kind;
      const name = document.createElement("span"); name.className = "request-name"; name.textContent = entry.name; name.title = entry.name;
      const occurred = entry.kind !== "ENTRY" || Boolean(entry.requestedAt);
      const timing = document.createElement("span"); timing.className = "request-timing";
      const timingLabel = document.createElement("span"); timingLabel.className = "request-time-label";
      timingLabel.textContent = entry.requestedAtSource === "observed" ? "首次观测" : "发生时间";
      let timingValue;
      if (entry.requestedAt) {
        timingValue = document.createElement("time"); timingValue.dateTime = entry.requestedAt; timingValue.title = entry.requestedAt;
        timingValue.textContent = formatTimestamp(entry.requestedAt);
      } else {
        timingValue = document.createElement("span"); timingValue.textContent = occurred ? "时间未知" : "—";
      }
      timingValue.className = `request-time-value${entry.requestedAt ? "" : " missing"}`;
      timing.append(timingLabel, timingValue);
      const requestState = document.createElement("span"); requestState.className = `request-state ${occurred ? "occurred" : "pending"}`;
      requestState.textContent = occurred ? "已发生" : "待提交";
      const requestLabel = document.createElement("b"); requestLabel.className = "request-payload-label"; requestLabel.textContent = "REQUEST";
      const requestPayload = document.createElement("pre"); requestPayload.textContent = pretty(entry.request);
      summary.append(number, kind, name, timing, requestState); details.append(summary, requestLabel, requestPayload);
      if (Object.prototype.hasOwnProperty.call(entry, "response")) {
        const responseLabel = document.createElement("b"); responseLabel.className = "request-payload-label response"; responseLabel.textContent = "RESPONSE";
        const responsePayload = document.createElement("pre"); responsePayload.textContent = pretty(entry.response);
        details.append(responseLabel, responsePayload);
      }
      root.append(details);
    });
  }

  function collapseTaskInput(restoreFocus = false) {
    $("narratoInputSummary").hidden = true;
    $("narratoInputParams").setAttribute("aria-expanded", "false");
    $("narratoInputParams").textContent = "INPUT PARAMS";
    if (restoreFocus) $("narratoInputParams").focus();
  }

  function resetTaskInputActions() {
    state.taskInput = null;
    collapseTaskInput();
    $("narratoInputJson").textContent = "";
    $("narratoInputParams").hidden = true;
    const video = $("narratoInputVideo");
    video.hidden = true;
    video.removeAttribute("href");
  }

  function renderTaskInputActions(entryRequest, events) {
    const input = window.AgentKRequestLedger.selectTaskInput(entryRequest, events);
    state.taskInput = input;
    const params = $("narratoInputParams");
    params.hidden = !input;
    $("narratoInputJson").textContent = input ? pretty(input) : "";
    if (!input) collapseTaskInput();

    const videoUri = window.AgentKRequestLedger.findInputVideoUri(input);
    const video = $("narratoInputVideo");
    video.hidden = !videoUri;
    if (videoUri) video.href = videoUri;
    else video.removeAttribute("href");
  }

  function toggleTaskInput() {
    if (!state.taskInput) return;
    const summary = $("narratoInputSummary");
    const expanded = summary.hidden;
    summary.hidden = !expanded;
    $("narratoInputParams").setAttribute("aria-expanded", String(expanded));
    $("narratoInputParams").textContent = expanded ? "HIDE INPUT" : "INPUT PARAMS";
  }

  function renderCatalog(agents) {
    $("agent100Count").textContent = agents.length;
    const root = $("agent100List"); root.replaceChildren();
    agents.forEach((agent, index) => {
      const button = document.createElement("button");
      button.className = `catalog-item${index === 0 ? " active" : ""}`;
      button.style.setProperty("--item-accent", agent.accent);
      button.innerHTML = `<span class="agent-mini"></span><b></b><small></small><span class="item-meta"></span>`;
      button.querySelector(".agent-mini").textContent = agent.icon;
      button.querySelector("b").textContent = agent.name;
      button.querySelector("small").textContent = agent.description;
      button.querySelector(".item-meta").textContent = agent.id;
      root.append(button);
    });
  }

  function renderCapabilities(items, error) {
    const root = $("narratoCapabilityList"); root.replaceChildren();
    if (error) {
      $("narratoCapabilitySummary").textContent = "查询失败";
      const row = document.createElement("p"); row.className = "narrato-hint"; row.textContent = error; root.append(row); return;
    }
    const pipelines = items.filter(item => item.group === "pipeline"), dependencies = items.filter(item => item.group !== "pipeline");
    const pipelineReady = pipelines.filter(item => item.published).length;
    const dependencyReady = dependencies.filter(item => item.published || item.activityOnly).length;
    $("narratoCapabilitySummary").textContent = `${pipelineReady}/${pipelines.length} PIPELINES · ${dependencyReady}/${dependencies.length} DEPS`;
    items.forEach(item => {
      const row = document.createElement("div"); row.className = `narrato-capability ${item.group || "dependency"}${item.published || item.activityOnly ? "" : " missing"}`;
      const code = document.createElement("code"); code.textContent = item.id;
      const status = document.createElement("span"); status.textContent = item.activityOnly ? "ACTIVITY ABI" : item.published ? "PUBLISHED" : "MISSING";
      row.append(code, status); root.append(row);
    });
  }

  function renderHistory(items) {
    const root = $("narratoHistoryList"); root.replaceChildren();
    const runningCount = items.filter(item => !TERMINAL.has(String(item.status || "running").toLowerCase())).length;
    $("narratoHistoryCount").textContent = `${items.length} 条记录`;
    $("narratoHistoryRunning").textContent = runningCount ? `${runningCount} 个后台执行中` : "暂无运行中任务";
    $("narratoHistoryRunning").classList.toggle("active", runningCount > 0);
    if (!items.length) { const empty = document.createElement("p"); empty.textContent = "暂无生成记录"; root.append(empty); return; }
    items.slice(0, 12).forEach(item => {
      const storedStatus = String(item.status || "running").toLowerCase();
      const running = !TERMINAL.has(storedStatus);
      const normalizedStatus = item.cancelRequested && running ? "cancelling" : storedStatus;
      const selected = state.sessionId === item.sessionId && state.executionId === item.executionId;
      const card = document.createElement("article");
      card.className = `narrato-history-item status-${normalizedStatus}${selected ? " selected" : ""}`;
      const button = document.createElement("button"); button.type = "button"; button.className = "narrato-history-open";
      const title = document.createElement("b"); title.textContent = item.title || item.pipeline;
      const status = document.createElement("span"); status.textContent = normalizedStatus.toUpperCase();
      const meta = document.createElement("small");
      meta.textContent = `${formatTimestamp(item.createdAt)} · ${item.pipeline}`;
      const execution = document.createElement("code"); execution.textContent = item.executionId;
      button.append(title, status, meta, execution); button.addEventListener("click", () => openHistory(item)); card.append(button);
      if (running) {
        const stop = document.createElement("button"); stop.type = "button"; stop.className = "narrato-history-stop";
        stop.textContent = item.cancelRequested ? "停止中…" : "STOP"; stop.disabled = Boolean(item.cancelRequested);
        stop.setAttribute("aria-label", `停止任务：${item.title || item.pipeline}`);
        stop.addEventListener("click", () => cancelHistory(item, stop)); card.append(stop);
      }
      root.append(card);
    });
  }

  async function loadHistory(silent = false, refresh = false) {
    try {
      const history = await api(`/api/agent100/narrato/executions${refresh ? "?refresh=true" : ""}`);
      renderHistory(history.items || []);
    }
    catch (error) { if (!silent) toast(`Narrato 历史读取失败：${error.message}`, true); }
  }

  function scheduleHistoryPoll(delay = HISTORY_POLL_INTERVAL_MS) {
    if (state.historyTimer) clearTimeout(state.historyTimer);
    state.historyTimer = setTimeout(async () => {
      await loadHistory(true, true);
      scheduleHistoryPoll();
    }, delay);
  }

  function flashHistory() {
    const history = $("narratoHistory");
    history.classList.remove("attention");
    requestAnimationFrame(() => history.classList.add("attention"));
    setTimeout(() => history.classList.remove("attention"), 1800);
  }

  async function cancelHistory(item, button) {
    if (button.disabled) return;
    button.disabled = true; button.textContent = "停止中…";
    try {
      await api(`/api/agent100/narrato/executions/${encodeURIComponent(item.sessionId)}/${encodeURIComponent(item.executionId)}/cancel`, { method: "POST" });
      toast(`已向「${item.title || item.pipeline}」发送停止请求`);
      if (state.sessionId === item.sessionId && state.executionId === item.executionId) {
        $("narratoStage").textContent = "停止请求已接受 · 等待 AgentK 返回终态";
      }
      await loadHistory(true, false);
      scheduleHistoryPoll(1500);
    } catch (error) {
      button.disabled = false; button.textContent = "STOP"; toast(`停止失败：${error.message}`, true);
    }
  }

  function openHistory(item) {
    stopPolling(); hideError(); state.output = null; state.pollFailures = 0;
    state.entryRequest = null;
    state.requestTimes.clear();
    resetTaskInputActions();
    state.sessionId = item.sessionId; state.executionId = item.executionId;
    if (PIPELINES.has(item.pipeline)) selectPipeline(item.pipeline);
    $("narratoSession").textContent = state.sessionId; $("narratoExecution").textContent = state.executionId;
    $("narratoEvents").replaceChildren(); renderRequests(null, []); $("narratoOutputJson").textContent = "正在读取历史结果…";
    $("narratoSaveOutput").disabled = true; $("narratoArtifact").hidden = true; $("narratoRenderScript").hidden = true;
    setExecutionStatus(item.status || "running");
    void poll();
  }

  async function load() {
    try {
      const [catalog, config] = await Promise.all([api("/api/agent100"), api("/api/agent100/narrato/config")]);
      renderCatalog(catalog.agents || []);
      const defaults = config.models?.defaults || {}, options = config.models?.options || {};
      setSelect("narratoVisionModel", options.vision, defaults.vision);
      setSelect("narratoAsrModel", options.asr, defaults.asr);
      setSelect("narratoStoryModel", options.story, defaults.story);
      setSelect("narratoScriptModel", options.script, defaults.script);
      renderStyles(config.styles || []);
      state.ttsProviders = config.tts?.providers || {};
      const providers = Object.keys(state.ttsProviders);
      setSelect("narratoTtsProvider", providers, config.tts?.defaultProvider || providers[0] || "");
      selectTtsProvider($("narratoTtsProvider").value);
      renderCapabilities(config.capabilities || [], config.capabilityError || "");
      $("narratoMediaHint").textContent = config.mediaError ? `上传尚未配置：${config.mediaError}` : "";
      const connection = $("narratoConnection"); connection.className = `connection ${config.capabilityError ? "bad" : "ok"}`;
      connection.querySelector("span").textContent = config.capabilityError ? "AGENTK OFFLINE" : "READY";
      updatePreview();
      await loadHistory(true, true);
      scheduleHistoryPoll();
    } catch (error) {
      const connection = $("narratoConnection"); connection.className = "connection bad"; connection.querySelector("span").textContent = "OFFLINE";
      toast(`Narrato 初始化失败：${error.message}`, true);
    }
  }

  const MAX_VIDEO_DURATION_SECONDS = 60 * 60;

  function videoDuration(file) {
    return new Promise((resolve) => {
      const video = document.createElement("video"), url = URL.createObjectURL(file);
      const finish = (duration = null) => {
        URL.revokeObjectURL(url);
        video.removeAttribute("src");
        resolve(duration);
      };
      video.preload = "metadata";
      video.onloadedmetadata = () => finish(Number.isFinite(video.duration) ? video.duration : null);
      video.onerror = () => finish();
      video.src = url;
    });
  }

  async function upload(file = $("narratoVideoFile").files[0]) {
    if (!file) return toast("请先选择一个视频文件", true);
    if (state.uploading) return;
    state.uploading = true;
    const input = $("narratoVideoFile"); input.disabled = true;
    $("narratoUploadTitle").textContent = `${file.name} · 正在检查…`;
    try {
      const duration = await videoDuration(file);
      if (duration !== null && duration > MAX_VIDEO_DURATION_SECONDS) {
        throw new Error(`视频时长不能超过 1 小时（当前约 ${(duration / 60).toFixed(1)} 分钟）`);
      }
      const form = new FormData(); form.append("file", file);
      $("narratoUploadTitle").textContent = `${file.name} · 正在上传…`;
      const result = await api("/api/agent100/narrato/media", { method: "POST", body: form });
      $("narratoVideoUri").value = result.uri;
      $("narratoUploadTitle").textContent = `${file.name} · 已上传`;
      updatePreview(); toast("Narrato 视频上传完成");
    } catch (error) {
      input.value = "";
      $("narratoUploadTitle").textContent = `${file.name} · 上传失败，请重新选择`;
      toast(error.message, true);
    } finally {
      state.uploading = false;
      input.disabled = false;
    }
  }

  function setExecutionStatus(status = "idle") {
    $("narratoStatus").textContent = status.toUpperCase();
  }

  function setSubmitting(submitting) {
    state.submitting = submitting;
    $("narratoRun").disabled = submitting;
    $("narratoRun").classList.toggle("loading", submitting);
  }

  function showError(message) { $("narratoError").hidden = false; $("narratoError").textContent = message; }
  function hideError() { $("narratoError").hidden = true; $("narratoError").textContent = ""; }

  async function run() {
    if (state.submitting) return;
    hideError(); let body;
    try { body = requestBody(true); } catch (error) { showError(error.message); return toast(error.message, true); }
    setSubmitting(true);
    try {
      const started = await api("/api/agent100/narrato/executions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
      setSubmitting(false);
      openHistory({ ...started, pipeline: body.pipeline, title: body.title });
      void loadHistory(true, false);
      flashHistory();
      scheduleHistoryPoll();
      toast("已经在后台执行，到历史生成记录里去查看（已自动打开）；你可以继续创建新的生成记录");
    } catch (error) { showError(error.message); toast(error.message, true); }
    finally { if (state.submitting) setSubmitting(false); }
  }

  function renderEvents(events) {
    $("narratoEventCount").textContent = events.length;
    const root = $("narratoEvents"); root.replaceChildren();
    const timings = window.AgentKRequestLedger.eventTimings(events);
    events.forEach((event, index) => {
      const row = document.createElement("div"); row.className = "event-row";
      const kind = document.createElement("span"); kind.className = "event-kind"; kind.textContent = event.step || event.type;
      const data = document.createElement("span"); data.className = "event-data"; data.textContent = event.message || event.status || pretty(event.data || {});
      row.append(kind, data);
      const timing = timings[index] || {};
      if (timing.startedAt || timing.endedAt) {
        const times = document.createElement("span"); times.className = "event-time";
        if (timing.startedAt) {
          const started = document.createElement("time"); started.dateTime = timing.startedAt; started.title = timing.startedAt;
          started.textContent = `开始 ${formatTimestamp(timing.startedAt)}`; times.append(started);
        }
        if (timing.endedAt) {
          const ended = document.createElement("time"); ended.dateTime = timing.endedAt; ended.title = timing.endedAt;
          ended.textContent = `结束 ${formatTimestamp(timing.endedAt)}`; times.append(ended);
        }
        row.append(times);
      }
      root.append(row);
    });
    const latest = [...events].reverse().find(event => event.type === "progress" && !["request", "response"].includes(event.data?.kind));
    const percent = Math.max(0, Math.min(100, Number(latest?.data?.percent || 0)));
    $("narratoPercent").textContent = `${percent}%`; $("narratoProgressBar").style.width = `${percent}%`;
    $("narratoStage").textContent = latest ? `${latest.step} / ${latest.message}` : "EXECUTION ACCEPTED";
  }

  function renderOutput(output) {
    state.output = output; $("narratoOutputJson").textContent = pretty(output); $("narratoSaveOutput").disabled = false;
    const artifacts = Array.isArray(output?.artifacts) ? output.artifacts : [];
    const video = artifacts.find(item => item?.type === "video" && /^https?:\/\//.test(item.uri || ""));
    if (video) { $("narratoArtifact").href = video.uri; $("narratoArtifact").hidden = false; }
    $("narratoRenderScript").hidden = !(output?.script || output?.version === "2");
  }

  async function poll() {
    if (!state.sessionId || !state.executionId) return;
    const sessionId = state.sessionId, executionId = state.executionId;
    try {
      const result = await api(`/api/agent100/narrato/executions/${encodeURIComponent(sessionId)}/${encodeURIComponent(executionId)}`);
      if (state.sessionId !== sessionId || state.executionId !== executionId) return;
      const events = window.AgentKRequestLedger.withObservedRequestTimes(result.events || [], state.requestTimes);
      state.pollFailures = 0; state.entryRequest = result.request || state.entryRequest; renderRequests(state.entryRequest, events, result.createdAt); renderTaskInputActions(state.entryRequest, events); renderEvents(events); if (result.output != null) renderOutput(result.output); if (result.error) showError(result.error);
      if (result.statusSyncError) $("narratoStage").textContent = "显示缓存状态 · AgentK 连接恢复后将继续同步";
      void loadHistory(true, false);
      if (TERMINAL.has(result.status)) { setExecutionStatus(result.status); if (result.status === "completed") { $("narratoPercent").textContent = "100%"; $("narratoProgressBar").style.width = "100%"; } }
      else { setExecutionStatus(result.status || "running"); schedulePoll(); }
    } catch (error) {
      if (state.sessionId !== sessionId || state.executionId !== executionId) return;
      state.pollFailures += 1;
      $("narratoStage").textContent = `状态同步暂时中断，正在重试（${state.pollFailures}）`;
      if (state.pollFailures === 3) toast("Narrato 状态连接暂时中断，正在自动重试", true);
      schedulePoll();
    }
  }

  function schedulePoll(delay = POLL_INTERVAL_MS) { stopPolling(); state.timer = setTimeout(poll, delay); }
  function stopPolling() { if (state.timer) clearTimeout(state.timer); state.timer = null; }
  function sample() { $("narratoScriptJson").value = pretty({ version: "2", sourceVideoUri: "https://cdn.example/source.mp4", sourceDurationSeconds: 12, narrativeProfile: { primaryGenre: "纪录片", secondaryGenres: [], thesis: "雾如何改变山谷清晨的视野。", hook: "天亮了，山谷为什么仍像被藏起来？", style: "克制而有画面感", pacing: "舒缓，关键变化处加重", globalTtsInstruction: "沉稳、清晰、带轻微好奇。" }, narrationCues: [{ id: "cue-001", startSeconds: 1, endSeconds: 5, narration: "天已经亮了，山谷却还藏在缓慢退去的晨雾里。", evidenceWindowIndexes: [0], ttsInstruction: "开头略带疑问，后半句放缓。", importance: 5, emotion: "calm" }] }); updatePreview(); }
  function editOutput() { const script = state.output?.script || state.output; if (!script) return; $("narratoScriptJson").value = pretty(script); selectPipeline("documentary_render"); $("narratoRoot").scrollIntoView({ behavior: "smooth" }); }
  function save() { if (!state.output) return; const url = URL.createObjectURL(new Blob([pretty(state.output)], { type: "application/json" })); const link = document.createElement("a"); link.href = url; link.download = "narrato-output.json"; link.click(); URL.revokeObjectURL(url); }
  function switchPanel(panel) { $$('[data-narrato-panel]').forEach(button => button.classList.toggle("active", button.dataset.narratoPanel === panel)); $$(".narrato-console-panel").forEach(node => node.classList.toggle("active", node.id === `narratoPanel${panel[0].toUpperCase()}${panel.slice(1)}`)); }

  function bind() {
    $$('[data-narrato-pipeline]').forEach(button => button.addEventListener("click", () => selectPipeline(button.dataset.narratoPipeline)));
    $("narratoRoot").querySelectorAll("input,textarea,select").forEach(input => { input.addEventListener("input", updatePreview); input.addEventListener("change", updatePreview); });
    $("narratoVolume").addEventListener("input", () => $("narratoVolumeOutput").textContent = Number($("narratoVolume").value).toFixed(1));
    $("narratoOriginalVolume").addEventListener("input", () => { state.originalVolumeTouched = true; $("narratoOriginalVolumeOutput").textContent = Number($("narratoOriginalVolume").value).toFixed(2); });
    $("narratoTtsProvider").addEventListener("change", () => selectTtsProvider($("narratoTtsProvider").value));
    $("narratoVideoFile").addEventListener("change", () => { const file = $("narratoVideoFile").files[0]; if (file) { $("narratoVideoUri").value = ""; updatePreview(); upload(file); } });
    const zone = $("narratoUploadZone"); ["dragenter", "dragover"].forEach(name => zone.addEventListener(name, event => { event.preventDefault(); event.stopPropagation(); zone.classList.add("dragging"); }));
    ["dragleave", "drop"].forEach(name => zone.addEventListener(name, event => { event.preventDefault(); event.stopPropagation(); zone.classList.remove("dragging"); }));
    zone.addEventListener("drop", event => { const file = event.dataTransfer.files[0]; if (!file) return; const transfer = new DataTransfer(); transfer.items.add(file); $("narratoVideoFile").files = transfer.files; $("narratoUploadTitle").textContent = file.name; upload(); });
    $("narratoRun").addEventListener("click", run);
    $("narratoRefreshHistory").addEventListener("click", () => loadHistory());
    $("narratoSampleScript").addEventListener("click", sample); $("narratoRenderScript").addEventListener("click", editOutput); $("narratoSaveOutput").addEventListener("click", save);
    $("narratoInputParams").addEventListener("click", toggleTaskInput); $("narratoCloseInput").addEventListener("click", () => collapseTaskInput(true));
    $("narratoScriptFile").addEventListener("change", async () => { const file = $("narratoScriptFile").files[0]; if (file) { $("narratoScriptJson").value = await file.text(); updatePreview(); } });
    $$('[data-narrato-panel]').forEach(button => button.addEventListener("click", () => switchPanel(button.dataset.narratoPanel)));
    window.addEventListener("beforeunload", () => { stopPolling(); if (state.historyTimer) clearTimeout(state.historyTimer); });
  }

  window.AgentKNarrato = { buildArgs, requestBody, selectPipeline };
  bind(); selectPipeline(state.pipeline); load();
})();
