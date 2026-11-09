import { fetchJson } from "../shared/api.js";
import { icon } from "../shared/icons.js";

/* 侧边栏的历史质检记录。
 *
 * 数据源是后端对 runtime/results/ 的只读投影，所以这里不缓存业务状态 ——
 * 每次跑完、改名、删除之后重新拉一次列表，避免前后端各存一份而漂移。
 *
 * 交互取自对话式列表的习惯：整行可点（回看那次质检），悬停才露出改名与删除，
 * 删除走行内二次确认而不是浏览器原生弹窗（原生弹窗会打断视频播放且不可控）。
 */

const ICON_RENAME = icon("pencil", { size: 13, stroke: 1.4 });
const ICON_DELETE = icon("trash", { size: 13, stroke: 1.4 });

/** 状态 → 圆点配色的语义类名。未知状态不上色，避免把「没跑完」说成「通过」。 */
const STATUS_CLASS = { pass: "ok", needs_review: "warn", blocked: "bad" };

function startOfDay(offsetDays = 0) {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  d.setDate(d.getDate() - offsetDays);
  return d.getTime();
}

/** 按「今天 / 昨天 / 近 7 天 / 更早」分组，组内保持后端给的倒序。 */
function groupByDate(items) {
  const today = startOfDay(0);
  const yesterday = startOfDay(1);
  const weekAgo = startOfDay(7);
  const buckets = [
    { key: "today", label: "今天", items: [] },
    { key: "yesterday", label: "昨天", items: [] },
    { key: "week", label: "近 7 天", items: [] },
    { key: "older", label: "更早", items: [] },
  ];
  items.forEach((item) => {
    const ms = (item.ts || 0) * 1000;
    if (ms >= today) buckets[0].items.push(item);
    else if (ms >= yesterday) buckets[1].items.push(item);
    else if (ms >= weekAgo) buckets[2].items.push(item);
    else buckets[3].items.push(item);
  });
  return buckets.filter((bucket) => bucket.items.length);
}

function timeText(ts) {
  if (!ts) return "";
  const d = new Date(ts * 1000);
  const pad = (n) => String(n).padStart(2, "0");
  const clock = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return d.getTime() >= startOfDay(0) ? clock : `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${clock}`;
}

