import { tierOfDimension } from "./verdict-glyphs.js";

const HOLD_DEFAULT = 1.2;

/* 时间轴上一个标记点的档位排序：坏的盖过轻的，未采信的垫底。 */
const TIER_WEIGHT = { bad: 3, flaw: 2, review: 1 };

/**
 * issue_id → 该问题在界面上的档位。
 *
 * 一条 issue 自己不带档位，它的分量来自引用它的维度：同一条事实可能同时压低
 * 两个维度，取其中最重的那个才不会让「恶劣错误」在时间轴上显示成「瑕疵」。
 * 只出现在 review_candidates 里的问题没有压低任何维度，单列 review 档 ——
 * 让它和确认的缺陷用同一种记号是错的。
 */
export function issueTiers(report) {
  const tiers = new Map();
  ((report && report.rubric) || []).forEach((dimension) => {
    const tier = tierOfDimension(dimension);
    const key = tier.key === "bad" || tier.key === "flaw" ? tier.key : "review";
    (dimension.subcriteria || []).forEach((criterion) => {
      (criterion.issue_ids || []).forEach((id) => {
        const previous = tiers.get(id);
        if (!previous || TIER_WEIGHT[key] > TIER_WEIGHT[previous]) tiers.set(id, key);
      });
    });
  });
  ((report && report.review_candidates) || []).forEach((issue) => {
    if (issue && issue.issue_id && !tiers.has(issue.issue_id)) {
      tiers.set(issue.issue_id, "review");
    }
  });
  return tiers;
}

/**
 * Convert normalized [x1, y1, x2, y2] coordinates to percentages.
 * Invalid or effectively empty boxes are intentionally ignored.
 */
export function boxFromBbox(bbox) {
  if (!Array.isArray(bbox) || bbox.length !== 4) return null;
  const values = bbox.map(Number);
  if (values.some((value) => !Number.isFinite(value))) return null;
  const x1 = Math.min(values[0], values[2]);
  const x2 = Math.max(values[0], values[2]);
  const y1 = Math.min(values[1], values[3]);
  const y2 = Math.max(values[1], values[3]);
  if (x2 - x1 < 0.01 || y2 - y1 < 0.01) return null;
  return {
    x: x1 * 100,
    y: y1 * 100,
    w: (x2 - x1) * 100,
    h: (y2 - y1) * 100,
  };
}

/** Return the stable annotation payload copied from the evidence panel. */
export function annotationOf(card) {
  return {
    issue_id: card.issue_id || card.ref_id || undefined,
    tier: card.tier || undefined,
    title: card.title || undefined,
    target: card.target || undefined,
    time: card.time || undefined,
    frame_time: card.frame_time != null ? Number(card.frame_time).toFixed(2) + "s" : undefined,
    grounding: card.bbox ? "bbox: [" + card.bbox.join(", ") + "]" : "无法定位到具体画面位置",
    decision_summary: card.decision_summary || undefined,
    reasoning: card.note || undefined,
  };
}

/**
 * Group report evidence by frame time so dense reports remain navigable.
 * Cards without a usable timestamp are retained as orphans.
 */
export function clipFromReport(report, label) {
  const tiers = issueTiers(report);
  const cards = ((report && report.evidence) || [])
    .filter((card) => card && typeof card === "object")
    // 证据卡按 ISSUE-* 主键回挂档位；对不上号的一律按 review 处理，
    // 宁可显得未定论，也不能让一条没有出处的框看起来像已确认的缺陷。
    .map((card) => ({ ...card, tier: tiers.get(card.issue_id || card.ref_id) || "review" }));
  const buckets = new Map();
  const orphans = [];
  cards.forEach((card) => {
    if (card.frame_time == null) {
      orphans.push(card);
      return;
    }
    const time = Math.round(Number(card.frame_time) * 10) / 10;
    if (!Number.isFinite(time)) {
      orphans.push(card);
      return;
    }
    if (!buckets.has(time)) buckets.set(time, []);
    buckets.get(time).push(card);
  });
  const marks = [...buckets.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([time, group]) => {
      const boxed = group.map((card) => ({ card, box: boxFromBbox(card.bbox) }));
      const tier = group.reduce(
        (worst, card) => (TIER_WEIGHT[card.tier] > TIER_WEIGHT[worst] ? card.tier : worst),
        "review",
      );
      return {
        time,
        tier,
        cards: group,
        boxes: boxed.filter((item) => item.box),
        hold: HOLD_DEFAULT,
      };
    });
  return {
    label: label || "待质检视频",
    src: (report && report.media && report.media.video_url) || "",
    marks,
    orphans,
  };
}

/** Syntax-highlight a copied annotation without inserting unescaped JSON. */
export function jsonHTMLFor(cards) {
  const annotations = cards.map(annotationOf);
  const body = annotations.length === 1 ? annotations[0] : annotations;
  const raw = JSON.stringify(body, null, 2).replace(
    /[&<>]/g,
    (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[char]),
  );
  return raw.replace(
    /("(?:[^"\\]|\\.)*")(\s*:)?|(-?\d+(?:\.\d+)?)/g,
    (whole, stringValue, colon, numberValue) => {
      if (numberValue !== undefined) return `<span class="jn">${numberValue}</span>`;
      if (colon) return `<span class="jk">${stringValue}</span><span class="jp">${colon}</span>`;
      return `<span class="js">${stringValue}</span>`;
    },
  );
}
