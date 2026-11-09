import { fetchJson } from "./shared/api.js";
import { icon } from "./shared/icons.js";
import { pollJob } from "./shared/job-poller.js";
import { replaceUrlState } from "./shared/url-state.js";
import { createDatasetController } from "./studio/dataset-controller.js";
import { createHistoryController } from "./studio/history-controller.js";
import { createPaneResizer } from "./studio/pane-resizer.js";
import { createProviderController } from "./studio/provider-controller.js";
import { createRubricView } from "./studio/rubric-view.js";
import { createVideoEvidence } from "./studio/video-evidence.js";

/* 多模态视频质检 Studio —— 本服务唯一的页面。
 *
 * 输入是浏览器**上传**（或从离线数据集里挑一条现成素材），所以走
 * POST /api/upload_analyze（multipart）→ 轮询 /api/job → 页内证据摘要，
 * 完整 HTML 报告统一在新窗口打开。
 *
 * 参考素材的 label（Image 3 / Audio 1 / Video 1）是模型把 prompt 里的
 * 「参考 Image 3」对回具体素材的唯一依据，因此每张卡片的标签都可人工改。
 */

const MAX_VIDEO_MB = 300;
const MAX_REF_MB = 200;
const KIND_LABEL = { image: "Image", audio: "Audio", video: "Video" };
const KIND_TEXT = { image: "参考图", audio: "参考音频", video: "参考视频" };
const AUDIO_EXT = ["mp3", "wav", "m4a", "aac", "flac", "ogg"];
const VIDEO_EXT = ["mp4", "mov", "webm", "mkv"];

const state = {
  video: null,
  refs: [],
  jobId: null,
  jobController: null,
  report: null,
  seq: 0,
  providers: {},
  // 离线数据集浏览器
  datasetCaseId: null,   // 当前已载入素材所属的 case（提交时后端据此解析文件名）
  datasetCases: null,    // /api/dataset_cases 结果，只拉一次
  datasetFilter: "",
  datasetDetail: null,   // 展开中的 case 详情（含 refs / results）
  datasetLoading: false,
  // 历史质检记录
  historySlug: null,     // 当前左栏素材来自哪条历史记录（提交时后端据此原地取素材）
};

const el = {};

function $(id) {
  return document.getElementById(id);
}

function bindElements() {
  [
    "prompt", "promptCount", "dropzone", "fileInput", "videoInput",
    "assetList", "videoSlot", "runButton", "runHint", "modeBadge",
    "stageList", "resultPane", "emptyState", "errorBox",
    "verdictPane", "vdSchema", "vdTally", "vdReport", "vdGrid", "vdDrawer", "vdFlags",
    "themeToggle", "overlay", "refCount", "clearAll",
    "browseDataset", "datasetPanel", "datasetClose", "datasetSearch",
    "datasetList", "datasetHint",
    "providerSelect",
    "lightbox", "lightboxTitle", "lightboxMeta", "lightboxBody", "lightboxClose",
    "videoPane", "vpaneCount", "vpaneFilters",
    "vWrap", "vEl", "vLayer", "vTrack", "vFill",
    "vTime", "vJumps",
    "vOrphans", "vOrphansHead", "vOrphansList", "jCopy",
    "jTags", "jHint",
    "navToggle", "inputToggle",
    "useVlm",
    "historyList", "historyNew",
  ].forEach((key) => {
    el[key] = $(key);
  });
}

function extOf(name) {
  const parts = String(name || "").toLowerCase().split(".");
  return parts.length > 1 ? parts.pop() : "";
}

function kindOf(file) {
  const ext = extOf(file.name);
  if (AUDIO_EXT.includes(ext) || (file.type || "").startsWith("audio/")) return "audio";
  if (VIDEO_EXT.includes(ext) || (file.type || "").startsWith("video/")) return "video";
  return "image";
}

function humanSize(bytes) {
  // 历史素材若已被清理就没有大小可报，此时留空而不是印出 "NaN KB"
  if (!Number.isFinite(bytes)) return "";
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + " MB";
  return Math.max(1, Math.round(bytes / 1024)) + " KB";
}

