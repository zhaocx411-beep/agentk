/* Keyboard-first client for the boundary verification workbook.
   Every decision is posted immediately, so closing the tab loses nothing. */

const state = { candidate: null, progress: null, label: "", flagged: false };

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

function renderEvidence(target, evidence) {
  target.textContent = "";
  if (evidence.kind !== "table") {
    target.textContent = evidence.text || "（空）";
    return;
  }
  const wrapper = document.createElement("div");
  wrapper.className = "cells";
  evidence.columns.forEach((column) => {
    const cell = document.createElement("div");
    cell.className = "cell";
    const header = document.createElement("span");
    header.className = "cell-header";
    header.textContent = column.header || "—";
    const value = document.createElement("span");
    value.className = column.value ? "cell-value" : "cell-value empty";
    value.textContent = column.value;
    cell.append(header, value);
    wrapper.append(cell);
  });
  target.append(wrapper);
  if (evidence.collapsed > 0) {
    const note = document.createElement("div");
    note.className = "collapsed-note";
    note.textContent = `已合并 ${evidence.collapsed} 个重复的合并单元格列`;
    target.append(note);
  }
}

function renderSection(target, path, same) {
  target.className = path ? "section-path" : "section-path empty";
  target.textContent = "";
  if (!path) return;
  const label = document.createElement("span");
  label.textContent = path;
  if (same) label.className = "same";
  target.append(label);
}

function renderCandidate(candidate) {
  state.candidate = candidate;
  state.label = candidate.human_label || "";
  state.flagged = candidate.review_status === "NEEDS_DISCUSSION";

  element("candidate-id").textContent = candidate.candidate_id;
  element("project").textContent = candidate.project_id;
  element("stratum").textContent = candidate.boundary_stratum || "—";
  element("row-group").textContent =
    candidate.row_group === "FLAGGED_REVIEW" ? "难例（单独报告）" : "一致率样本";

  element("left-type").textContent = candidate.left.block_type;
  element("right-type").textContent = candidate.right.block_type;
  renderSection(element("left-section"), candidate.left.section, candidate.same_section);
  renderSection(element("right-section"), candidate.right.section, candidate.same_section);
  renderEvidence(element("left-evidence"), candidate.left.evidence);
  renderEvidence(element("right-evidence"), candidate.right.evidence);

  element("notes").value = candidate.review_notes || "";
  element("flag").checked = state.flagged;
  document.querySelectorAll(".label").forEach((button) => {
    button.classList.toggle("active", button.dataset.label === state.label);
  });
  element("counter").textContent = `${candidate.index + 1} / ${candidate.total}`;
}

function renderProgress(progress) {
  state.progress = progress;
  const ratio = progress.total ? progress.decided / progress.total : 0;
  element("progress-fill").style.width = `${(ratio * 100).toFixed(1)}%`;

  const stats = element("stats");
  stats.textContent = "";
  const overall = document.createElement("span");
  overall.className = "stat";
  overall.textContent = `已判 ${progress.decided} · 剩余 ${progress.remaining} · 歧义 ${progress.flagged}`;
  stats.append(overall);

  const counts = progress.label_counts;
  const distribution = document.createElement("span");
  distribution.className = "stat";
  distribution.textContent = `BREAK ${counts.BREAK} · NO_BREAK ${counts.NO_BREAK} · NOISE ${counts.NOISE}`;
  stats.append(distribution);

  Object.entries(progress.projects).forEach(([project, entry]) => {
    const chip = document.createElement("span");
    chip.className = entry.decided === entry.total ? "stat done" : "stat";
    chip.textContent = `${project} ${entry.decided}/${entry.total}`;
    stats.append(chip);
  });
}

function flashSaved() {
  const saved = element("saved");
  saved.textContent = "已写回工作簿";
  saved.classList.add("show");
  window.setTimeout(() => saved.classList.remove("show"), 900);
}

function showFinished() {
  document.querySelector("main").innerHTML =
    '<div class="done-banner"><h2>1,050 行已全部判定</h2>' +
    "<p>接下来运行 <code>python3 scripts/promote_llm_prelabels.py</code> 查看一致率与 kappa，" +
    "达标后加 <code>--write</code> 写出正式金标准。</p></div>";
}

/* ---------- actions ---------- */

async function decide(label) {
  const notes = element("notes").value.trim();
  const status = state.flagged ? "NEEDS_DISCUSSION" : "COMPLETE";
  if (state.flagged && !notes) {
    element("notes").focus();
    element("notes").placeholder = "标为歧义必须写明歧义来源";
    return;
  }
  try {
    const payload = await send("/api/decide", {
      index: state.candidate.index,
      label,
      status,
      notes,
      next: "undecided",
    });
    renderProgress(payload.progress);
    flashSaved();
    if (payload.finished) showFinished();
    else renderCandidate(payload.candidate);
  } catch (error) {
    element("saved").textContent = error.message;
    element("saved").classList.add("show");
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

document.querySelectorAll(".label").forEach((button) => {
  button.addEventListener("click", () => decide(button.dataset.label));
});

element("flag").addEventListener("change", (event) => {
  state.flagged = event.target.checked;
});

element("prev").addEventListener("click", () => goto(state.candidate.index - 1));
element("next").addEventListener("click", () => goto(state.candidate.index + 1));
element("jump-undecided").addEventListener("click", gotoUndecided);
element("help-toggle").addEventListener("click", () => {
  element("help").hidden = !element("help").hidden;
});
element("help").addEventListener("click", (event) => {
  if (event.target === element("help")) element("help").hidden = true;
});

document.addEventListener("keydown", (event) => {
  const notes = element("notes");
  if (event.target === notes) {
    if (event.key === "Escape") notes.blur();
    return;
  }
  if (!state.candidate) return;
  const keys = { 1: "BREAK", 2: "NO_BREAK", 3: "NOISE" };
  if (keys[event.key]) {
    event.preventDefault();
    decide(keys[event.key]);
  } else if (event.key === "d") {
    event.preventDefault();
    state.flagged = !state.flagged;
    element("flag").checked = state.flagged;
    if (state.flagged) notes.focus();
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
