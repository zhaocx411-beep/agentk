import {
  GLYPH,
  TIER,
  TIER_ORDER,
  tierOfDimension,
  unresolvedCount,
  verdictOf,
} from "./verdict-glyphs.js";
import { icon } from "../shared/icons.js";

/**
 * native8 判定区：八维三档 + 未采信清单 + V2V 对齐测量。
 *
 * 这个组件是报告的门面，它刻意**不提供任何汇总数字**。八个维度各自独立成立，
 * 把它们压成一个数会让一条编造出来的结论被平均掉 —— 那是上一版协议的失败方式。
 */
export function createRubricView({ elements, escapeHtml, reportUrl, onIssueFocus }) {
  let wired = false;
  const local = {
    rubric: [],
    issues: new Map(),
    openDimension: null,
  };

  const FACT_LABEL = {
    requirement_importance: "要求重要性",
    object_scope: "变化对象",
    normal_speed_visible: "正常速度可见",
    duration_seconds: "持续时长",
    text_importance: "文字重要性",
    motion_type: "动态问题类型",
    affects_adoption: "影响采用",
    same_shot: "同一镜头内",
    state_before: "前态",
    state_after: "后态",
    change_cause: "变化成因",
    source_relation: "与源视频的关系",
    event_origin: "情节来源",
    confidence: "置信度",
    defect_type: "缺陷类型",
    text_issue_type: "文字问题类型",
    requirement_id: "要求编号",
    constraint_id: "约束编号",
    cut_required_by_prompt: "Prompt 要求切镜",
    motion_continuity_broken: "运动连续性中断",
    prompt_change_excluded: "已排除 Prompt 要求的变化",
    occlusion_or_exit_excluded: "已排除遮挡与出画",
    viewpoint_explanation_excluded: "已排除视角变化",
    continuous_frames_confirmed: "连续帧确认",
    story_conflict_type: "逻辑冲突类型",
    story_event_before: "前序事件",
    story_event_after: "后续事件",
    independent_of_prompt_failure: "独立于指令失败",
    primary_event_changed: "主线事件受影响",
  };

  /* 事实里的枚举值。留原文等于把内部标识符甩给读者，而这些值恰恰是定档的依据。 */
  const FACT_VALUE = {
    core: "核心要求",
    detail: "细节要求",
    subject: "主体",
    non_subject: "非主体",
    actual_change: "真实变化",
    inherited_unchanged: "源视频继承、本次未改",
    generated_addition: "模型自行补充",
    high: "高",
    medium: "中",
    low: "低",
    required_or_major: "必需或显著文字",
    incidental_small: "次要小字",
    physics_violation: "物理违背",
    structural_break: "结构崩坏",
    temporal_disappearance: "无因消失",
    uncoordinated: "动作不协调",
    camera_jitter: "镜头抖动",
    discontinuous_cut: "突兀切镜",
    causal_break: "因果断裂",
    motivation_break: "动机断裂",
    semantic_contradiction: "语义矛盾",
    common_sense_violation: "常识违背",
  };

  const SOURCE_LABEL = {
    unified: "主判",
    specialist: "专项复核",
    programmatic: "程序测量",
  };

  const REJECT_REASON = {
    incomplete_tier_or_dimension_evidence: "缺少定档事实或证据强度不足，程序拒绝采信",
  };

  const ALIGN_VERDICT = {
    unchanged: "几乎未改动",
    local_edit: "局部编辑",
    global_edit: "全片重绘",
    minor_diff: "轻微差异",
  };

  function factValue(key, value) {
    if (typeof value === "boolean") return value ? "是" : "否";
    if (key === "duration_seconds" && typeof value === "number") return `${value}s`;
    if (Array.isArray(value)) return value.map((item) => factValue(key, item)).join("、");
    if (value && typeof value === "object") return JSON.stringify(value);
    return FACT_VALUE[String(value)] || String(value);
  }

  /** 子判据的中文名；rubric 是它唯一的出处，所以反查而不是另建一张表。 */
  function criterionName(dimensionId, key) {
    const dimension = local.rubric.find((item) => item.dimension_id === dimensionId);
    const criterion = ((dimension && dimension.subcriteria) || [])
      .find((item) => item.key === key);
    return (criterion && criterion.name) || key || "";
  }

  function issuePill(issueId) {
    return `<button class="vd-issue" type="button" data-issue="${escapeHtml(issueId)}"
      title="在下方视频里定位 ${escapeHtml(issueId)}">${escapeHtml(issueId)}</button>`;
  }

  /* 事实表是这条判断唯一的依据，所以铺开而不是折叠：读者要能一眼看出
   * 「这条 0.5 分是哪个字段换来的」。 */
  function issueCard(issue) {
    if (!issue) return "";
    const facts = Object.entries(issue.facts || {}).map(([key, value]) => `
      <div class="vd-fact">
        <dt>${escapeHtml(FACT_LABEL[key] || key)}</dt>
        <dd>${escapeHtml(factValue(key, value))}</dd>
      </div>`).join("");
    const evidence = (issue.evidence || []).map((row) => `
      <li class="vd-eviline">
        <span class="vd-evitime">${escapeHtml(row.time || "未定位")}</span>
        <span>${escapeHtml(row.detail || "")}</span>
      </li>`).join("");
    const frames = (issue.evidence_frames || [])
      .map((frame) => (frame && frame.time != null ? `${Number(frame.time).toFixed(1)}s` : ""))
      .filter(Boolean);
    return `
      <article class="vd-issue-card">
        <header class="vd-issue-head">
          ${issuePill(issue.issue_id)}
          <span class="vd-issue-source">${escapeHtml(SOURCE_LABEL[issue.source] || issue.source || "")}</span>
          ${frames.length
            ? `<span class="vd-issue-frames">证据帧 ${escapeHtml(frames.join(" / "))}</span>`
            : ""}
        </header>
        ${issue.decision_summary
          ? `<p class="vd-issue-summary">${escapeHtml(issue.decision_summary)}</p>`
          : ""}
        ${evidence ? `<ul class="vd-evilist">${evidence}</ul>` : ""}
        ${facts ? `<dl class="vd-facts">${facts}</dl>` : ""}
      </article>`;
  }

  function tallyRow(rubric) {
    const counts = new Map();
    rubric.forEach((dimension) => {
      const tier = tierOfDimension(dimension);
      counts.set(tier.key, (counts.get(tier.key) || 0) + 1);
    });
    return TIER_ORDER.filter((key) => counts.get(key)).map((key) => {
      const tier = TIER[key];
      return `<li class="vd-tallyitem" data-tier="${key}">
        ${tier.glyph}<span class="vd-tallynum">${counts.get(key)}</span>
        <span class="vd-tallylabel">${escapeHtml(tier.label)}</span>
      </li>`;
    }).join("");
  }

  function tileMarkup(dimension) {
    const tier = tierOfDimension(dimension);
    const unresolved = unresolvedCount(dimension);
    const total = (dimension.subcriteria || []).length;
    const violated = (dimension.subcriteria || [])
      .filter((item) => item.verdict === "violated").length;
    const open = local.openDimension === dimension.dimension_id;
    const detail = dimension.applicable === false
      ? `${total} 项判据未评`
      : [
        violated ? `${violated} 项违反` : "",
        unresolved ? `${unresolved} 项未核实` : "",
        !violated && !unresolved ? `${total} 项均通过` : "",
      ].filter(Boolean).join(" · ");
    return `
      <li class="vd-tile" data-tier="${tier.key}" data-unresolved="${unresolved ? "1" : "0"}">
        <button class="vd-tilebtn${open ? " on" : ""}" type="button"
                data-dim="${escapeHtml(dimension.dimension_id)}" aria-expanded="${open}">
          <span class="vd-rail" aria-hidden="true"></span>
          <span class="vd-tiletop">${tier.glyph}<span class="vd-tiertext">${escapeHtml(tier.label)}</span></span>
          <span class="vd-dimname">${escapeHtml(dimension.dimension_name || dimension.name || dimension.dimension_id)}</span>
          <span class="vd-dimmeta">${escapeHtml(detail)}</span>
        </button>
      </li>`;
  }

  function drawerMarkup(dimension) {
    if (!dimension) {
      return `<p class="vd-drawer-hint">点开任一维度查看它的子判据、判定依据与关联问题。</p>`;
    }
    const tier = tierOfDimension(dimension);
    const rows = (dimension.subcriteria || []).map((item) => {
      const verdict = verdictOf(item.verdict);
      const pills = (item.issue_ids || []).map(issuePill).join("");
      const basis = item.decision_basis && typeof item.decision_basis === "object"
        ? Object.entries(item.decision_basis)
          .map(([key, value]) => `${key}：${factValue(key, value)}`)
          .join(" · ")
        : "";
      return `
        <li class="vd-crit" data-verdict="${verdict.key}">
          <span class="vd-critmark">${verdict.glyph}</span>
          <span class="vd-critname">${escapeHtml(item.name || item.key)}</span>
          <span class="vd-critverdict">${escapeHtml(verdict.label)}</span>
          <span class="vd-critissues">${pills}</span>
          ${basis ? `<span class="vd-critbasis">${escapeHtml(basis)}</span>` : ""}
        </li>`;
    }).join("");

    const cited = [...new Set(
      (dimension.subcriteria || []).flatMap((item) => item.issue_ids || []),
    )];
    const cards = cited.map((id) => issueCard(local.issues.get(id))).join("");

    return `
      <div class="vd-drawer-head">
        <span class="vd-drawer-tier" data-tier="${tier.key}">${tier.glyph}${escapeHtml(tier.label)}</span>
        <h4 class="vd-drawer-title">${escapeHtml(dimension.dimension_name || dimension.dimension_id)}</h4>
        ${dimension.escalated
          ? `<span class="vd-escalated">三处以上瑕疵，已升级为恶劣错误</span>`
          : ""}
        <button class="vd-drawer-close" type="button" data-close-drawer aria-label="收起">${
          icon("xmark", { size: 12, stroke: 1.8 })
        }</button>
      </div>
      <ul class="vd-critlist">${rows}</ul>
      ${cards ? `<div class="vd-issue-cards">${cards}</div>` : ""}`;
  }

  /* 未采信 / 待复核。空的时候也要出一行，否则读者无从知道这一关跑过没有。 */
  function reviewMarkup(report) {
    const required = report.review_required || [];
    const candidates = report.review_candidates || [];
    const funnel = report.review_funnel || {};
    const dimensionName = (id) => {
      const row = local.rubric.find((item) => item.dimension_id === id);
      return (row && (row.dimension_name || row.dimension_id)) || id;
    };

    if (!required.length && !candidates.length) {
      return `<p class="vd-flag-empty">${GLYPH.pass}没有被程序驳回或挂起的判断。</p>`;
    }

    const rows = required.map((item) => `
      <li class="vd-reject">
        ${issuePill(item.issue_id)}
        <span class="vd-reject-where">${escapeHtml(dimensionName(item.dimension_id))} · ${escapeHtml(criterionName(item.dimension_id, item.subcriterion))}</span>
        <span class="vd-reject-why">${escapeHtml(REJECT_REASON[item.reason] || item.reason || "")}</span>
      </li>`).join("");

    const funnelBits = [
      funnel.admitted_issue_count != null ? `采信 ${funnel.admitted_issue_count} 条` : "",
      funnel.gate_rejection_count != null ? `驳回 ${funnel.gate_rejection_count} 条` : "",
      funnel.unresolved_count != null ? `仍悬置 ${funnel.unresolved_count} 条` : "",
    ].filter(Boolean).join(" · ");

    return `
      <details class="vd-flag" data-flag="review" open>
        <summary class="vd-flag-sum">
          ${GLYPH.review}
          <span class="vd-flag-title">程序未采信 ${required.length} 条判断 · 待人工复核 ${candidates.length} 条</span>
          <span class="vd-flag-note">这些内容没有计入上方任何维度</span>
        </summary>
        <div class="vd-flag-body">
          ${funnelBits ? `<p class="vd-funnel">${escapeHtml(funnelBits)}</p>` : ""}
          ${rows ? `<ul class="vd-rejectlist">${rows}</ul>` : ""}
          ${candidates.length
            ? `<div class="vd-issue-cards">${candidates.map(issueCard).join("")}</div>`
            : ""}
        </div>
      </details>`;
  }

  function alignMarkup(align) {
    if (!align || align.available === false) return "";
    const pct = (value) => `${Math.round(Number(value || 0) * 100)}%`;
    const metrics = [
      ["画面相似度均值", `${align.ssim_mean}（最低 ${align.ssim_min}）`],
      ["改动时刻占比", pct(align.changed_ratio)],
      ["时长差", `${align.duration_delta}s`],
      ["采样", `${align.sample_count} 点 @ ${align.sample_fps}fps`],
      ["改动集中区域", (align.grid_changed_cells || []).join("、") || "无"],
    ].map(([label, value]) => `
      <div class="vd-metric">
        <dt>${escapeHtml(label)}</dt>
        <dd>${escapeHtml(value)}</dd>
      </div>`).join("");

    const span = Math.max(
      Number(align.duration_output) || 0,
      Number(align.duration_source) || 0,
      0.001,
    );
    const bands = (align.edited_segments || []).map((segment) => {
      const left = (Number(segment.start) / span) * 100;
      const width = Math.max(0.8, (Number(segment.duration) || 0) / span * 100);
      return `<span class="vd-band" style="left:${left}%;width:${width}%"
        title="${escapeHtml(segment.span || "")} · 最低相似度 ${escapeHtml(String(segment.min_ssim))}"></span>`;
    }).join("");

    return `
      <details class="vd-flag" data-flag="align">
        <summary class="vd-flag-sum">
          ${GLYPH.align}
          <span class="vd-flag-title">V2V 源视频对齐 · ${escapeHtml(ALIGN_VERDICT[align.verdict] || align.verdict || "")}</span>
          <span class="vd-flag-note">ffmpeg SSIM 逐时刻实测，不经模型</span>
        </summary>
        <div class="vd-flag-body">
          <dl class="vd-metrics">${metrics}</dl>
          <div class="vd-bandtrack" aria-hidden="true">${bands}</div>
          <p class="vd-bandnote">深色段为与源视频不同的时刻，共 ${(align.edited_segments || []).length} 段</p>
        </div>
      </details>`;
  }

  function renderGrid() {
    elements.vdGrid.innerHTML = local.rubric.map(tileMarkup).join("");
    const dimension = local.rubric.find((item) => item.dimension_id === local.openDimension);
    elements.vdDrawer.innerHTML = drawerMarkup(dimension);
    elements.vdDrawer.dataset.open = dimension ? "1" : "0";
  }

  function mount(report) {
    if (!report || !(report.rubric || []).length) {
      reset();
      return;
    }
    local.rubric = report.rubric;
    local.issues = new Map(
      [...(report.issues || []), ...(report.review_candidates || [])]
        .filter((issue) => issue && issue.issue_id)
        .map((issue) => [issue.issue_id, issue]),
    );
    local.openDimension = null;

    elements.verdictPane.hidden = false;
    elements.vdSchema.textContent = [
      report.schema_version || "",
      report.case && report.case.mode ? report.case.mode : "",
      report.analysis_status === "complete" ? "分析完成" : report.analysis_status || "",
    ].filter(Boolean).join(" · ");
    elements.vdTally.innerHTML = tallyRow(local.rubric);

    const href = reportUrl(report);
    elements.vdReport.hidden = !href;
    if (href) elements.vdReport.href = href;

    renderGrid();
    elements.vdFlags.innerHTML = reviewMarkup(report) + alignMarkup(report.v2v_align);
  }

  function reset() {
    local.rubric = [];
    local.issues = new Map();
    local.openDimension = null;
    elements.verdictPane.hidden = true;
    elements.vdSchema.textContent = "";
    elements.vdTally.innerHTML = "";
    elements.vdGrid.innerHTML = "";
    elements.vdDrawer.innerHTML = "";
    elements.vdDrawer.dataset.open = "0";
    elements.vdFlags.innerHTML = "";
    elements.vdReport.hidden = true;
  }

  function wire() {
    if (wired) return;
    wired = true;
    elements.verdictPane.addEventListener("click", (event) => {
      const issue = event.target.closest("[data-issue]");
      if (issue) {
        onIssueFocus(issue.dataset.issue);
        return;
      }
      if (event.target.closest("[data-close-drawer]")) {
        local.openDimension = null;
        renderGrid();
        return;
      }
      const tile = event.target.closest("[data-dim]");
      if (!tile) return;
      const opening = local.openDimension !== tile.dataset.dim;
      local.openDimension = opening ? tile.dataset.dim : null;
      renderGrid();
      // 判定区自己是个滚动容器，抽屉常常展开在可视区之外，看起来像点了没反应。
      if (opening) elements.vdDrawer.scrollIntoView({ behavior: "smooth", block: "nearest" });
    });
  }

  return { mount, reset, wire };
}