/* 素材来源的行内标注。本地上传不标（是默认情形），其余都标出来，
 * 否则「这些字节到底在谁的磁盘上」只能靠猜。 */
function originNote(item) {
  if (!item) return "";
  if (item.available === false) return " · 素材已清理";
  if (item.origin === "dataset") return " · 离线数据集";
  if (item.origin === "history") return " · 历史素材";
  return "";
}

function escapeHtml(text) {
  return String(text == null ? "" : text).replace(/[&<>"']/g, (ch) => (
    { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]
  ));
}

const videoEvidence = createVideoEvidence({
  elements: el,
  escapeHtml,
});

const rubricView = createRubricView({
  elements: el,
  escapeHtml,
  reportUrl,
  onIssueFocus: (issueId) => videoEvidence.focusIssue(issueId),
});

/* 标签按 kind 重新编号：删掉 Image 2 后，原 Image 3 应变成 Image 2，
 * 否则 prompt 里的引用会指向不存在的标签。仅重排未被人工锁定的卡片。 */
function renumberLabels() {
  const counters = { image: 0, audio: 0, video: 0 };
  state.refs.forEach((ref) => {
    counters[ref.kind] += 1;
    if (!ref.labelLocked) ref.label = `${KIND_LABEL[ref.kind]} ${counters[ref.kind]}`;
  });
}

/* 从文件名反解标签。数据集的命名带序号（`1-2-image-4.png` / `s2-7-img-1.png`），
 * 本地整理过的文件常写成 `ref_04_Image4.png`，两种都要认。
 * 直接沿用文件名比按上传顺序重编号准确得多 —— 编号错了模型就会把「参考 Image 4」对到别的素材。 */
const LABEL_RE = /(?:^|[-_\s])(image|img|audio|video)[-_\s]*(\d+)/i;

function sniffLabel(name, kind) {
  const match = String(name || "").match(LABEL_RE);
  if (!match) return "";
  const raw = match[1].toLowerCase();
  const sniffed = raw === "img" ? "image" : raw;
  if (sniffed !== kind) return "";
  return `${KIND_LABEL[kind]} ${parseInt(match[2], 10)}`;
}

function addFiles(fileList, target) {
  const files = Array.from(fileList || []);
  if (!files.length) return;
  if (target === "video") {
    const file = files[0];
    if (file.size > MAX_VIDEO_MB * (1 << 20)) {
      showError(`待质检视频超过 ${MAX_VIDEO_MB}MB`);
      return;
    }
    revokeUrl(state.video);
    revokeFullUrl(state.video);
    state.video = { file, name: file.name, size: file.size, url: URL.createObjectURL(file) };
    render();
    return;
  }
  files.forEach((file) => {
    if (file.size > MAX_REF_MB * (1 << 20)) {
      showError(`${file.name} 超过 ${MAX_REF_MB}MB，已跳过`);
      return;
    }
    const kind = kindOf(file);
    const sniffed = sniffLabel(file.name, kind);
    state.refs.push({
      id: `r${++state.seq}`,
      file,
      name: file.name,
      size: file.size,
      kind,
      label: sniffed,
      labelLocked: Boolean(sniffed),
      url: kind === "image" ? URL.createObjectURL(file) : null,
    });
  });
  sortRefs();
  renumberLabels();
  render();
}

/* Image → Audio → Video，与后端 normalize_references 的排序一致。 */
function sortRefs() {
  const order = { image: 0, audio: 1, video: 2 };
  state.refs.sort((a, b) => order[a.kind] - order[b.kind]);
}

/* 只有 blob: 是本地上传创建的，需要手动释放；离线素材的 url 指向 /api/dataset_media。 */
function revokeUrl(item) {
  if (item && item.url && item.url.startsWith("blob:")) URL.revokeObjectURL(item.url);
}

function removeRef(id) {
  const index = state.refs.findIndex((r) => r.id === id);
  if (index < 0) return;
  const [ref] = state.refs.splice(index, 1);
  revokeUrl(ref);
  revokeFullUrl(ref);
  renumberLabels();
  render();
}

