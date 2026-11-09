import { fetchJson } from "../shared/api.js";
import { icon } from "../shared/icons.js";
import { readUrlState, replaceUrlState } from "../shared/url-state.js";

/**
 * Offline dataset browser.
 *
 * The controller owns dataset fetching, filtering, selection and the browser
 * panel. It receives only the form actions needed to hydrate the Studio input.
 */
export function createDatasetController({
  state,
  elements,
  escapeHtml,
  humanSize,
  clearAll,
  sortRefs,
  showError,
  renderForm,
  closeLightbox,
}) {
  function open() {
    elements.datasetPanel.hidden = false;
    if (!state.datasetCases && !state.datasetLoading) loadCases();
    else render();
    elements.datasetSearch.focus();
  }

  function close() {
    elements.datasetPanel.hidden = true;
    state.datasetDetail = null;
  }

  async function loadCases() {
    state.datasetLoading = true;
    render();
    try {
      const data = await fetchJson("./api/dataset_cases");
      state.datasetCases = data.cases || [];
    } catch (error) {
      state.datasetCases = [];
      elements.datasetHint.textContent = `加载数据集失败：${error.message}`;
    } finally {
      state.datasetLoading = false;
      render();
    }
  }

  async function toggleCase(caseId) {
    if (state.datasetDetail && state.datasetDetail.case_id === caseId) {
      state.datasetDetail = null;
      render();
      return;
    }
    state.datasetDetail = { case_id: caseId, loading: true };
    render();
    try {
      const data = await fetchJson(`./api/dataset_case?case_id=${encodeURIComponent(caseId)}`);
      state.datasetDetail = data;
    } catch (error) {
      state.datasetDetail = null;
      elements.datasetHint.textContent = `读取 ${caseId} 失败：${error.message}`;
    }
    render();
  }

  /**
   * Hydrate the form with one case. Dataset labels are authoritative because
   * they preserve prompt references such as "Image 4".
   */
  function loadCase(detail, resultFile) {
    clearAll();
    state.datasetCaseId = detail.case_id;
    elements.prompt.value = detail.prompt || "";
    const result = (detail.results || []).find((item) => item.file === resultFile)
      || (detail.results || [])[0];
    if (result) {
      state.video = {
        origin: "dataset",
        name: result.file,
        size: result.size,
        url: result.url,
        poster: result.thumb || null,
        fullUrl: result.url,
      };
    }
    (detail.refs || []).forEach((ref) => {
      state.refs.push({
        id: `d${++state.seq}`,
        origin: "dataset",
        name: ref.file,
        size: ref.size,
        kind: ref.kind,
        label: ref.label,
        labelLocked: true,
        // Cards use thumbnails; model submission resolves the original by name.
        url: ref.kind === "image" ? (ref.thumb || ref.url) : null,
        fullUrl: ref.url,
      });
    });
    sortRefs();
    close();
    showError("");
    replaceUrlState({ case: detail.case_id });
    renderForm();
  }

  function render() {
    if (elements.datasetPanel.hidden) return;
    if (state.datasetLoading) {
      elements.datasetHint.textContent = "正在读取数据集…";
      elements.datasetList.innerHTML = "";
      return;
    }
    const all = state.datasetCases || [];
    const keyword = state.datasetFilter.trim().toLowerCase();
    const cases = keyword
      ? all.filter((item) => item.case_id.toLowerCase().includes(keyword)
          || (item.prompt_preview || "").toLowerCase().includes(keyword))
      : all;

    elements.datasetHint.textContent = all.length
      ? `共 ${all.length} 条${keyword ? ` · 匹配 ${cases.length} 条` : ""} · 素材在服务器本地，不经浏览器传输`
      : "数据集为空或路径不可读";

    elements.datasetList.innerHTML = cases.map((item) => {
      const expanded = state.datasetDetail && state.datasetDetail.case_id === item.case_id;
      const kinds = [
        item.ref_kinds.image ? `${item.ref_kinds.image} 图` : "",
        item.ref_kinds.audio ? `${item.ref_kinds.audio} 音频` : "",
        item.ref_kinds.video ? `${item.ref_kinds.video} 视频` : "",
      ].filter(Boolean).join(" · ") || "无参考素材";
      return `
        <li class="ds-case${expanded ? " open" : ""}">
          <button class="ds-case-head" type="button" data-case="${escapeHtml(item.case_id)}">
            <span class="ds-case-id">${escapeHtml(item.case_id)}</span>
            <span class="ds-badge ${item.mode === "I2VA" ? "i2v" : "t2v"}">${escapeHtml(item.mode)}</span>
            <span class="ds-case-meta">${escapeHtml(kinds)} · ${item.result_count} 条待质检</span>
            <span class="ds-case-prompt">${escapeHtml(item.prompt_preview)}${item.prompt_chars > item.prompt_preview.length ? "…" : ""}</span>
            <span class="ds-chevron" aria-hidden="true">${icon("chevronRight", { size: 13, stroke: 1.7 })}</span>
          </button>
          ${expanded ? renderDetail(state.datasetDetail) : ""}
        </li>`;
    }).join("") || `<li class="ds-empty">没有匹配的 case</li>`;
  }

  function renderDetail(detail) {
    if (detail.loading) return `<div class="ds-detail"><p class="ds-loading">读取中…</p></div>`;
    const refs = (detail.refs || []).map((ref) => `
      <li class="ds-ref">
        <div class="ds-ref-thumb kind-${ref.kind}">${
          ref.kind === "audio"
            ? `<span class="thumb-glyph" aria-hidden="true">${icon("waveform", { size: 18 })}</span>`
            : `<img src="${escapeHtml(ref.thumb || ref.url)}" alt="" loading="lazy" />`
        }</div>
        <span class="ds-ref-label">${escapeHtml(ref.label)}</span>
      </li>`).join("");

    // Results mount posters first. Real video bytes load only after Preview.
    const results = (detail.results || []).map((result) => `
      <li class="ds-result" data-file="${escapeHtml(result.file)}">
        <div class="ds-result-media">
          <img class="ds-result-poster" src="${escapeHtml(result.thumb || "")}" alt="" loading="lazy" />
          <button class="ds-play" type="button" data-play="${escapeHtml(result.file)}"
                  aria-label="预览 ${escapeHtml(result.file)}">${icon("playFill", { size: 15 })}</button>
        </div>
        <div class="ds-result-body">
          <p class="ds-result-name">${escapeHtml(result.file)}</p>
          <p class="ds-result-meta">${humanSize(result.size)}</p>
        </div>
        <button class="btn-primary ds-pick" type="button"
                data-pick="${escapeHtml(result.file)}">用这条质检</button>
      </li>`).join("");

    return `
      <div class="ds-detail">
        <p class="ds-section-title">Prompt 全文</p>
        <p class="ds-prompt">${escapeHtml(detail.prompt)}</p>
        ${refs ? `<p class="ds-section-title">参考素材（${detail.refs.length}）</p>
        <ul class="ds-ref-list">${refs}</ul>` : ""}
        <p class="ds-section-title">待质检视频（${(detail.results || []).length}）</p>
        <ul class="ds-result-list">${results}</ul>
      </div>`;
  }

  function wire() {
    elements.browseDataset.addEventListener("click", open);
    elements.datasetClose.addEventListener("click", close);
    elements.datasetPanel.addEventListener("click", (event) => {
      if (event.target === elements.datasetPanel) close();
    });
    window.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") return;
      if (!elements.lightbox.hidden) {
        closeLightbox();
        return;
      }
      if (!elements.datasetPanel.hidden) close();
    });
    elements.datasetSearch.addEventListener("input", (event) => {
      state.datasetFilter = event.target.value;
      render();
    });
    elements.datasetList.addEventListener("click", (event) => {
      const play = event.target.closest("[data-play]");
      if (play) {
        const media = play.closest(".ds-result-media");
        const detail = state.datasetDetail;
        const item = detail && (detail.results || []).find((result) => result.file === play.dataset.play);
        if (media && item) {
          media.innerHTML = `<video class="ds-result-video" src="${escapeHtml(item.url)}#t=0.5"
            controls autoplay muted playsinline preload="metadata"></video>`;
        }
        return;
      }
      const pick = event.target.closest("[data-pick]");
      if (pick) {
        if (state.datasetDetail && !state.datasetDetail.loading) {
          loadCase(state.datasetDetail, pick.dataset.pick);
        }
        return;
      }
      const head = event.target.closest("[data-case]");
      if (head) toggleCase(head.dataset.case);
    });
  }

  async function restore() {
    const caseId = readUrlState().case;
    if (!caseId) return;
    try {
      const detail = await fetchJson(`./api/dataset_case?case_id=${encodeURIComponent(caseId)}`);
      state.datasetDetail = detail;
      loadCase(detail);
    } catch (_) {
      replaceUrlState({ case: null });
    }
  }

  return { loadCase, render, restore, wire };
}
