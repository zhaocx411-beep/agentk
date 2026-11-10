/* Keyboard-first client for the privacy review workbook.
   Validation lives on the server so this file cannot drift from the rules the
   import gate enforces: a rejected save simply shows the reason it returned. */

const state = { candidate: null, status: "" };

const element = (id) => document.getElementById(id);

async function send(route, body) {
  const response = await fetch(route, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || "请求失败");
  return payload;
}

/* ---------- rendering ---------- */

/* Server-side highlighting returns runs rather than HTML, so evidence text is
   never interpolated into markup. */
function renderRuns(target, runs) {
  target.textContent = "";
  (runs || []).forEach((run) => {
    if (run.sensitive) {
      const mark = document.createElement("mark");
      mark.textContent = run.text;
      target.append(mark);
    } else {
      target.append(document.createTextNode(run.text));
    }
  });
  if (!(runs || []).length) target.textContent = "—";
}

const FIELD_NAMES = {
  question: "题干",
  reference_answer: "参考答案",
  gold_evidence_span: "证据跨度",
  source_document: "文档名",
  source_section: "章节路径",
};
const CONTENT_FIELDS = ["question", "reference_answer", "gold_evidence_span"];

/* The banner answers the one question the flag leaves open: would clearing this
   row publish the customer name, or only record it in a workbench file? */
function renderVerdict(candidate) {
  const box = element("verdict");
  box.textContent = "";
  const { fields, content_exposed, metadata_only } = candidate.occurrences;

  const title = document.createElement("div");
  title.className = "verdict-title";
  const body = document.createElement("div");
  body.className = "verdict-body";

  if (content_exposed) {
    box.className = "verdict content";
    title.textContent = "客户标识出现在会发布的题目内容里";
    body.textContent =
      "放行这一行，客户名会随基准一起进入题干、答案或证据跨度。请判断是否已获授权公开；" +
      "若要脱敏，改参考答案并判 ANONYMIZED，不要改证据跨度。";
  } else if (metadata_only) {
    box.className = "verdict metadata";
    title.textContent = "已配置的标识只出现在来源元数据里";
    body.textContent =
      "命中来自文档名或章节路径，这些字段只留在工作台文件里，不随基准发布。但高亮只覆盖已配置的标识，" +
      "正文里的第三方企业或人名推导不出来，仍需通读题干、答案与证据跨度后再下结论。";
  } else {
    box.className = "verdict none";
    title.textContent = "本行未命中任何已配置的敏感标识";
    body.textContent =
      "这不等于本行干净：预筛标记可能来自其它规则，也可能是正文里的第三方主体而" +
      "自动推导覆盖不到。请通读全文；若发现新主体，用 --identifier 补充后重开再给结论。";
  }
  box.append(title, body);

  /* The verdict is only as good as the strings being matched, so name them. */
  const basis = document.createElement("div");
  basis.className = "verdict-body";
  basis.textContent = candidate.identifiers.length
    ? `匹配标识：${candidate.identifiers.join("、")}`
    : "未配置任何敏感标识：本视图无法定位客户名，请用 --identifier 指定后重开。";
  box.append(basis);

  if (fields.length) {
    const tags = document.createElement("div");
    tags.className = "verdict-fields";
    fields.forEach((field) => {
      const tag = document.createElement("span");
      tag.className = CONTENT_FIELDS.includes(field) ? "field-tag content" : "field-tag";
      tag.textContent = FIELD_NAMES[field] || field;
      tags.append(tag);
    });
    box.append(tags);
  }
}

function markField(id, field, candidate) {
  const node = element(id);
  const hit = candidate.occurrences.fields.includes(field);
  node.className = hit ? "where hit" : "where clean";
  node.textContent = hit ? "含客户标识" : "无命中";
}

function alertLine(label, text, tone) {
  const box = document.createElement("div");
  box.className = `alert ${tone}`;
  const strong = document.createElement("strong");
  strong.textContent = label;
  const body = document.createElement("span");
  body.textContent = text;
  box.append(strong, body);
  element("alerts").append(box);
}