/* 放大用的原始地址：离线素材后端已给 fullUrl（/api/dataset_media，支持 Range +
 * immutable 缓存）；本地文件只有点开放大时才建 blob，避免一上传就为十几个
 * 素材各占一份内存。 */
function fullUrlOf(item) {
  if (!item) return "";
  if (item.fullUrl) return item.fullUrl;
  if (item.file) {
    item.fullUrl = URL.createObjectURL(item.file);
    item.fullOwned = true;
    return item.fullUrl;
  }
  return item.url || "";
}

function revokeFullUrl(item) {
  if (item && item.fullOwned && item.fullUrl) {
    URL.revokeObjectURL(item.fullUrl);
    item.fullUrl = null;
    item.fullOwned = false;
  }
}

/**
 * Return the result column to one canonical idle state.
 *
 * Case changes, "clear all" and new runs all pass through this function so
 * stale reports, clips and stage rows cannot survive a new input context.
 */
function resetResultView() {
  if (state.jobController) state.jobController.abort();
  state.jobController = null;
  state.jobId = null;
  state.report = null;

  el.resultPane.dataset.phase = "idle";
  el.stageList.innerHTML = "";
  rubricView.reset();
  el.emptyState.hidden = false;
  el.emptyState.textContent = "上传待质检视频后点击「开始质检」，报告将在此处呈现。填写 Prompt 与参考素材可额外评估指令遵循。";
  videoEvidence.reset();
}

/* resetInputs 只清输入与结果；clearAll 额外把侧栏历史的选中态一起清掉。
 * 分成两个是因为「回看一条历史记录」也要先清空输入，但绝不能顺手把刚点亮
 * 的那一行取消选中。 */
function resetInputs() {
  state.refs.forEach(revokeUrl);
  state.refs.forEach(revokeFullUrl);
  state.refs = [];
  revokeUrl(state.video);
  revokeFullUrl(state.video);
  state.video = null;
  state.datasetCaseId = null;
  state.historySlug = null;
  replaceUrlState({ case: null });
  el.prompt.value = "";
  resetResultView();
  showError("");
  render();
}

function clearAll() {
  resetInputs();
  historyController.clearActive();
}

/* ── 历史记录回看 ────────────────────────────────────────────────────
 * 素材留在服务器上（runtime/uploads/<slug>/），所以「恢复」只是把界面指回
 * 那批文件的 URL，不重新下载字节。origin="history" 的条目在提交时不上传内容，
 * 只回传 slug 与标签，因此可以改完 Prompt 直接重跑。
 */

function restoreFromHistory(payload) {
  const restore = (payload && payload.restore) || {};
  resetInputs();
  state.historySlug = restore.slug || null;
  el.prompt.value = restore.prompt || "";

  const video = restore.video || {};
  if (video.url) {
    state.video = {
      name: video.name || "历史视频",
      size: video.size,
      url: video.url,
      origin: "history",
      available: video.available !== false,
    };
  }
  (restore.references || []).forEach((ref) => {
    state.refs.push({
      id: `r${++state.seq}`,
      name: ref.name || ref.label || "参考素材",
      size: ref.size,
      kind: ref.kind || "image",
      label: ref.label || "",
      // 历史标签也参与内容寻址，锁住它才能保证「什么都不改就重跑」命中原缓存
      labelLocked: true,
      origin: "history",
      sourceLabel: ref.label || "",
      url: ref.kind === "image" ? ref.url : null,
      fullUrl: ref.url,
      available: ref.available !== false,
    });
  });
  sortRefs();

  if (restore.rerunnable === false) {
    showError("这次质检的原始素材已被清理，只能回看报告，无法原地重跑。");
  }
  finish(payload.report || null);
}