export function createHistoryController({ elements, escapeHtml, onOpen, onNew, showError }) {
  const local = {
    items: [],
    activeId: null,
    editingId: null,
    confirmingId: null,
    loading: false,
    loaded: false,
  };

  function metaLine(item) {
    const bits = [];
    if (item.mode_badge) bits.push(item.mode_badge);
    if (item.engine && item.engine !== "agentk") bits.push(item.engine);
    return bits.join(" · ");
  }

  function rowInner(item) {
    if (local.confirmingId === item.id) {
      return `
        <div class="hist-confirm">
          <span class="hist-confirm-text">删除这条记录？素材一并清理</span>
          <span class="hist-confirm-actions">
            <button type="button" class="hist-danger" data-act="delete-yes">删除</button>
            <button type="button" class="hist-cancel" data-act="delete-no">取消</button>
          </span>
        </div>`;
    }
    if (local.editingId === item.id) {
      return `
        <input class="hist-rename" type="text" data-act="rename-input"
               value="${escapeHtml(item.title || "")}" maxlength="80"
               aria-label="重命名这条记录" />`;
    }
    const dot = STATUS_CLASS[item.status] || "idle";
    return `
      <span class="hist-dot ${dot}" aria-hidden="true"></span>
      <span class="hist-body">
        <span class="hist-title" title="${escapeHtml(item.prompt_preview || item.title || "")}">${escapeHtml(item.title || "未命名")}</span>
        <span class="hist-meta">${escapeHtml(metaLine(item))}</span>
      </span>
      <span class="hist-time">${escapeHtml(timeText(item.ts))}</span>
      <span class="hist-actions">
        <button type="button" class="hist-icon" data-act="rename" title="重命名" aria-label="重命名">${ICON_RENAME}</button>
        <button type="button" class="hist-icon" data-act="delete" title="删除" aria-label="删除">${ICON_DELETE}</button>
      </span>`;
  }

  function render() {
    const list = elements.historyList;
    if (!list) return;
    if (local.loading && !local.loaded) {
      list.innerHTML = `<li class="hist-empty">读取中…</li>`;
      return;
    }
    if (!local.items.length) {
      list.innerHTML = `<li class="hist-empty">还没有质检记录。跑完一条就会出现在这里。</li>`;
      return;
    }
    list.innerHTML = groupByDate(local.items).map((bucket) => `
      <li class="hist-group" role="presentation">${escapeHtml(bucket.label)}</li>
      ${bucket.items.map((item) => `
        <li class="hist-row${item.id === local.activeId ? " active" : ""}${local.confirmingId === item.id ? " confirming" : ""}"
            data-id="${escapeHtml(item.id)}"
            ${local.editingId === item.id || local.confirmingId === item.id ? "" : 'tabindex="0" role="button"'}
            ${item.id === local.activeId ? 'aria-current="true"' : ""}>
          ${rowInner(item)}
        </li>`).join("")}
    `).join("");

    const editing = list.querySelector('[data-act="rename-input"]');
    if (editing) {
      editing.focus();
      editing.select();
    }
  }

  async function load({ silent = false } = {}) {
    if (!elements.historyList) return;
    local.loading = true;
    if (!silent) render();
    try {
      const data = await fetchJson("./api/history?limit=200");
      local.items = Array.isArray(data.items) ? data.items : [];
      local.loaded = true;
      // 选中的那条可能刚被删掉（或缓存版本变了），别留一个指向空气的高亮
      if (local.activeId && !local.items.some((i) => i.id === local.activeId)) {
        local.activeId = null;
      }
    } catch (error) {
      local.items = [];
      local.loaded = true;
      elements.historyList.innerHTML = `<li class="hist-empty">历史记录读取失败</li>`;
      local.loading = false;
      return;
    }
    local.loading = false;
    render();
  }

  async function open(id) {
    if (!id || local.editingId || local.confirmingId) return;
    try {
      const data = await fetchJson(`./api/history_item?id=${encodeURIComponent(id)}`);
      local.activeId = id;
      render();
      onOpen(data);
    } catch (error) {
      showError(`打开历史记录失败：${error.message || error}`);
      // 大概率是文件已被清理，重拉一次让列表回到真实状态
      load({ silent: true });
    }
  }

  async function commitRename(id, value) {
    local.editingId = null;
    const item = local.items.find((i) => i.id === id);
    const next = String(value || "").trim();
    if (item && next === (item.title || "")) {
      render();
      return;
    }
    try {
      await fetchJson("./api/history_title", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id, title: next }),
      });
    } catch (error) {
      showError(`重命名失败：${error.message || error}`);
    }
    await load({ silent: true });
  }

  async function remove(id) {
    local.confirmingId = null;
    try {
      const res = await fetchJson("./api/history_delete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id, with_assets: true }),
      });
      if (res && res.assets_kept_shared) {
        showError("记录已删除；素材被同一批的其他记录共用，因此保留在磁盘上。");
      }
      if (local.activeId === id) {
        local.activeId = null;
        onNew();
      }
    } catch (error) {
      showError(`删除失败：${error.message || error}`);
    }
    await load({ silent: true });
  }

  function wire() {
    const list = elements.historyList;
    if (!list) return;

    list.addEventListener("click", (event) => {
      const row = event.target.closest(".hist-row");
      if (!row) return;
      const id = row.dataset.id;
      const act = event.target.closest("[data-act]")?.dataset.act;
      if (act === "rename") {
        local.confirmingId = null;
        local.editingId = id;
        render();
        return;
      }
      if (act === "delete") {
        local.editingId = null;
        local.confirmingId = id;
        render();
        return;
      }
      if (act === "delete-yes") {
        remove(id);
        return;
      }
      if (act === "delete-no") {
        local.confirmingId = null;
        render();
        return;
      }
      if (act === "rename-input") return;
      open(id);
    });

    list.addEventListener("keydown", (event) => {
      const row = event.target.closest(".hist-row");
      if (!row) return;
      const id = row.dataset.id;
      if (event.target.dataset.act === "rename-input") {
        if (event.key === "Enter") {
          event.preventDefault();
          commitRename(id, event.target.value);
        } else if (event.key === "Escape") {
          event.preventDefault();
          local.editingId = null;
          render();
        }
        return;
      }
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        open(id);
      }
    });

    // 失焦即提交：改完名字点到别处是「保存」的直觉，不是「丢弃」。
    list.addEventListener("focusout", (event) => {
      if (event.target.dataset?.act !== "rename-input") return;
      const row = event.target.closest(".hist-row");
      if (row && local.editingId === row.dataset.id) commitRename(row.dataset.id, event.target.value);
    });

    if (elements.historyNew) elements.historyNew.addEventListener("click", () => {
      local.activeId = null;
      local.editingId = null;
      local.confirmingId = null;
      render();
      onNew();
    });
  }

  /** 跑完一条之后：重拉列表，并把新产生的那条设为选中。 */
  async function refreshWithSlug(slug) {
    await load({ silent: true });
    if (!slug) return;
    const hit = local.items.find((item) => item.slug === slug);
    if (hit) {
      local.activeId = hit.id;
      render();
    }
  }

  function clearActive() {
    local.activeId = null;
    render();
  }

  return { load, wire, refreshWithSlug, clearActive, open };
}