function renderAlerts(candidate) {
  element("alerts").textContent = "";
  if (!candidate.unit_resolved) {
    alertLine(
      "语料未加载",
      "找不到证据所在单元的原文，无法显示上下文。请确认启动时 --corpus 指向了该项目的 corpus.json。",
      "warn"
    );
  } else if (!candidate.span_in_unit) {
    alertLine(
      "证据跨度已与语料不一致",
      "这段跨度不再逐字出现在语料单元里，finalize_qa_review.py 会以「片段不在单元内」拒绝该题。" +
        "若这是为脱敏而做的就地改写，请改回原文，用参考答案脱敏或直接判 REJECTED。",
      "bad"
    );
  }
  if (state.status === "REJECTED") {
    alertLine(
      "连带后果",
      "判 REJECTED 会同时把 approval_status 改为 REJECTED，该题不进入基准。",
      "warn"
    );
  }
}

function renderCandidate(candidate) {
  state.candidate = candidate;
  state.status = candidate.privacy_review_status || "";

  element("qa-id").textContent = candidate.qa_id;
  element("project").textContent = candidate.project_id;
  element("question-type").textContent = candidate.question_type || "—";
  element("flags").textContent = candidate.privacy_flags || "—";
  element("counter").textContent = `${candidate.index + 1} / ${candidate.total}`;

  renderVerdict(candidate);
  markField("where-question", "question", candidate);
  markField("where-answer", "reference_answer", candidate);
  markField("where-span", "gold_evidence_span", candidate);
  renderRuns(element("question"), candidate.question);
  renderRuns(element("answer"), candidate.reference_answer);
  renderRuns(element("span"), candidate.gold_evidence_span);
  renderRuns(element("source-document"), candidate.source_document);
  renderRuns(element("source-section"), candidate.source_section);
  renderRuns(element("unit-text"), candidate.unit_text);
  element("unit-id").textContent = candidate.gold_evidence_unit_id || "";
  element("unit-card").hidden = !candidate.unit_resolved;

  const badges = element("span-badges");
  badges.textContent = "";
  const badge = document.createElement("span");
  badge.className = candidate.span_in_unit ? "badge good" : "badge warn";
  badge.textContent = candidate.span_in_unit ? "与语料逐字一致" : "与语料不一致";
  if (candidate.unit_resolved) badges.append(badge);

  const noteCard = element("content-note-card");
  noteCard.hidden = !candidate.review_notes;
  element("content-note").textContent = candidate.review_notes || "";

  element("notes").value = candidate.privacy_review_notes || "";
  syncOptions();
  renderAlerts(candidate);
  message("", "");
}

function syncOptions() {
  document.querySelectorAll(".option").forEach((button) => {
    button.classList.toggle("active", state.status === button.dataset.value);
  });
  const needsReason = state.status === "ANONYMIZED" || state.status === "REJECTED";
  element("notes").placeholder = needsReason
    ? state.status === "ANONYMIZED"
      ? "必填：写明替换规则，例如「客户名 → 甲方企业A，全文一致」"
      : "必填：写明无法安全脱敏的理由"
    : "备注（选填）";
}

function renderProgress(progress) {
  const ratio = progress.total ? progress.decided / progress.total : 0;
  element("progress-fill").style.width = `${(ratio * 100).toFixed(1)}%`;

  const stats = element("stats");
  stats.textContent = "";
  const overall = document.createElement("span");
  overall.className = progress.remaining === 0 ? "stat done" : "stat";
  overall.textContent = `已判 ${progress.decided} · 剩余 ${progress.remaining} · 放行 ${progress.cleared}`;
  stats.append(overall);
  Object.entries(progress.status_counts).forEach(([status, count]) => {
    if (!count) return;
    const chip = document.createElement("span");
    chip.className = "stat";
    chip.textContent = `${status} ${count}`;
    stats.append(chip);
  });
  const exposed = document.createElement("span");
  exposed.className = "stat";
  exposed.textContent = `内容含客户标识 ${progress.content_exposed} 行`;
  stats.append(exposed);
}