function renderVideoSlot() {
  if (!state.video) {
    el.videoSlot.className = "video-slot empty";
    el.videoSlot.innerHTML = `
      <div class="slot-icon" aria-hidden="true">${icon("film", { size: 20 })}</div>
      <div class="slot-copy">
        <p class="slot-title">待质检视频</p>
        <p class="slot-hint">点击选择，或拖到此处 · mp4 / mov ≤ ${MAX_VIDEO_MB}MB</p>
      </div>`;
    return;
  }
  el.videoSlot.className = "video-slot filled";
  // 离线视频用海报占位，不预加载（一条 result 就有 15MB）；本地上传的 blob 无此顾虑。
  const preview = state.video.poster
    ? `<img class="slot-video" src="${escapeHtml(state.video.poster)}" alt="" />`
    : `<video class="slot-video" src="${escapeHtml(state.video.url)}" muted playsinline preload="metadata"></video>`;
  el.videoSlot.innerHTML = `
    ${preview}
    <div class="slot-copy">
      <p class="slot-title">${escapeHtml(state.video.name)}</p>
      <p class="slot-hint">${humanSize(state.video.size)}${originNote(state.video)} · 点击可替换</p>
    </div>
    <button class="lb-zoom" type="button" data-zoom-video aria-label="放大预览视频">${icon("expand", { size: 13, stroke: 1.7 })}</button>
    <button class="slot-remove" type="button" data-remove-video aria-label="移除视频">${icon("xmark", { size: 12, stroke: 1.8 })}</button>`;
}

function renderAssets() {
  el.refCount.textContent = state.refs.length ? `${state.refs.length} 个` : "";
  if (!state.refs.length) {
    el.assetList.innerHTML = "";
    el.assetList.hidden = true;
    return;
  }
  el.assetList.hidden = false;
  el.assetList.innerHTML = state.refs.map((ref) => {
    const thumb = ref.kind === "image"
      ? `<img src="${escapeHtml(ref.url)}" alt="" />`
      : `<span class="thumb-glyph" aria-hidden="true">${icon(ref.kind === "audio" ? "waveform" : "film", { size: 20 })}</span>`;
    // 标签下拉给出该 kind 的 1..N + 当前值，避免用户手输错格式。
    const sameKind = state.refs.filter((r) => r.kind === ref.kind);
    const ordinals = sameKind
      .map((r) => parseInt(String(r.label).replace(/\D+/g, ""), 10))
      .filter((n) => Number.isFinite(n));
    const total = Math.max(9, sameKind.length, ...ordinals);
    const options = [];
    for (let i = 1; i <= total; i += 1) {
      const value = `${KIND_LABEL[ref.kind]} ${i}`;
      options.push(`<option value="${escapeHtml(value)}"${value === ref.label ? " selected" : ""}>${escapeHtml(value)}</option>`);
    }
    return `
      <li class="asset-card" data-id="${ref.id}">
        <div class="asset-thumb zoomable kind-${ref.kind}" data-zoom="${ref.id}"
             role="button" tabindex="0" title="点击放大">${thumb}</div>
        <div class="asset-body">
          <p class="asset-name" title="${escapeHtml(ref.name)}">${escapeHtml(ref.name)}</p>
          <p class="asset-meta">${KIND_TEXT[ref.kind]} · ${humanSize(ref.size)}${originNote(ref)}</p>
          <label class="asset-label">
            <span class="sr-only">素材标签</span>
            <select data-label-for="${ref.id}">${options.join("")}</select>
          </label>
        </div>
        <button class="asset-remove" type="button" data-remove="${ref.id}" aria-label="移除 ${escapeHtml(ref.label)}">${icon("xmark", { size: 12, stroke: 1.8 })}</button>
      </li>`;
  }).join("");
}

/* preload="metadata" 是硬要求：默认的 auto 会在弹层打开瞬间缓冲整片（一条
 * result 15MB），经反向代理时直接卡住；metadata 只取文件头，起播靠 Range。 */
