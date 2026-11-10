/* Keyboard-first client for the QA approval workbook.
   Validation lives on the server so this file cannot drift from the rules the
   promotion step enforces: a rejected save simply shows the reason it returned. */

const state = { candidate: null, approval: "", privacy: "", difficulty: "" };

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

function badge(target, text, tone) {
  const span = document.createElement("span");
  span.className = tone ? `badge ${tone}` : "badge";
  span.textContent = text;
  target.append(span);
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
  const alerts = element("alerts");
  alerts.textContent = "";
  if (candidate.span_missing) {
    alertLine(
      "证据已失效",
      "这段证据在当前语料里找不到了，放行也会被导入步骤退回。请判 REJECTED 并在备注写明。",
      "bad"
    );
  }
  if (candidate.over_ceiling) {
    alertLine(
      "证据不独特",
      `这段原文在语料中出现 ${candidate.occurrences} 处，超过独特性上限，检索无法判定命中哪一处。应判 REJECTED。`,
      "bad"
    );
  } else if (candidate.occurrences > 1) {
    alertLine(
      "多处证据",
      `这段原文在语料中出现 ${candidate.occurrences} 处，命中任一处都算正确。放行前请确认下面每一处的答案都成立。`,
      "warn"
    );
  }
  if (candidate.needs_role_check) {
    alertLine(
      "表头存疑",
      "证据所在表格的表头有重复或占位列名，答案归属哪一列字符匹配查不出来。请核对高亮单元格的列语义，并把结论写进备注。",
      "warn"
    );
  }
  if (candidate.candidate_state === "CANDIDATE_SUPPLEMENT") {
    alertLine("补写草稿", "这条是为补足项目最低题数而补写的草稿，未经过第一轮预审，请逐字核对。", "info");
  }
  if (candidate.candidate_state === "CANDIDATE_REWRITE") {
    alertLine("经改写", "题干或答案经过改写并通过机械校验，仍需你确认改写后没有引入证据以外的信息。", "info");
  }
  if (!alerts.children.length) alerts.textContent = "";
}

function renderSegments(target, segments) {
  target.textContent = "";
  segments.forEach((segment) => {
    const node = segment.match ? document.createElement("mark") : document.createElement("span");
    node.textContent = segment.text;
    target.append(node);
  });
}

function renderEvidence(target, evidence) {
  target.textContent = "";
  if (evidence.kind !== "table") {
    renderSegments(target, evidence.segments);
    return;
  }
  /* The two rows are shown as they sit in the document. Rendering them as
     header-and-value pairs would assert a column semantics that some of these
     tables do not have, which is the very thing the reviewer must judge. */
  const table = document.createElement("table");
  table.className = "grid";
  const caption = document.createElement("caption");
  caption.textContent = "上行为该表首行，下行为证据所在行";
  const headRow = document.createElement("tr");
  const bodyRow = document.createElement("tr");
  evidence.columns.forEach((column) => {
    const head = document.createElement("th");
    head.textContent = column.header || "—";
    const body = document.createElement("td");
    body.textContent = column.value;
    if (!column.value) body.className = "empty";
    if (column.match) {
      head.classList.add("match");
      body.classList.add("match");
    }
    headRow.append(head);
    bodyRow.append(body);
  });
  const header = document.createElement("thead");
  header.append(headRow);
  const body = document.createElement("tbody");
  body.append(bodyRow);
  table.append(caption, header, body);

  const scroller = document.createElement("div");
  scroller.className = "grid-scroll";
  scroller.append(table);
  target.append(scroller);

  if (evidence.collapsed > 0) {
    const note = document.createElement("div");
    note.className = "collapsed-note";
    note.textContent = `已合并 ${evidence.collapsed} 个重复的合并单元格列`;
    target.append(note);
  }
}

function renderLocations(candidate) {
  const container = element("locations");
  container.textContent = "";
  if (!candidate.locations.length) return;

  const head = document.createElement("div");
  head.className = "locations-head";
  head.textContent =
    candidate.occurrences === 1
      ? "证据所在位置"
      : `证据所在位置 · 共 ${candidate.occurrences} 处，答案须在每一处都成立`;
  container.append(head);

  candidate.locations.forEach((location, index) => {
    const card = document.createElement("article");
    card.className = "location";
    const cardHead = document.createElement("div");
    cardHead.className = "location-head";

    const number = document.createElement("span");
    number.className = "location-index";
    number.textContent = `${index + 1}.`;
    const document_ = document.createElement("span");
    document_.className = "location-document";
    document_.textContent = location.document;
    const unit = document.createElement("span");
    unit.className = "location-unit";
    unit.textContent = `${location.unit_id} · ${location.block_type}`;
    cardHead.append(number, document_, unit);

    if (location.heading_path) {
      const path = document.createElement("span");
      path.className = "location-path";
      path.textContent = location.heading_path;
      cardHead.append(path);
    }

    const body = document.createElement("div");
    body.className = "evidence";
    renderEvidence(body, location.evidence);

    card.append(cardHead, body);
    container.append(card);
  });

  if (candidate.locations_hidden > 0) {
    const note = document.createElement("div");
    note.className = "collapsed-note";
    note.textContent = `另有 ${candidate.locations_hidden} 处未展示`;
    container.append(note);
  }
}