function message(text, tone) {
  const node = element("message");
  node.textContent = text;
  node.className = tone ? `message ${tone}` : "message";
}

function showFinished(progress) {
  const cleared = progress ? progress.cleared : 0;
  document.querySelector("main").innerHTML =
    `<div class="done-banner"><h2>P01 全部 ${progress ? progress.total : 27} 行已完成隐私复核</h2>` +
    `<p>放行 ${cleared} 行。接下来运行 ` +
    "<code>python3 scripts/import_review_workbooks.py --qa-projects P01</code> 导入，" +
    "再运行 <code>python3 scripts/finalize_qa_review.py</code> 写出 " +
    "<code>qa_approved.json</code>，最后跑 " +
    "<code>python3 scripts/audit_formal_readiness.py</code> 确认放行。</p></div>";
}

/* ---------- actions ---------- */

function choose(value) {
  state.status = state.status === value ? "" : value;
  syncOptions();
  renderAlerts(state.candidate);
}

async function save() {
  try {
    const payload = await send("/api/decide", {
      index: state.candidate.index,
      status: state.status,
      notes: element("notes").value,
    });
    renderProgress(payload.progress);
    if (payload.finished) showFinished(payload.progress);
    else {
      renderCandidate(payload.candidate);
      message("已写回工作簿", "saved");
    }
  } catch (error) {
    message(error.message, "error");
  }
}

async function goto(index) {
  const payload = await send("/api/goto", { index });
  renderProgress(payload.progress);
  renderCandidate(payload.candidate);
}

async function gotoUndecided() {
  const payload = await send("/api/next-undecided", { index: state.candidate.index });
  renderProgress(payload.progress);
  if (payload.finished) showFinished(payload.progress);
  else renderCandidate(payload.candidate);
}

/* ---------- wiring ---------- */

document.querySelectorAll(".option").forEach((button) => {
  button.addEventListener("click", () => choose(button.dataset.value));
});

element("save").addEventListener("click", save);
element("prev").addEventListener("click", () => goto(state.candidate.index - 1));
element("next").addEventListener("click", () => goto(state.candidate.index + 1));
element("jump-undecided").addEventListener("click", gotoUndecided);
element("help-toggle").addEventListener("click", () => {
  element("help").hidden = !element("help").hidden;
});
element("help").addEventListener("click", (event) => {
  if (event.target === element("help")) element("help").hidden = true;
});

const KEYS = {
  1: "NO_RISK",
  2: "ANONYMIZED",
  3: "CLEARED",
  4: "REJECTED",
};

document.addEventListener("keydown", (event) => {
  const notes = element("notes");
  if (event.target === notes) {
    if (event.key === "Escape") notes.blur();
    if (event.key === "Enter") save();
    return;
  }
  if (!state.candidate) return;
  if (KEYS[event.key]) {
    event.preventDefault();
    choose(KEYS[event.key]);
  } else if (event.key === "Enter") {
    event.preventDefault();
    save();
  } else if (event.key === "n") {
    event.preventDefault();
    notes.focus();
  } else if (event.key === "u") {
    event.preventDefault();
    gotoUndecided();
  } else if (event.key === "ArrowLeft") {
    goto(state.candidate.index - 1);
  } else if (event.key === "ArrowRight") {
    goto(state.candidate.index + 1);
  } else if (event.key === "?") {
    element("help").hidden = !element("help").hidden;
  } else if (event.key === "Escape") {
    element("help").hidden = true;
  }
});

(async function start() {
  const response = await fetch("/api/state");
  const payload = await response.json();
  renderProgress(payload.progress);
  if (payload.finished) showFinished(payload.progress);
  else renderCandidate(payload.candidate);
})();