function openLightbox(item, kind) {
  if (!item) return;
  const src = fullUrlOf(item);
  if (!src) return;
  const url = escapeHtml(src);
  let body;
  if (kind === "image") {
    body = `<img class="lb-image" src="${url}" alt="${escapeHtml(item.name || "")}" />`;
  } else if (kind === "audio") {
    body = `<audio class="lb-audio" src="${url}" controls autoplay></audio>`;
  } else {
    body = `<video class="lb-video" src="${url}" controls autoplay
                   preload="metadata" playsinline></video>`;
  }
  el.lightboxTitle.textContent = item.name || "";
  const bits = [];
  if (item.label) bits.push(item.label);
  if (item.size) bits.push(humanSize(item.size));
  if (item.origin === "dataset") bits.push("离线数据集");
  el.lightboxMeta.textContent = bits.join(" · ");
  el.lightboxBody.innerHTML = body;
  el.lightbox.hidden = false;
}

/* 清空 innerHTML 即停掉播放；blob 不在这里 revoke —— 卡片缩略图还在用同一个
 * 对象，释放了会让已渲染的 <img> 变白。 */
function closeLightbox() {
  el.lightboxBody.innerHTML = "";
  el.lightbox.hidden = true;
}

function renderMode() {
  const hasImage = state.refs.some((r) => r.kind === "image");
  el.modeBadge.textContent = hasImage ? "I2VA" : "T2VA";
  el.modeBadge.className = `mode-badge ${hasImage ? "i2v" : "t2v"}`;
  el.modeBadge.title = hasImage ? "含参考图 → 图生视频评测" : "无参考图 → 文生视频评测";
}

function renderRunButton() {
  const busy = Boolean(state.jobId);
  // Prompt 非必填：没有它就退化成「无指令质检」，只跑画质/动态/音画等
  // 不依赖指令的维度，指令遵循维度自动跳过。视频才是唯一的硬前提。
  // 回看一条素材已被清理的历史记录时，视频槽有内容但磁盘上没文件，不能让它可点
  const ready = Boolean(state.video) && state.video.available !== false;
  const selectedProvider = state.providers[el.providerSelect && el.providerSelect.value];
  const providerReady = !el.useVlm.checked || Boolean(selectedProvider && selectedProvider.configured);
  const providerLoaded = Object.keys(state.providers).length > 0;
  el.runButton.disabled = busy || !ready || !providerReady;
  el.clearAll.disabled = busy;
  if (el.providerSelect) el.providerSelect.disabled = busy || !providerLoaded;
  el.runButton.textContent = busy ? "质检中…" : "开始质检";
  if (busy) el.runHint.textContent = "正在调用模型，请勿关闭页面";
  else if (!state.video) el.runHint.textContent = "请上传待质检视频";
  else if (state.video.available === false) el.runHint.textContent = "原始素材已被清理，仅可回看报告";
  else if (!providerReady) el.runHint.textContent = "当前模型通道未配置";
  else if (!el.prompt.value.trim()) el.runHint.textContent = "未填 Prompt，将跳过指令遵循维度";
  else el.runHint.textContent = `${state.refs.length} 个参考素材已就绪`;
}

function render() {
  el.promptCount.textContent = `${el.prompt.value.length}`;
  renderVideoSlot();
  renderAssets();
  renderMode();
  renderRunButton();
}

function showError(message) {
  el.errorBox.textContent = message;
  el.errorBox.hidden = !message;
}

const STAGE_STATE = { running: "运行中", done: "完成", error: "失败", skipped: "跳过" };

function renderStages(events) {
  events.forEach((event) => {
    if (!event || event.type !== "stage") return;
    const key = String(event.id || event.title || "");
    if (!key) return;
    let row = el.stageList.querySelector(`[data-stage="${CSS.escape(key)}"]`);
    if (!row) {
      row = document.createElement("li");
      row.className = "stage-row";
      row.dataset.stage = key;
      el.stageList.appendChild(row);
    }
    const status = String(event.status || "running");
    row.dataset.status = status;
    row.dataset.kind = String(event.kind || "tool");
    const note = event.output || event.detail || event.input || "";
    row.innerHTML = `
      <span class="stage-dot" aria-hidden="true"></span>
      <span class="stage-name">${escapeHtml(event.title || key)}</span>
      <span class="stage-note" title="${escapeHtml(note)}">${escapeHtml(note)}</span>
      <span class="stage-state">${escapeHtml(STAGE_STATE[status] || status)}</span>`;
  });
}