function renderCandidate(candidate) {
  state.candidate = candidate;
  state.approval = candidate.decision.approval_status;
  state.privacy = candidate.decision.privacy_review_status;
  state.difficulty = candidate.decision.difficulty;

  element("qa-key").textContent = candidate.qa_key;
  element("project").textContent = candidate.project_id;
  element("state").textContent = candidate.candidate_state;
  element("question-type").textContent = candidate.question_type || "—";
  element("counter").textContent = `${candidate.index + 1} / ${candidate.total}`;

  element("question").textContent = candidate.question;
  element("answer").textContent = candidate.reference_answer;
  element("span").textContent = candidate.gold_evidence_span;

  const answerBadges = element("answer-badges");
  answerBadges.textContent = "";
  if (candidate.answer_is_substring) badge(answerBadges, "答案是证据子串", "good");
  else badge(answerBadges, "答案为改述，需人工判断可回溯性", "warn");
  if (candidate.answer_grounding) badge(answerBadges, candidate.answer_grounding);

  const spanBadges = element("span-badges");
  spanBadges.textContent = "";
  badge(
    spanBadges,
    `语料中 ${candidate.occurrences} 处`,
    candidate.occurrences === 1 ? "good" : "warn"
  );

  renderAlerts(candidate);
  renderLocations(candidate);

  const prereview = [candidate.prereview_notes, candidate.suggested_privacy_flags]
    .filter(Boolean)
    .join("\n");
  element("prereview-card").hidden = !prereview;
  element("prereview").textContent = prereview;

  element("difficulty-hint").textContent = candidate.suggested_difficulty
    ? `预审建议 ${candidate.suggested_difficulty}（按 s 采纳）`
    : "无预审建议";

  element("notes").value = candidate.decision.review_notes || "";
  element("message").textContent = "";
  element("message").className = "message";
  syncOptions();
  window.scrollTo({ top: 0 });
}

function syncOptions() {
  const chosen = { approval: state.approval, privacy: state.privacy, difficulty: state.difficulty };
  document.querySelectorAll(".option").forEach((button) => {
    button.classList.toggle("active", chosen[button.dataset.field] === button.dataset.value);
  });
  // A rejected row must carry no difficulty, so the control is taken away rather
  // than left to be cleared by hand.
  element("difficulty-group").classList.toggle("disabled", state.approval === "REJECTED");
}

function renderProgress(progress) {
  const ratio = progress.total ? progress.decided / progress.total : 0;
  element("progress-fill").style.width = `${(ratio * 100).toFixed(1)}%`;

  const stats = element("stats");
  stats.textContent = "";
  const overall = document.createElement("span");
  overall.className = "stat";
  overall.textContent = `已判 ${progress.decided} · 剩余 ${progress.remaining} · 放行 ${progress.approved} · 拒绝 ${progress.rejected}`;
  stats.append(overall);
  Object.entries(progress.projects).forEach(([project, entry]) => {
    const chip = document.createElement("span");
    chip.className = entry.decided === entry.total ? "stat done" : "stat";
    chip.textContent = `${project} ${entry.decided}/${entry.total}`;
    stats.append(chip);
  });
}

function message(text, tone) {
  const node = element("message");
  node.textContent = text;
  node.className = tone ? `message ${tone}` : "message";
}

function showFinished() {
  document.querySelector("main").innerHTML =
    '<div class="done-banner"><h2>225 行已全部终审</h2>' +
    "<p>接下来运行 <code>python3 scripts/import_qa_approval_workbook.py</code> 先看报告，" +
    "无问题后加 <code>--write</code> 写出 <code>qa_approved.json</code>。</p></div>";
}

/* ---------- actions ---------- */

function choose(field, value) {
  state[field] = state[field] === value ? "" : value;
  if (field === "approval" && state.approval === "REJECTED") state.difficulty = "";
  syncOptions();
}

async function save() {
  try {
    const payload = await send("/api/decide", {
      index: state.candidate.index,
      approval: state.approval,
      privacy: state.privacy,
      difficulty: state.difficulty,
      notes: element("notes").value,
    });
    renderProgress(payload.progress);
    if (payload.finished) showFinished();
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
  if (payload.finished) showFinished();
  else renderCandidate(payload.candidate);
}

/* ---------- wiring ---------- */

document.querySelectorAll(".option").forEach((button) => {
  button.addEventListener("click", () => choose(button.dataset.field, button.dataset.value));
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
  a: ["approval", "APPROVED"],
  r: ["approval", "REJECTED"],
  1: ["privacy", "NO_RISK"],
  2: ["privacy", "ANONYMIZED"],
  3: ["privacy", "CLEARED"],
  4: ["privacy", "REJECTED"],
  e: ["difficulty", "Easy"],
  m: ["difficulty", "Medium"],
  h: ["difficulty", "Hard"],
};

document.addEventListener("keydown", (event) => {
  const notes = element("notes");
  if (event.target === notes) {
    if (event.key === "Escape") notes.blur();
    if (event.key === "Enter") save();
    return;
  }
  if (!state.candidate) return;
  const binding = KEYS[event.key];
  if (binding) {
    event.preventDefault();
    if (binding[0] === "difficulty" && state.approval === "REJECTED") return;
    choose(binding[0], binding[1]);
  } else if (event.key === "s") {
    event.preventDefault();
    if (state.candidate.suggested_difficulty && state.approval !== "REJECTED") {
      choose("difficulty", state.candidate.suggested_difficulty);
    }
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
  if (payload.finished) showFinished();
  else renderCandidate(payload.candidate);
})();
