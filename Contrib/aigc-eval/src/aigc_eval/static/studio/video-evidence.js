import {
  annotationOf,
  boxFromBbox,
  clipFromReport,
  jsonHTMLFor,
} from "./video-evidence-model.js";
import { GLYPH, TIER } from "./verdict-glyphs.js";
import { icon } from "../shared/icons.js";

const TIER_GLYPH = {
  bad: TIER.bad.glyph,
  flaw: TIER.flaw.glyph,
  review: GLYPH.review,
};

const TIER_LABEL = { bad: "恶劣错误", flaw: "瑕疵", review: "未采信" };

/**
 * Stateful problem-location video component.
 *
 * The parent Studio owns jobs and reports; this component owns clip
 * navigation, evidence filtering, annotation boxes and media event wiring.
 */
export function createVideoEvidence({ elements, escapeHtml }) {
  let wired = false;
  const state = {
    clip: null,
    markIndex: -1,
    pickedBoxes: null,
    openJson: new Set(),
    pendingSeek: null,
    metaReady: false,
    tierFilter: null,
    volTouched: false,
    volMuted: true,
    volLevel: 1,
  };

  function fmtClock(seconds) {
    if (!Number.isFinite(seconds)) return "0:00";
    return Math.floor(seconds / 60) + ":" + String(Math.floor(seconds % 60)).padStart(2, "0");
  }

  function currentClip() {
    return state.clip;
  }

  function visibleMarks() {
    const clip = currentClip();
    if (!clip) return [];
    if (state.tierFilter == null) return clip.marks;
    return clip.marks.filter((mark) => mark.tier === state.tierFilter);
  }

  function defaultPicked(mark) {
    const picked = new Set();
    if (!mark) return picked;
    const firstBoxed = mark.cards.findIndex((card) => boxFromBbox(card.bbox));
    picked.add(firstBoxed >= 0 ? firstBoxed : 0);
    return picked;
  }

  function pickedSet(mark) {
    if (!state.pickedBoxes) state.pickedBoxes = defaultPicked(mark);
    return state.pickedBoxes;
  }

  function pickedCards(mark) {
    if (!mark) return [];
    const picked = pickedSet(mark);
    return mark.cards.filter((card, index) => picked.has(index));
  }

  function renderTags() {
    const mark = visibleMarks()[state.markIndex];
    if (!elements.jTags) return;
    if (!mark) {
      elements.jTags.innerHTML = "";
      if (elements.jHint) elements.jHint.hidden = false;
      return;
    }
    if (elements.jHint) elements.jHint.hidden = true;
    const picked = pickedSet(mark);
    elements.jTags.innerHTML = mark.cards.map((card, index) => {
      const selectedClass = picked.has(index) ? " on" : "";
      const expanded = state.openJson.has(index);
      const tier = card.tier || "review";
      const title = card.title || card.ref_id || "问题";
      const boxed = !!boxFromBbox(card.bbox);
      const noBox = boxed
        ? ""
        : `<span class="tagnobox" title="这条问题没有画面定位框（多为整片级属性，如运镜、音画同步、时序一致性）">◇</span>`;
      return `<div class="tagrow">
        <div class="tagline">
          <button class="tagbtn${selectedClass}${boxed ? "" : " nobox"}" type="button" data-tag="${index}"
                  aria-pressed="${picked.has(index)}" title="${escapeHtml(title)}">
            <span class="tagbox" aria-hidden="true">${icon("checkmark", { size: 10, stroke: 2.4 })}</span>
            <span class="tagtier" data-tier="${tier}" title="${escapeHtml(TIER_LABEL[tier] || "")}">
              ${TIER_GLYPH[tier] || TIER_GLYPH.review}
              <span class="sr-only">${escapeHtml(TIER_LABEL[tier] || "")}</span>
            </span>
            <span class="tagtext">
              <span class="tagtitle">${escapeHtml(title)}</span>
              ${card.note ? `<span class="tagnote">${escapeHtml(card.note)}</span>` : ""}
            </span>
            ${noBox}
          </button>
          <button class="tagjson-btn${expanded ? " on" : ""}" type="button" data-json="${index}"
                  title="查看这条的原始 JSON" aria-expanded="${expanded}">${
            icon("chevronRight", { size: 10, stroke: 2, cls: "tagjson-chev" })
          }JSON</button>
        </div>
        ${expanded ? `<pre class="json">${jsonHTMLFor([card])}</pre>` : ""}
      </div>`;
    }).join("");
  }

  // Annotation percentages are relative to the video's contained content,
  // not the wrapper including letterbox bars.
  function fitAnnotationLayer() {
    if (!elements.vWrap || !elements.vEl) return;
    const containerWidth = elements.vWrap.clientWidth;
    const containerHeight = elements.vWrap.clientHeight;
    const sourceWidth = elements.vEl.videoWidth || 16;
    const sourceHeight = elements.vEl.videoHeight || 9;
    if (!containerWidth || !containerHeight) return;
    const scale = Math.min(containerWidth / sourceWidth, containerHeight / sourceHeight);
    const width = sourceWidth * scale;
    const height = sourceHeight * scale;
    elements.vLayer.style.width = width + "px";
    elements.vLayer.style.height = height + "px";
    elements.vLayer.style.left = (containerWidth - width) / 2 + "px";
    elements.vLayer.style.top = (containerHeight - height) / 2 + "px";
  }

  function activeMarks() {
    const marks = visibleMarks();
    if (!marks.length) return [];
    const currentTime = elements.vEl.currentTime || 0;
    const active = new Set();
    marks.forEach((mark, index) => {
      if (currentTime >= mark.time && currentTime < mark.time + mark.hold) active.add(index);
    });
    if (
      elements.vEl.paused
      && state.markIndex >= 0
      && state.markIndex < marks.length
    ) {
      active.add(state.markIndex);
    }
    return [...active];
  }

  function renderBoxes() {
    const marks = visibleMarks();
    if (!elements.vLayer) return;
    const picked = state.markIndex >= 0 && marks[state.markIndex]
      ? pickedCards(marks[state.markIndex])
      : [];
    elements.vLayer.innerHTML = activeMarks().map((index) => {
      const mark = marks[index];
      if (!mark) return "";
      const boxes = index === state.markIndex
        ? mark.boxes.filter(({ card }) => picked.includes(card))
        : mark.boxes;
      return boxes.map(({ card, box }) => {
        const nearTop = box.y < 8 ? " near-top" : "";
        const label = [card.title || card.ref_id, card.target].filter(Boolean).join(" · ");
        return `<div class="annotation-box${nearTop}" data-tier="${card.tier || "review"}"
          style="left:${box.x}%;top:${box.y}%;width:${box.w}%;height:${box.h}%"><span>${escapeHtml(label)}</span></div>`;
      }).join("");
    }).join("");
  }

  function clipDuration() {
    const marks = visibleMarks();
    if (Number.isFinite(elements.vEl.duration) && elements.vEl.duration > 0) {
      return elements.vEl.duration;
    }
    return marks.length ? Math.max(...marks.map((mark) => mark.time)) + 3 : 1;
  }

  function markActive(index) {
    elements.vJumps?.querySelectorAll(".jump").forEach((jump) => {
      jump.classList.toggle("act", Number(jump.dataset.i) === index);
    });
    elements.vTrack?.querySelectorAll(".mark").forEach((mark) => {
      mark.classList.toggle("act", Number(mark.dataset.i) === index);
    });
  }

  function gotoMark(index, seek) {
    const marks = visibleMarks();
    const mark = marks[index];
    if (!mark) return;
    if (index !== state.markIndex) {
      state.pickedBoxes = null;
      state.openJson.clear();
    }
    state.markIndex = index;
    if (seek) {
      if (state.metaReady && Number.isFinite(elements.vEl.duration)) {
        elements.vEl.currentTime = mark.time;
        elements.vEl.pause();
      } else {
        state.pendingSeek = mark.time;
      }
    }
    renderTags();
    markActive(index);
    renderBoxes();
  }

  function renderMarks() {
    if (!elements.vTrack) return;
    elements.vTrack.querySelectorAll(".mark").forEach((mark) => mark.remove());
    const duration = clipDuration();
    visibleMarks().forEach((mark, index) => {
      const dot = document.createElement("div");
      dot.className = "mark";
      dot.dataset.tier = mark.tier;
      dot.dataset.i = String(index);
      dot.style.left = Math.min(99, (mark.time / duration) * 100) + "%";
      const titles = mark.cards.map((card) => card.title || card.ref_id).filter(Boolean).join("、");
      dot.title = `${mark.time.toFixed(1)}s · ${titles}`;
      dot.onclick = (event) => {
        event.stopPropagation();
        gotoMark(index, true);
      };
      elements.vTrack.appendChild(dot);
    });
    if (state.markIndex >= 0) markActive(state.markIndex);
  }

  function renderJumps() {
    if (!elements.vJumps) return;
    const marks = visibleMarks();
    elements.vJumps.innerHTML = "";
    marks.forEach((mark, index) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "jump";
      button.dataset.i = String(index);
      const extra = mark.cards.length > 1 ? `<span class="jn">×${mark.cards.length}</span>` : "";
      const first = mark.cards[0] || {};
      button.innerHTML = `<span class="jd" data-tier="${mark.tier}">${TIER_GLYPH[mark.tier] || TIER_GLYPH.review}</span>`
        + `${mark.time.toFixed(1)}s`
        + ` ${escapeHtml(String(first.title || first.ref_id || "问题").slice(0, 14))}${extra}`;
      button.onclick = () => gotoMark(index, true);
      elements.vJumps.appendChild(button);
    });
  }

  function renderFilters() {
    const clip = currentClip();
    if (!elements.vpaneFilters || !clip) return;
    const chips = [{ value: "", label: `全部 ${clip.marks.length}`, glyph: "" }].concat(
      ["bad", "flaw", "review"]
        .map((tier) => ({
          value: tier,
          glyph: TIER_GLYPH[tier],
          count: clip.marks.filter((mark) => mark.tier === tier).length,
        }))
        .filter((chip) => chip.count)
        .map((chip) => ({ ...chip, label: `${TIER_LABEL[chip.value]} ${chip.count}` })),
    );
    elements.vpaneFilters.innerHTML = chips.map((chip) => {
      const selectedClass = (state.tierFilter || "") === chip.value ? " on" : "";
      return `<button class="vfilter${selectedClass}" type="button" data-tier="${chip.value}"
        >${chip.glyph || ""}${escapeHtml(chip.label)}</button>`;
    }).join("");
  }

  function renderOrphans() {
    const clip = currentClip();
    if (!elements.vOrphans || !clip) return;
    if (!clip.orphans.length) {
      elements.vOrphans.hidden = true;
      return;
    }
    elements.vOrphans.hidden = false;
    elements.vOrphansHead.textContent = `另有 ${clip.orphans.length} 条问题没有时间戳，无法定位到时间轴`;
    elements.vOrphansList.innerHTML = clip.orphans.map((card) => `
      <li class="vorphan">
        <span class="vorphan-title">${escapeHtml(card.title || card.ref_id || "问题")}</span>
        <span>${escapeHtml(card.note || "")}</span>
      </li>`).join("");
  }

  function loadClip() {
    const clip = state.clip;
    if (!clip) return;
    state.markIndex = -1;
    state.pickedBoxes = null;
    state.openJson.clear();
    state.pendingSeek = null;
    state.tierFilter = null;
    renderFilters();
    renderOrphans();
    renderTags();
    elements.vpaneCount.textContent = clip.marks.length
      ? `${clip.marks.length} 个问题时刻 · ${clip.marks.reduce((count, mark) => count + mark.boxes.length, 0)} 个定位框`
      : "本条视频没有需要定位的问题";
    elements.vEl.style.opacity = "0";
    elements.vLayer.style.opacity = "0";
    elements.vLayer.innerHTML = "";
    state.metaReady = false;
    elements.vEl.src = clip.src;
    elements.vEl.load();
    if (state.volTouched) {
      elements.vEl.muted = state.volMuted;
      elements.vEl.volume = state.volLevel;
    } else {
      elements.vEl.muted = true;
    }
    elements.vFill.style.width = "0%";
    renderJumps();
    renderMarks();
    if (clip.marks.length) gotoMark(0, true);
  }

  function mountReport(report) {
    const clip = clipFromReport(report, "待质检视频");
    if (!clip.src) {
      state.clip = null;
      elements.videoPane.hidden = true;
      return;
    }
    state.clip = clip;
    elements.videoPane.hidden = false;
    loadClip();
    window.requestAnimationFrame(fitAnnotationLayer);
  }

  /* 判定区里点一个 ISSUE-* 药丸就跳到它的证据时刻。筛选器可能正把它藏着，
   * 所以先解除筛选再定位，否则点了没反应。 */
  function focusIssue(issueId) {
    const clip = currentClip();
    if (!clip || !issueId) return;
    const hit = clip.marks.findIndex(
      (mark) => mark.cards.some((card) => (card.issue_id || card.ref_id) === issueId),
    );
    if (hit < 0) return;
    if (state.tierFilter !== null) {
      state.tierFilter = null;
      renderFilters();
      renderJumps();
      renderMarks();
    }
    gotoMark(hit, true);
    elements.videoPane.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }

  function rememberVolume() {
    state.volTouched = true;
    state.volMuted = elements.vEl.muted;
    state.volLevel = elements.vEl.volume;
  }

  function refit() {
    fitAnnotationLayer();
    renderBoxes();
  }

  function reset() {
    state.clip = null;
    state.markIndex = -1;
    state.pickedBoxes = null;
    state.openJson.clear();
    state.pendingSeek = null;
    state.metaReady = false;
    state.tierFilter = null;
    elements.videoPane.hidden = true;
    elements.vEl.pause();
    elements.vEl.removeAttribute("src");
    elements.vEl.load();
    elements.vLayer.innerHTML = "";
    elements.vpaneFilters.innerHTML = "";
    elements.vTrack.querySelectorAll(".mark").forEach((mark) => mark.remove());
    elements.vJumps.innerHTML = "";
    elements.vOrphans.hidden = true;
    elements.vOrphansList.innerHTML = "";
    elements.jTags.innerHTML = "";
    if (elements.jHint) elements.jHint.hidden = false;
    elements.vpaneCount.textContent = "";
    elements.vFill.style.width = "0%";
    elements.vTime.textContent = "0:00 / 0:00";
  }

  function wire() {
    if (wired) return;
    wired = true;
    elements.vEl.addEventListener("loadedmetadata", () => {
      state.metaReady = true;
      renderMarks();
      elements.vTime.textContent = "0:00 / " + fmtClock(elements.vEl.duration);
      fitAnnotationLayer();
      if (state.pendingSeek != null) {
        elements.vEl.currentTime = state.pendingSeek;
        state.pendingSeek = null;
        elements.vEl.pause();
      }
    });
    elements.vEl.addEventListener("loadeddata", () => {
      elements.vEl.style.opacity = "1";
      elements.vLayer.style.opacity = "1";
      refit();
    });
    elements.vEl.addEventListener("seeked", renderBoxes);
    elements.vEl.addEventListener("timeupdate", () => {
      const ratio = elements.vEl.currentTime / (elements.vEl.duration || 1);
      elements.vFill.style.width = ratio * 100 + "%";
      elements.vTime.textContent = fmtClock(elements.vEl.currentTime)
        + " / "
        + fmtClock(elements.vEl.duration);
      renderBoxes();
    });
    elements.vEl.addEventListener("pause", renderBoxes);
    elements.vEl.addEventListener("volumechange", rememberVolume);
    document.addEventListener("fullscreenchange", () => {
      window.requestAnimationFrame(refit);
    });
    window.addEventListener("resize", fitAnnotationLayer);

    let dragging = false;
    const seekAt = (event) => {
      const rect = elements.vTrack.getBoundingClientRect();
      const x = event.touches ? event.touches[0].clientX : event.clientX;
      const ratio = Math.max(0, Math.min(1, (x - rect.left) / rect.width));
      if (!state.metaReady || !Number.isFinite(elements.vEl.duration)) return;
      elements.vEl.currentTime = ratio * elements.vEl.duration;
    };
    elements.vTrack.addEventListener("mousedown", (event) => {
      if (event.target.classList.contains("mark")) return;
      dragging = true;
      seekAt(event);
    });
    document.addEventListener("mousemove", (event) => {
      if (dragging) seekAt(event);
    });
    document.addEventListener("mouseup", () => {
      dragging = false;
    });

    elements.vpaneFilters.addEventListener("click", (event) => {
      const chip = event.target.closest("[data-tier]");
      if (!chip) return;
      state.tierFilter = chip.dataset.tier || null;
      state.markIndex = -1;
      state.pickedBoxes = null;
      state.openJson.clear();
      renderFilters();
      renderJumps();
      renderMarks();
      renderTags();
      renderBoxes();
    });
    elements.jTags.addEventListener("click", (event) => {
      const jsonButton = event.target.closest("[data-json]");
      if (jsonButton) {
        const index = Number(jsonButton.dataset.json);
        if (state.openJson.has(index)) state.openJson.delete(index);
        else state.openJson.add(index);
        renderTags();
        return;
      }
      const tag = event.target.closest("[data-tag]");
      if (!tag) return;
      const mark = visibleMarks()[state.markIndex];
      if (!mark) return;
      const index = Number(tag.dataset.tag);
      const picked = pickedSet(mark);
      if (picked.has(index)) picked.delete(index);
      else picked.add(index);
      renderTags();
      renderBoxes();
    });
    elements.jCopy.onclick = () => {
      const mark = visibleMarks()[state.markIndex];
      if (!mark) return;
      const cards = pickedCards(mark);
      const annotations = (cards.length ? cards : mark.cards).map(annotationOf);
      const text = JSON.stringify(
        annotations.length === 1 ? annotations[0] : annotations,
        null,
        2,
      );
      // 只切 class：图标是 DOM 里的两个 SVG，写 textContent 会把它们抹掉。
      navigator.clipboard.writeText(text).then(() => {
        elements.jCopy.classList.add("copied");
        window.setTimeout(() => elements.jCopy.classList.remove("copied"), 1600);
      });
    };
  }

  return {
    focusIssue,
    mountReport,
    refit,
    reset,
    wire,
  };
}