async function submit() {
  if (state.jobId) return;
  showError("");
  if (!state.video) {
    showError("请先上传待质检视频");
    return;
  }
  const prompt = el.prompt.value.trim();

  const form = new FormData();
  form.append("prompt", prompt);
  form.append("use_vlm", $("useVlm").checked ? "1" : "0");
  form.append("allow_fallback", $("useVlm").checked ? "0" : "1");
  form.append("refresh", $("refreshCache").checked ? "1" : "0");
  form.append("provider", el.providerSelect.value);

  // 离线素材已在服务器磁盘上，只传文件名让后端原地引用 —— 否则要白跑一趟
  // 下行（下载到浏览器）再加一趟上行（传回去）。
  const usesDataset = state.video.origin === "dataset"
    || state.refs.some((r) => r.origin === "dataset");
  if (usesDataset) form.append("dataset_case_id", state.datasetCaseId || "");

  // 历史素材同理：runtime/uploads/<slug>/ 下的文件原地引用，只回传 slug。
  const usesHistory = state.video.origin === "history"
    || state.refs.some((r) => r.origin === "history");
  if (usesHistory) form.append("history_slug", state.historySlug || "");

  if (state.video.origin === "dataset") {
    form.append("dataset_video", state.video.name);
  } else if (state.video.origin === "history") {
    // 视频由 history_slug 在后端解析，无需字段
  } else {
    form.append("video", state.video.file, state.video.name);
  }
  // 三种来源各自独立编号：后端按 `label_<i>` / `dataset_label_<i>` /
  // `history_label_<i>` 分别取，互不干扰。
  let uploadIndex = 0;
  state.refs.forEach((ref, index) => {
    if (ref.origin === "dataset") {
      form.append(`dataset_ref_${index}`, ref.name);
      form.append(`dataset_label_${index}`, ref.label);
      return;
    }
    if (ref.origin === "history") {
      // 选条用 manifest 里的原标签，重跑用的标签发界面上当前那个 ——
      // 删掉一条后剩下的会重新编号，两者可能已经不同。
      form.append(`history_ref_${index}`, ref.sourceLabel || ref.label);
      form.append(`history_label_${index}`, ref.label);
      return;
    }
    form.append("ref", ref.file, ref.name);
    // 标签按 ref 的提交序号对应（同名文件可能出现两次，不能用文件名做 key）。
    form.append(`label_${uploadIndex}`, ref.label);
    uploadIndex += 1;
  });

  resetResultView();
  el.emptyState.hidden = true;
  el.resultPane.dataset.phase = "running";
  state.jobId = "pending";
  render();

  try {
    const data = await fetchJson("./api/upload_analyze", { method: "POST", body: form });
    state.jobId = data.job_id;
    watchJob(data.job_id);
  } catch (error) {
    state.jobId = null;
    el.resultPane.dataset.phase = "idle";
    showError(`提交失败：${error.message}`);
    render();
  }
  render();
}

async function watchJob(jobId) {
  const controller = new AbortController();
  state.jobController = controller;
  try {
    const data = await pollJob({
      jobId,
      intervalMs: 1500,
      signal: controller.signal,
      onEvents: renderStages,
    });
    if (data.status === "cancelled") return;
    if (data.status === "done") {
      finish(data.report);
      // 刚落盘的那条应当立刻出现在侧栏并处于选中态。上传流的
      // case_id 就是内容寻址 slug，据此把新记录对上号。
      historyController.refreshWithSlug(data.report?.case?.case_id);
      return;
    }
    if (data.status === "error") {
      state.jobId = null;
      el.resultPane.dataset.phase = "idle";
      showError(`质检失败：${data.error || "未知错误"}`);
      render();
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      state.jobId = null;
      el.resultPane.dataset.phase = "idle";
      // 404 是服务重启或作业被淘汰导致的，报「任务不存在」对用户没有意义，
      // 直接给出下一步动作。
      showError(error.status === 404
        ? "质检中断：任务已过期（服务可能重启过），请重新提交。"
        : `质检中断：${error.message}`);
      render();
    }
  } finally {
    if (state.jobController === controller) state.jobController = null;
  }
}

