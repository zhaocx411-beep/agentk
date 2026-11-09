(() => {
  "use strict";
  const state = {
    mode: "capabilities", capabilities: [], filtered: [], capability: null,
    raw: false, capArgs: {}, capRun: null, capTimer: null, capPollFailures: 0, capRunning: false,
    agents: [], agent: null, session: null, agentStream: null, agentPollFailures: 0, agentSnapshot: "",
    uploads: [], sending: false, uploading: false, agentRunning: false,
    pendingAsks: [], answeringAsk: "", answeredAsks: new Set(),
  };
  const $ = (id) => document.getElementById(id);
  const terminal = (status = "") => /COMPLETED|FAILED|CANCELLED/i.test(status);
  const { defaultsFor, exampleFor, mergeUploadedUrls, selectOptionsFor, uploadAcceptFor } = window.AgentKFormDefaults;
  const { validateCapabilityArgs } = window.AgentKSchemaValidation;
  const { capabilityPollView } = window.AgentKRunState;
  const { composerActionsBlocked, messageInputBlocked } = window.AgentKComposerState;
  const { mediaArtifactsFor } = window.AgentKResultMedia;
  const { renderMarkdown } = window.AgentKMarkdown;

  async function api(path, options = {}) {
    const response = await fetch(path, { cache: "no-store", ...options });
    let body = null;
    try { body = await response.json(); } catch { body = null; }
    if (!response.ok) throw new Error(body?.detail || body?.error || `HTTP ${response.status}`);
    return body;
  }
  function toast(message, error = false) {
    const node = document.createElement("div"); node.className = "toast" + (error ? " error" : ""); node.textContent = message;
    $("toasts").append(node); setTimeout(() => node.remove(), 4500);
  }
  function esc(value) { const node = document.createElement("span"); node.textContent = String(value ?? ""); return node.innerHTML; }
  function pretty(value) { try { return JSON.stringify(value, null, 2); } catch { return String(value); } }

  async function init() {
    bind();
    const config = await api("/api/config"); $("gatewayAddress").textContent = config.agentkAddress;
    const [connection, agents] = await Promise.all([api("/api/connection"), api("/api/agents")]);
    setConnection(connection); state.agents = agents.agents || []; $("agentCount").textContent = state.agents.length; renderAgents();
    selectAgent(state.agents[0]);
    try {
      const result = await api("/api/capabilities"); state.capabilities = result.capabilities || []; state.filtered = state.capabilities; $("capabilityCount").textContent = result.count; renderCapabilities();
      if (state.capabilities.length) selectCapability(state.capabilities[0]);
    } catch (error) {
      $("capabilityList").innerHTML = `<div class="catalog-item"><b>CATALOG OFFLINE</b><small>${esc(error.message)}</small></div>`;
      toast(error.message, true);
    }
  }
  function setConnection(result) {
    const node = $("connection"); node.className = "connection " + (result.ok ? "ok" : "bad");
    node.querySelector("span").textContent = result.ok ? (result.identity?.displayName || "CONNECTED") : "OFFLINE";
    if (!result.ok) toast(result.error || "AgentK offline", true);
  }
  function bind() {
    document.querySelectorAll(".mode-tabs button").forEach(btn => btn.addEventListener("click", () => switchMode(btn.dataset.mode)));
    $("capabilitySearch").addEventListener("input", (event) => { const q = event.target.value.trim().toLowerCase(); state.filtered = state.capabilities.filter(item => `${item.id} ${item.description || ""}`.toLowerCase().includes(q)); renderCapabilities(); });
    $("rawModeButton").addEventListener("click", toggleRaw);
    $("resetDefaultsButton").addEventListener("click", resetCapabilityExamples);
    $("capabilityForm").addEventListener("submit", runCapability);
    $("cancelCapability").addEventListener("click", cancelCapability);
    $("composer").addEventListener("submit", sendMessage);
    $("messageInput").addEventListener("keydown", event => { if (event.key === "Enter" && !event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey && !event.isComposing) { event.preventDefault(); $("composer").requestSubmit(); } });
    $("chatUpload").addEventListener("change", event => uploadChatFiles(event.target.files));
    $("newChat").addEventListener("click", resetChat);
    $("cancelAgent").addEventListener("click", cancelAgent);
    const carriesFiles = event => [...(event.dataTransfer?.types || [])].includes("Files");
    document.addEventListener("dragenter", event => { if (state.mode === "agents" && carriesFiles(event)) { event.preventDefault(); document.body.classList.add("drag-over"); } });
    document.addEventListener("dragover", event => { if (state.mode === "agents" && carriesFiles(event)) { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; } });
    document.addEventListener("dragleave", event => { if (event.relatedTarget === null) document.body.classList.remove("drag-over"); });
    document.addEventListener("drop", event => { if (state.mode === "agents" && carriesFiles(event)) { event.preventDefault(); document.body.classList.remove("drag-over"); uploadChatFiles(event.dataTransfer.files); } });
  }
  function switchMode(mode) {
    state.mode = mode; document.querySelectorAll(".mode-tabs button").forEach(b => b.classList.toggle("active", b.dataset.mode === mode));
    document.querySelectorAll(".workspace").forEach(w => w.classList.remove("active")); $(`${mode}Workspace`).classList.add("active");
  }

  function renderCapabilities() {
    const list = $("capabilityList"); list.replaceChildren();
    state.filtered.forEach(item => {
      const button = document.createElement("button"); button.className = "catalog-item" + (state.capability?.id === item.id ? " active" : "");
      const source = String(item.source || "").replace("CAPABILITY_SOURCE_", "") || "UNKNOWN";
      button.innerHTML = `<b>${esc(item.id)}</b><small>${esc(item.description || "No description")}</small><span class="item-meta">${esc(source)} · ${Object.keys(item.inputSchema?.properties || {}).length} INPUTS</span>`;
      button.addEventListener("click", () => selectCapability(item)); list.append(button);
    });
    if (!state.filtered.length) list.innerHTML = '<div class="catalog-item"><small>没有匹配的 capability</small></div>';
  }
  function selectCapability(item) {
    clearInterval(state.capTimer); state.capability = item; state.capArgs = defaultsFor(item.inputSchema || {}, item.id); state.raw = false; state.capRun = null; state.capPollFailures = 0; setCapabilityRunning(false);
    $("capabilityEmpty").hidden = true; $("capabilityDetail").hidden = false; $("capabilityRun").hidden = true; $("capabilityResult").hidden = true; $("cancelCapability").hidden = true; renderCapabilityValidation([]);
    $("capabilityName").textContent = item.id; $("capabilityDescription").textContent = item.description || "No description supplied by manifest.";
    $("capabilitySource").textContent = String(item.source || "CAPABILITY").replace("CAPABILITY_SOURCE_", "") + " CAPABILITY";
    $("rawModeButton").textContent = "{ } RAW JSON"; $("schemaForm").hidden = false; $("rawEditor").hidden = true; renderSchema(item.inputSchema || { type: "object" }); renderCapabilities();
  }
  function resetCapabilityExamples() {
    if (!state.capability) return;
    state.capArgs = defaultsFor(state.capability.inputSchema || {}, state.capability.id);
    renderCapabilityValidation([]);
    if (state.raw) $("rawEditor").value = pretty(cleanArgs(state.capArgs));
    else renderSchema(state.capability.inputSchema || {});
    toast("已恢复 schema 默认值和必填参数示例");
  }
  function renderSchema(schema) {
    const root = $("schemaForm"); root.replaceChildren(); const properties = schema.properties || {};
    if (!Object.keys(properties).length) root.innerHTML = '<div class="field wide"><small>此 capability 没有声明输入字段，可以直接运行。</small></div>';
    Object.entries(properties).forEach(([name, child]) => root.append(makeField(name, child, (schema.required || []).includes(name))));
    $("rawEditor").value = pretty(cleanArgs(state.capArgs));
  }
  function attachUploadControl(wrap, input, name, schema, update) {
    const accept = uploadAcceptFor(name, schema);
    if (!accept) return false;
    const multiple = schema.type === "array";
    const row = document.createElement("div"); row.className = "file-row"; row.append(input);
    const pick = document.createElement("label"); pick.className = "file-trigger"; pick.innerHTML = `${multiple ? "UPLOAD FILES" : "UPLOAD"}<input type="file">`;
    const fileInput = pick.querySelector("input"); fileInput.accept = accept; fileInput.multiple = multiple;
    fileInput.addEventListener("change", async event => {
      const files = [...event.target.files]; if (!files.length) return;
      fileInput.disabled = true; const uploaded = []; let failure = null;
      try {
        for (const file of files) { try { uploaded.push(await uploadFile(file)); } catch (error) { failure = error; break; } }
        if (uploaded.length) {
          let current = input.value;
          if (multiple) { try { current = JSON.parse(input.value || "[]"); } catch { current = []; } }
          const urls = uploaded.map(item => item.url);
          input.value = multiple ? pretty(mergeUploadedUrls(current, urls, true)) : mergeUploadedUrls(current, urls, false);
          update();
        }
        if (failure) toast(`${uploaded.length ? `已上传 ${uploaded.length} 个；` : ""}${failure.message}`, true);
        else toast(uploaded.find(item => item.warning)?.warning || `已上传 ${uploaded.length} 个文件`);
      } finally { fileInput.disabled = false; fileInput.value = ""; }
    });
    row.append(pick); wrap.append(row); return true;
  }
  function makeField(name, schema, required) {
    const wrap = document.createElement("div"); const complex = ["object","array"].includes(schema.type) || schema.properties;
    wrap.className = "field" + (complex || /prompt|content|text|script|description/i.test(name) ? " wide" : "");
    const label = document.createElement("label"); label.innerHTML = `<span>${esc(schema.title || name)} ${required ? "<i>*</i>" : ""}</span><code>${esc(schema.type || "any")}</code>`; wrap.append(label);
    let input;
    const selectOptions = selectOptionsFor(schema);
    if (selectOptions) {
      input = document.createElement("select");
      if (!required) input.append(new Option("— 未设置 —", ""));
      if (!selectOptions.length) {
        input.append(new Option(schema["x-options-unavailable"] || "无可用选项", ""));
      } else {
        selectOptions.forEach(option => input.append(new Option(option.label, option.value)));
        if (required && !state.capArgs[name]) state.capArgs[name] = selectOptions[0].value;
      }
      input.value = state.capArgs[name] ?? "";
    } else if (schema.type === "boolean") { input = document.createElement("input"); input.type = "checkbox"; input.checked = Boolean(state.capArgs[name]); }
    else if (complex) { input = document.createElement("textarea"); input.value = state.capArgs[name] ? pretty(state.capArgs[name]) : schema.type === "array" ? "[]" : "{}"; input.spellcheck = false; }
    else if (schema.type === "integer" || schema.type === "number") { input = document.createElement("input"); input.type = "number"; input.step = schema.type === "integer" ? "1" : "any"; if (schema.minimum !== undefined) input.min = schema.minimum; if (schema.maximum !== undefined) input.max = schema.maximum; input.value = state.capArgs[name] ?? schema.default ?? ""; input.placeholder = String(exampleFor(schema, {capabilityId: state.capability.id, name}) ?? ""); }
    else { input = /prompt|content|text|script|description/i.test(name) ? document.createElement("textarea") : document.createElement("input"); if (input.tagName === "INPUT") input.type = "text"; input.value = state.capArgs[name] ?? schema.default ?? ""; input.placeholder = schema.examples?.[0] ?? exampleFor(schema, {capabilityId: state.capability.id, name}) ?? schema.format ?? ""; }
    input.required = required; input.dataset.name = name;
    if (schema.minLength !== undefined) input.minLength = schema.minLength;
    if (schema.maxLength !== undefined) input.maxLength = schema.maxLength;
    if (schema.pattern && input.tagName === "INPUT") input.pattern = schema.pattern;
    const update = () => { try { state.capArgs[name] = input.type === "checkbox" ? input.checked : complex ? JSON.parse(input.value || (schema.type === "array" ? "[]" : "{}")) : schema.type === "integer" ? (input.value === "" ? "" : parseInt(input.value,10)) : schema.type === "number" ? (input.value === "" ? "" : Number(input.value)) : input.value; input.setCustomValidity(""); renderCapabilityValidation([]); } catch { input.setCustomValidity("请输入有效 JSON"); } };
    input.addEventListener("input", update); input.addEventListener("change", update);
    if (!attachUploadControl(wrap, input, name, schema, update)) wrap.append(input);
    if (schema.description) { const help = document.createElement("small"); help.textContent = schema.description; wrap.append(help); }
    if (schema["x-contract-warning"]) { const warning = document.createElement("small"); warning.className = "contract-warning"; warning.textContent = `CONTRACT GAP · ${schema["x-contract-warning"]}`; wrap.append(warning); }
    return wrap;
  }
  function cleanArgs(value) { if (Array.isArray(value)) return value.map(cleanArgs); if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).filter(([,v]) => v !== "" && v !== undefined).map(([k,v]) => [k,cleanArgs(v)])); return value; }
  function renderCapabilityValidation(errors) {
    const root = $("capabilityValidation");
    document.querySelectorAll("#schemaForm .field.invalid").forEach(node => node.classList.remove("invalid"));
    root.hidden = errors.length === 0; root.replaceChildren();
    if (!errors.length) return;
    const title = document.createElement("b"); title.textContent = `提交前发现 ${errors.length} 个参数问题`;
    const list = document.createElement("ul");
    errors.forEach(error => { const item = document.createElement("li"); item.textContent = error.message; list.append(item); });
    root.append(title, list);
    const names = new Set(errors.map(error => String(error.path || "").split(/[.[]/, 1)[0]).filter(Boolean));
    document.querySelectorAll("#schemaForm [data-name]").forEach(input => { if (names.has(input.dataset.name)) input.closest(".field")?.classList.add("invalid"); });
  }
  function toggleRaw() {
    state.raw = !state.raw; if (state.raw) { $("rawEditor").value = pretty(cleanArgs(state.capArgs)); } else { try { state.capArgs = JSON.parse($("rawEditor").value); renderSchema(state.capability.inputSchema || {}); } catch (error) { toast("RAW JSON 无效：" + error.message,true); return; } }
    $("schemaForm").hidden = state.raw; $("rawEditor").hidden = !state.raw; $("rawModeButton").textContent = state.raw ? "FORM MODE" : "{ } RAW JSON";
  }
  async function runCapability(event) {
    event.preventDefault(); if (state.capRunning) return; let args;
    try { args = state.raw ? JSON.parse($("rawEditor").value) : cleanArgs(state.capArgs); } catch (error) { toast("JSON 无效：" + error.message,true); return; }
    const validationErrors = validateCapabilityArgs(state.capability.inputSchema || {}, args, state.capability.id);
    renderCapabilityValidation(validationErrors);
    if (validationErrors.length) { toast(`参数校验未通过：${validationErrors[0].message}`, true); $("capabilityValidation").scrollIntoView({behavior:"smooth", block:"nearest"}); return; }
    $("capabilityResult").hidden = true; $("capabilityOutputError").hidden = true; $("capabilityArtifacts").replaceChildren(); $("capabilityResultJson").textContent = ""; renderEvents($("capabilityEvents"), []); setCapabilityRunning(true);
    try { const result = await api(`/api/capabilities/${encodeURIComponent(state.capability.id)}/runs`, { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({args}) }); state.capRun = result.run; state.capPollFailures = 0; $("capabilityRun").hidden = false; $("cancelCapability").hidden = false; $("capabilityRunId").textContent = result.run.executionId; renderCapabilityPollView(capabilityPollView()); await pollCapability(); clearInterval(state.capTimer); if (state.capRunning) state.capTimer = setInterval(pollCapability, 1200); } catch (error) { setCapabilityRunning(false); toast(error.message,true); }
  }
  function setCapabilityRunning(running) {
    state.capRunning = running;
    const submit = $("capabilityForm").querySelector('button[type="submit"]');
    submit.disabled = running; submit.setAttribute("aria-busy", String(running));
    submit.innerHTML = running ? '<span class="spin">◌</span> GENERATING…' : '<span>▶</span> RUN CAPABILITY';
  }
  function renderCapabilityPollView(view) {
    const status = $("capabilityRunStatus"); status.textContent = view.label;
    status.classList.toggle("syncing", state.capPollFailures > 0 && !view.unavailable);
    status.title = state.capPollFailures > 0 && !view.unavailable ? `正在同步执行状态（重试 ${state.capPollFailures}/5）` : "";
    setCapabilityRunning(view.active);
  }
  function renderCapabilityOutput(output) {
    const firstRender = $("capabilityResult").hidden;
    const artifacts = mediaArtifactsFor(output);
    $("capabilityResult").hidden = false;
    $("capabilityOutputError").hidden = true;
    $("capabilityArtifacts").hidden = artifacts.length === 0;
    $("capabilityArtifacts").innerHTML = artifacts.map(renderArtifact).join("");
    $("capabilityArtifactCount").textContent = artifacts.length ? `${artifacts.length} ARTIFACT${artifacts.length > 1 ? "S" : ""}` : "STRUCTURED DATA";
    $("capabilityResultJson").textContent = pretty(output);
    if (firstRender) requestAnimationFrame(() => $("capabilityResult").scrollIntoView({behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "nearest"}));
  }
  function renderCapabilityFailure(error) {
    const firstRender = $("capabilityResult").hidden;
    $("capabilityResult").hidden = false; $("capabilityArtifacts").hidden = true;
    $("capabilityArtifactCount").textContent = "FAILED";
    $("capabilityOutputError").hidden = false; $("capabilityOutputError").textContent = error || "Capability 执行失败，未返回产物。";
    $("capabilityResultJson").textContent = pretty({error: error || "Capability execution failed"});
    if (firstRender) requestAnimationFrame(() => $("capabilityResult").scrollIntoView({behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "nearest"}));
  }
  async function pollCapability() {
    if (!state.capRun) return; try { const data = await api(`/api/runs/${state.capRun.executionId}`); state.capPollFailures = 0; const status = data.execution?.status || "RUNNING"; const view = capabilityPollView(status, 0); renderCapabilityPollView(view); renderEvents($("capabilityEvents"), data.events || []); const output = data.execution?.pipeline?.output; if (output !== undefined) renderCapabilityOutput(output); else if (view.label === "FAILED") renderCapabilityFailure(data.execution?.error); if (view.terminal) { clearInterval(state.capTimer); $("cancelCapability").hidden = true; } } catch (error) { state.capPollFailures += 1; const view = capabilityPollView("", state.capPollFailures); renderCapabilityPollView(view); if (view.unavailable) { clearInterval(state.capTimer); toast(error.message,true); } }
  }
  async function cancelCapability() { if (!state.capRun) return; try { await api(`/api/runs/${state.capRun.executionId}`, {method:"DELETE"}); toast("已发送取消请求"); await pollCapability(); } catch (e) { toast(e.message,true); } }
  function eventPayload(event) { const ignored = new Set(["eventId","executionId","roundId","createdAt"]); return Object.entries(event).find(([key]) => !ignored.has(key)) || ["event",event]; }
  function renderEvents(root, events) { root.innerHTML = events.map(event => { const [kind,data] = eventPayload(event); return `<div class="event-row"><span class="event-kind">${esc(kind)}</span><span class="event-data">${esc(pretty(data))}</span></div>`; }).join("") || '<div class="event-row"><span class="event-kind">WAITING</span><span class="event-data">等待事件提交…</span></div>'; root.scrollTop = root.scrollHeight; }

  function renderAgents() {
    const root = $("agentList"); root.replaceChildren(); state.agents.forEach(agent => { const button = document.createElement("button"); button.className = "catalog-item" + (state.agent?.id === agent.id ? " active" : ""); button.style.setProperty("--item-accent", agent.accent); button.innerHTML = `<span class="agent-mini">${esc(agent.icon)}</span><b>${esc(agent.name)}</b><small>${esc(agent.description)}</small><span class="item-meta">${esc(agent.id)}</span>`; button.addEventListener("click",()=>selectAgent(agent)); root.append(button); });
  }
  function selectAgent(agent) { if (!agent) return; stopAgentStream(); state.agent = agent; resetChat(false); document.documentElement.style.setProperty("--agent-accent",agent.accent); $("agentIcon").textContent = agent.icon; $("agentName").textContent = agent.name; $("agentDescription").textContent = agent.description; const starters=$("starterGrid"); starters.replaceChildren(); (agent.starters||[]).forEach(text=>{const b=document.createElement("button");b.type="button";b.textContent=text;b.onclick=()=>{$("messageInput").value=text;$("messageInput").focus();};starters.append(b);}); renderAgents(); }
  function composerBlocked() { return composerActionsBlocked(state); }
  function refreshComposerControls() {
    const blocked = composerBlocked();
    $("messageInput").disabled = messageInputBlocked(state);
    $("chatUpload").disabled = blocked;
    $("composer").querySelector(".send").disabled = blocked;
    $("chatUploadButton").classList.toggle("disabled", blocked);
    $("uploadHint").textContent = state.uploading ? "正在上传素材…" : state.pendingAsks.length ? "请先回答 Agent 的问题" : state.agentRunning ? "Agent 正在执行，请等待本轮结束" : "视频 ≤100MB；其他文件 ≤20MB，支持点击或拖入";
  }
  function clearUploads() {
    state.uploads.forEach(item => { if (item.previewUrl?.startsWith("blob:")) URL.revokeObjectURL(item.previewUrl); });
    state.uploads = [];
    renderAttachmentStrip();
  }
  function resetChat(notify=true) {
    stopAgentStream(); state.session=null; state.agentPollFailures=0; state.agentSnapshot="";
    state.sending=false; state.uploading=false; state.agentRunning=false; state.pendingAsks=[]; state.answeringAsk=""; state.answeredAsks.clear();
    clearUploads(); $("chatUpload").value="";
    $("chatFeed").innerHTML='<div class="chat-zero"><span>READY</span><p>从下方输入一条消息开始本地测试。</p></div>';
    $("askZone").replaceChildren(); $("cancelAgent").hidden=true; refreshComposerControls();
    if(notify) toast("已清空本地会话");
  }
  async function ensureSession(firstText) { if (state.session) return; const result=await api("/api/agent-sessions",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({agentId:state.agent.id,title:(firstText||state.agent.name).slice(0,48)})}); state.session=result.session; }
  async function sendMessage(event) {
    event.preventDefault(); if(composerBlocked())return;
    const input=$("messageInput"), text=input.value.trim();
    const attachments=state.uploads.filter(item=>item.url&&!item.error).map(item=>({uri:item.url,name:item.name,type:item.type,mimeType:item.mimeType}));
    if(!text&&!attachments.length)return;
    state.sending=true; state.agentRunning=true; state.answeredAsks.clear(); refreshComposerControls();
    try {
      await ensureSession(text || attachments[0]?.name || state.agent.name);
      const result=await api(`/api/agent-sessions/${state.session.sessionId}/messages`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({text,attachments})});
      state.session=result.session; state.agentPollFailures=0; input.value="";
      const failed=state.uploads.filter(item=>item.error); clearUploads(); state.uploads=failed; renderAttachmentStrip();
      $("cancelAgent").hidden=false; state.agentSnapshot=""; startAgentStream();
    } catch(error){state.agentRunning=false;toast(error.message,true);}
    finally {state.sending=false;refreshComposerControls();}
  }
  function stopAgentStream() { if(state.agentStream){state.agentStream.close();state.agentStream=null;} }
  function startAgentStream() {
    stopAgentStream();if(!state.session)return;
    const source=new EventSource(`/api/agent-sessions/${encodeURIComponent(state.session.sessionId)}/events`);state.agentStream=source;
    source.onopen=()=>{if(source===state.agentStream)state.agentPollFailures=0;};
    source.addEventListener("snapshot",event=>{
      if(source!==state.agentStream)return;
      let data;try{data=JSON.parse(event.data);}catch{toast("Agent 状态数据无效",true);return;}
      state.agentPollFailures=0;state.session=data.session;
      const status=data.execution?.status||"";state.agentRunning=Boolean(status&&!terminal(status));
      if(event.data!==state.agentSnapshot){state.agentSnapshot=event.data;renderChat(data.events||[],data.execution||{});}
      $("cancelAgent").hidden=!status||terminal(status);if(terminal(status))stopAgentStream();refreshComposerControls();
    });
    source.addEventListener("agent-error",event=>{
      if(source!==state.agentStream)return;let detail="Agent 状态连接失败";try{detail=JSON.parse(event.data).error||detail;}catch{}
      state.agentRunning=false;stopAgentStream();toast(detail,true);refreshComposerControls();
    });
    source.onerror=()=>{if(source!==state.agentStream)return;state.agentPollFailures+=1;if(state.agentPollFailures===3)toast("Agent 状态连接中断，正在重连…",true);};
  }
  function renderChat(events,execution={}) {
    const root=$("chatFeed"), messages=[], tools=new Map(), asks=new Map(); let assistant=null;
    events.forEach(event=>{const [kind,data]=eventPayload(event);
      if(kind==="userInput"){asks.clear();messages.push({role:"user",text:data.text||"",attachments:data.attachments||[]});assistant=null;}
      else if(kind==="roundStarted"){assistant={role:"assistant",text:"",reasoning:"",tools:[],artifacts:[]};messages.push(assistant);}
      else if(kind==="assistantText"){if(!assistant){assistant={role:"assistant",text:"",reasoning:"",tools:[],artifacts:[]};messages.push(assistant);} assistant.text+=(data.text||"");}
      else if(kind==="assistantReasoning"){if(!assistant){assistant={role:"assistant",text:"",reasoning:"",tools:[],artifacts:[]};messages.push(assistant);} assistant.reasoning+=(data.text||"");}
      else if(kind==="assistantToolCall"){if(!assistant){assistant={role:"assistant",text:"",reasoning:"",tools:[],artifacts:[]};messages.push(assistant);} const t={id:data.toolCallId,name:data.toolName,status:"pending",args:data.args};assistant.tools.push(t);tools.set(t.id,t);}
      else if(kind==="toolExecStarted"){const t=tools.get(data.toolCallId);if(t)t.status="running";}
      else if(kind==="toolExecSucceeded"){const t=tools.get(data.toolCallId);if(t){t.status="completed";t.result=data.result;} if(assistant)assistant.artifacts.push(...(data.artifacts||[]),...(data.result?.artifacts||[]));}
      else if(kind==="toolExecFailed"){const t=tools.get(data.toolCallId);if(t){t.status="failed";t.error=data.error;}}
      else if(kind==="askUserRequested"){if(!state.answeredAsks.has(data.toolCallId))asks.set(data.toolCallId,data);const t=tools.get(data.toolCallId);if(t)t.status="waiting-user";}
      else if(kind==="askUserResponded"){asks.delete(data.toolCallId);state.answeredAsks.delete(data.toolCallId);const t=tools.get(data.toolCallId);if(t)t.status="completed";}
      else if(kind==="invocationProgress"){if(!assistant){assistant={role:"assistant",text:"",reasoning:"",tools:[],artifacts:[]};messages.push(assistant);} assistant.progress=data.message||data.stepId;}
    });
    state.pendingAsks=[...asks.values()];
    root.innerHTML=messages.map(renderMessage).join("")||'<div class="chat-zero"><span>RUN</span><p>等待 AgentK 提交事件…</p></div>';
    if(execution.error)root.insertAdjacentHTML("beforeend",`<div class="toast error">${esc(execution.error)}</div>`);
    renderAsks(state.pendingAsks); root.scrollTop=root.scrollHeight;
  }
  function renderMessage(msg){const tools=(msg.tools||[]).map(t=>`<div class="tool-card"><header><b>${esc(t.name)}</b><span>${esc(t.status)}</span></header><pre>${esc(pretty(t.error||t.result||t.args||{}))}</pre></div>`).join("");const arts=(msg.artifacts||[]).filter((a,i,all)=>a.uri&&all.findIndex(x=>x.uri===a.uri)===i).map(renderArtifact).join("");const text=msg.role==="assistant"?`<div class="markdown-body">${renderMarkdown(msg.text||"")}</div>`:esc(msg.text||"");return `<article class="message ${msg.role}"><div class="message-head">${msg.role==="user"?"USER INPUT":"AGENTK"}</div><div class="bubble">${text}${msg.reasoning?`<details><summary>REASONING</summary><div class="reasoning">${esc(msg.reasoning)}</div></details>`:""}${msg.progress?`<div class="reasoning">${esc(msg.progress)}</div>`:""}${tools}${arts?`<div class="artifact-grid">${arts}</div>`:""}</div></article>`;}
  function renderArtifact(a){const uri=esc(a.uri),type=(a.kind||a.type||"").toLowerCase(),description=esc(a.description||"");let media="";if(type.includes("image")||/\.(png|jpe?g|webp|gif)(\?|$)/i.test(a.uri))media=`<img src="${uri}" alt="${description||"image artifact"}" loading="lazy">`;else if(type.includes("video")||/\.(mp4|webm|mov)(\?|$)/i.test(a.uri))media=`<video src="${uri}" controls preload="metadata"></video>`;else if(type.includes("audio")||/\.(mp3|wav|m4a|aac|ogg)(\?|$)/i.test(a.uri))media=`<audio src="${uri}" controls preload="metadata"></audio>`;else media='<div class="artifact-file">FILE</div>';return `<article class="artifact">${media}<div class="artifact-meta"><span>${esc(a.type||a.kind||"artifact")}</span><a href="${uri}" target="_blank" rel="noreferrer">OPEN ↗</a></div>${description?`<p>${description}</p>`:""}</article>`;}
  function renderAsks(asks){
    const root=$("askZone");root.replaceChildren();const ask=asks.at(-1);if(!ask)return;
    const form=document.createElement("form"),title=document.createElement("h3");title.textContent="AGENT 需要你的选择";form.append(title);
    (ask.questions||[]).forEach((q,index)=>{
      const field=document.createElement("fieldset");field.className="question";field.dataset.id=q.id;
      const legend=document.createElement("legend");legend.textContent=q.label?`${q.label} · ${q.question}`:q.question;field.append(legend);
      (q.options||[]).forEach((opt,optIndex)=>{const label=document.createElement("label"),input=document.createElement("input");input.type=q.multiSelect?"checkbox":"radio";input.name=`q_${index}`;input.value=opt;input.disabled=state.answeringAsk===ask.toolCallId;input.id=`q_${index}_${optIndex}`;label.htmlFor=input.id;label.append(input,document.createTextNode(opt));field.append(label);});
      const custom=document.createElement("input");custom.type="text";custom.className="ask-custom";custom.placeholder="其他答案（可填写）";custom.dataset.questionId=q.id;custom.disabled=state.answeringAsk===ask.toolCallId;field.append(custom);form.append(field);
    });
    const error=document.createElement("p");error.className="ask-error";error.hidden=true;
    const submit=document.createElement("button");submit.className="primary";submit.textContent=state.answeringAsk===ask.toolCallId?"SUBMITTING…":"CONFIRM ANSWERS";submit.disabled=state.answeringAsk===ask.toolCallId;form.append(error,submit);
    form.onsubmit=event=>submitAskAnswers(event,ask);root.append(form);
  }
  async function submitAskAnswers(event,ask){
    event.preventDefault();const form=event.currentTarget,answers=[];
    for(const field of form.querySelectorAll(".question")){
      const custom=field.querySelector(".ask-custom"),customValue=custom.value.trim();let selected=[...field.querySelectorAll("input:checked")].map(input=>input.value);
      const multi=field.querySelector('input[type="checkbox"]');if(customValue)selected=multi?[...selected,customValue]:[customValue];
      if(!selected.length){const error=form.querySelector(".ask-error");error.textContent="请回答每个问题后再提交。";error.hidden=false;(custom||field.querySelector("input"))?.focus();return;}
      answers.push({questionId:field.dataset.id,selected});
    }
    state.answeringAsk=ask.toolCallId;renderAsks(state.pendingAsks);
    try{
      const result=await api(`/api/agent-sessions/${state.session.sessionId}/answers`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({toolCallId:ask.toolCallId,answers})});
      state.session=result.session;state.answeredAsks.add(ask.toolCallId);state.pendingAsks=[];state.agentRunning=true;state.agentPollFailures=0;$("askZone").replaceChildren();refreshComposerControls();
      state.agentSnapshot="";startAgentStream();
    }catch(error){toast(error.message,true);}finally{state.answeringAsk="";renderAsks(state.pendingAsks);refreshComposerControls();}
  }
  async function uploadFile(file){const form=new FormData();form.append("file",file);return api("/api/uploads",{method:"POST",body:form});}
  async function uploadChatFiles(files){
    if(!files?.length||composerBlocked())return;
    const added=[...files].map(file=>({file,name:file.name,mimeType:file.type||"application/octet-stream",type:(file.type||"file").split("/")[0],url:"",previewUrl:file.type.startsWith("image/")?URL.createObjectURL(file):"",error:""}));
    state.uploads.push(...added);state.uploading=true;renderAttachmentStrip();refreshComposerControls();
    await Promise.all(added.map(async item=>{try{const uploaded=await uploadFile(item.file);if(item.previewUrl?.startsWith("blob:"))URL.revokeObjectURL(item.previewUrl);Object.assign(item,uploaded,{previewUrl:uploaded.type==="image"?(uploaded.previewUrl||uploaded.url):""});if(uploaded.warning)toast(uploaded.warning);}catch(error){item.error=error.message;toast(`${item.name}: ${error.message}`,true);}finally{delete item.file;renderAttachmentStrip();}}));
    state.uploading=false;$("chatUpload").value="";refreshComposerControls();
  }
  function renderAttachmentStrip(){
    const root=$("attachmentStrip");root.replaceChildren();state.uploads.forEach((item,index)=>{const node=document.createElement("div");node.className="attachment-chip"+(item.error?" error":"");
      if(item.previewUrl){const img=document.createElement("img");img.src=item.previewUrl;img.alt=item.name;node.append(img);}else{const icon=document.createElement("span");icon.textContent=(item.type||"file").toUpperCase().slice(0,3);node.append(icon);}
      const copy=document.createElement("span");copy.className="attachment-copy";const name=document.createElement("b");name.textContent=item.name;const status=document.createElement("span");status.textContent=item.error?`失败：${item.error}`:item.url?"已上传":"上传中…";copy.append(name,status);
      const remove=document.createElement("button");remove.type="button";remove.textContent="×";remove.title="移除";remove.disabled=!item.url&&!item.error;remove.onclick=()=>{if(item.previewUrl?.startsWith("blob:"))URL.revokeObjectURL(item.previewUrl);state.uploads.splice(index,1);renderAttachmentStrip();};node.append(copy,remove);root.append(node);});
  }
  async function cancelAgent(){if(!state.session)return;try{await api(`/api/agent-sessions/${state.session.sessionId}/execution`,{method:"DELETE"});toast("已发送取消请求");if(!state.agentStream)startAgentStream();}catch(error){toast(error.message,true);}}

  init().catch(error => { toast("页面初始化失败：" + error.message, true); setConnection({ok:false,error:error.message}); });
})();