function finish(report) {
  state.jobId = null;
  state.report = report;
  el.resultPane.dataset.phase = "done";
  // 有报告就没有「待上传」这回事。放在这里而不是各调用方，是因为回看历史
  // 记录时不经过 submit()，之前只有那条路径手动隐过占位文案。
  el.emptyState.hidden = Boolean(report);
  render();

  rubricView.mount(report);

  // 报告不再内嵌：右栏纵向空间全部留给视频组件，报告走新窗口
  // （嵌在下面时被压成一条缝，反而失去意义）。
  if (!report) {
    el.emptyState.hidden = false;
    el.emptyState.textContent = "这条记录没有可用的报告数据。";
  } else if (!reportUrl(report)) {
    el.emptyState.hidden = false;
    el.emptyState.textContent = "报告未生成，请查看阶段日志。";
  }

  videoEvidence.mountReport(report);
}

function reportUrl(report) {
  const runId = report && report.llm_trace && report.llm_trace.run_id;
  if (report && report.report_view_url) return report.report_view_url;
  return runId ? `./api/report?run_id=${encodeURIComponent(runId)}` : null;
}

const datasetController = createDatasetController({
  state,
  elements: el,
  escapeHtml,
  humanSize,
  clearAll,
  sortRefs,
  showError,
  renderForm: render,
  closeLightbox,
});

const providerController = createProviderController({
  state,
  elements: el,
  escapeHtml,
  onChange: render,
});

const historyController = createHistoryController({
  elements: el,
  escapeHtml,
  showError,
  onOpen: restoreFromHistory,
  // 控制器自己已经清掉选中态，这里只需要清输入，避免多渲染一遍
  onNew: resetInputs,
});

/* 拖拽：整页接管。dragleave 会在子元素间穿梭时误触发，所以用进入计数配平。 */
function wireDragDrop() {
  let depth = 0;
  const show = () => { el.overlay.hidden = false; };
  const hide = () => { depth = 0; el.overlay.hidden = true; };

  window.addEventListener("dragenter", (event) => {
    if (!event.dataTransfer || !Array.from(event.dataTransfer.types).includes("Files")) return;
    depth += 1;
    show();
  });
  window.addEventListener("dragover", (event) => event.preventDefault());
  window.addEventListener("dragleave", () => {
    depth = Math.max(0, depth - 1);
    if (!depth) hide();
  });
  window.addEventListener("drop", (event) => {
    event.preventDefault();
    hide();
    const files = Array.from((event.dataTransfer && event.dataTransfer.files) || []);
    if (!files.length) return;
    // 拖到视频槽上就当视频，其余一律进参考区。
    const onVideoSlot = event.target && event.target.closest && event.target.closest("#videoSlot");
    addFiles(files, onVideoSlot ? "video" : "ref");
  });
}

function wireTheme() {
  const stored = localStorage.getItem("studio-theme");
  const initial = stored || (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  document.documentElement.className = initial;
  el.themeToggle.addEventListener("click", () => {
    const next = document.documentElement.className === "dark" ? "light" : "dark";
    document.documentElement.className = next;
    localStorage.setItem("studio-theme", next);
  });
}

/* ── 左栏 / 中栏收起 ──────────────────────────────────────────────
 * 收起后视频区变宽，annotation-layer 必须重量 —— 它的百分比是相对视频
 * 自身的内容矩形，宽度一变框就全偏了。收起状态与主题一样落 localStorage。
 */
function setPane(which, collapsed) {
  const shell = document.querySelector(".shell");
  if (!shell) return;
  shell.classList.toggle(which === "nav" ? "nav-collapsed" : "input-collapsed", collapsed);
  localStorage.setItem(`studio-${which}-collapsed`, collapsed ? "1" : "0");
  const toggle = which === "nav" ? el.navToggle : el.inputToggle;
  if (toggle) {
    toggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    // 同一个键既收也展，所以提示文案必须跟着当前状态翻转
    const label = `${collapsed ? "展开" : "收起"}${which === "nav" ? "边栏" : "素材区"}`;
    toggle.title = label;
    toggle.setAttribute("aria-label", label);
  }
  // 过渡期间量到的是中间宽度，等一帧再量
  window.requestAnimationFrame(videoEvidence.refit);
}

/* 标题栏上那两个键是真正的开关：按一次收起，再按一次展开。
 * 原来它们只负责"收起"，展开得另找一颗恢复键 —— 于是收起后同一件事有两个
 * 入口。macOS 的边栏键从来只有一颗，状态靠它自己的高亮表达。 */
function wirePanes() {
  const shell = document.querySelector(".shell");
  ["nav", "input"].forEach((which) => {
    const toggle = which === "nav" ? el.navToggle : el.inputToggle;
    const cls = which === "nav" ? "nav-collapsed" : "input-collapsed";
    if (toggle) {
      toggle.addEventListener("click", () => {
        setPane(which, !(shell && shell.classList.contains(cls)));
      });
    }
    setPane(which, localStorage.getItem(`studio-${which}-collapsed`) === "1");
  });
}

function wire() {
  el.prompt.addEventListener("input", render);
  el.prompt.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") submit();
  });
  el.runButton.addEventListener("click", submit);
  el.clearAll.addEventListener("click", clearAll);
  el.useVlm.addEventListener("change", render);

  el.dropzone.addEventListener("click", () => el.fileInput.click());
  el.fileInput.addEventListener("change", (event) => {
    addFiles(event.target.files, "ref");
    event.target.value = "";
  });
  el.videoSlot.addEventListener("click", (event) => {
    if (event.target.closest("[data-remove-video]")) {
      revokeUrl(state.video);
      revokeFullUrl(state.video);
      state.video = null;
      render();
      return;
    }
    if (event.target.closest("[data-zoom-video]")) {
      openLightbox(state.video, "video");
      return;
    }
    el.videoInput.click();
  });
  el.videoInput.addEventListener("change", (event) => {
    addFiles(event.target.files, "video");
    event.target.value = "";
  });

  el.assetList.addEventListener("click", (event) => {
    const button = event.target.closest("[data-remove]");
    if (button) {
      removeRef(button.dataset.remove);
      return;
    }
    const zoom = event.target.closest("[data-zoom]");
    if (zoom) {
      const ref = state.refs.find((r) => r.id === zoom.dataset.zoom);
      if (ref) openLightbox(ref, ref.kind);
    }
  });
  el.assetList.addEventListener("change", (event) => {
    const select = event.target.closest("[data-label-for]");
    if (!select) return;
    const ref = state.refs.find((r) => r.id === select.dataset.labelFor);
    if (!ref) return;
    ref.label = select.value;
    ref.labelLocked = true;  // 人工指定过的标签不再被自动重排覆盖
    render();
  });

  el.lightboxClose.addEventListener("click", closeLightbox);
  el.lightbox.addEventListener("click", (event) => {
    if (event.target === el.lightbox) closeLightbox();
  });
  el.assetList.addEventListener("keydown", (event) => {
    const zoom = event.target.closest("[data-zoom]");
    if (!zoom || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    const ref = state.refs.find((r) => r.id === zoom.dataset.zoom);
    if (ref) openLightbox(ref, ref.kind);
  });

  wireDragDrop();
  datasetController.wire();
  providerController.wire();
  historyController.wire();
  wireTheme();
  videoEvidence.wire();
  rubricView.wire();
  wirePanes();
  // 栏宽一变就得重新量视频：grounding 框是相对播放器内容矩形的百分比。
  createPaneResizer({ onResize: videoEvidence.refit }).wire();
}

bindElements();
wire();
render();
providerController.load();
datasetController.restore();
historyController.load();
