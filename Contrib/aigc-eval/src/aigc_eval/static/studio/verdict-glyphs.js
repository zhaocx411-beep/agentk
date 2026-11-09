/* native8 三档与四判定的视觉编码表。
 *
 * 颜色只当第二信道。每个状态都另有一个**形状**（勾 / 叉 / 半填充 / 虚线问号 /
 * 短横）和一句中文，脱色打印或色觉障碍下仍然分得开。
 *
 * `not_observable` 与 `pass` 是最不能混淆的一对：把「没核实」读成「通过」会把
 * 一份不完整的报告读成完整的。所以虚线描边在整套编码里是 not_observable 独占的
 * 记号 —— 别的状态一律实线，别的地方也不要引入虚线圆环。
 */

function glyph(body, extra = "") {
  return `<svg class="vglyph" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true"
    fill="none" stroke="currentColor" stroke-width="1.5"
    stroke-linecap="round" stroke-linejoin="round"${extra}>${body}</svg>`;
}

export const GLYPH = {
  pass: glyph('<circle cx="8" cy="8" r="6.4" /><path d="M5.1 8.35 7.15 10.4 10.95 6.35" />'),

  violated: glyph('<circle cx="8" cy="8" r="6.4" /><path d="M5.75 5.75 10.25 10.25M10.25 5.75 5.75 10.25" />'),

  // 半个实心圆：一眼看出「不是满分也不是零分」，无需读色。
  flaw: glyph(
    '<circle cx="8" cy="8" r="6.4" />'
    + '<path d="M8 1.6a6.4 6.4 0 0 0 0 12.8z" fill="currentColor" stroke="none" />',
  ),

  notObservable: glyph(
    '<circle cx="8" cy="8" r="6.4" stroke-dasharray="2.2 2.1" />'
    + '<path d="M6.2 6.35a1.85 1.85 0 1 1 2.5 1.75c-.48.2-.76.6-.76 1.12v.28" />'
    + '<circle cx="8" cy="11.75" r=".92" fill="currentColor" stroke="none" />',
  ),

  notApplicable: glyph('<circle cx="8" cy="8" r="6.4" /><path d="M5.5 8h5" />'),

  // 复核 / 未采信的提示图标：与 notObservable 同族（虚线 + 问号），
  // 好让「程序没敢下结论」在整页里始终长一个样。
  review: glyph(
    '<path d="M8 1.9 14.1 12.6a1 1 0 0 1-.87 1.5H2.77a1 1 0 0 1-.87-1.5z" stroke-dasharray="2.4 2" />'
    + '<path d="M8 6.4v3" /><circle cx="8" cy="11.6" r=".9" fill="currentColor" stroke="none" />',
  ),

  align: glyph('<path d="M2.4 4.6h5.2v6.8H2.4zM8.4 4.6h5.2v6.8H8.4z" /><path d="M8 2.3v11.4" stroke-dasharray="1.6 1.8" />'),
};

/* 四个子判据判定。`key` 同时是 CSS 的 data-verdict 值。 */
export const VERDICT = {
  pass: { key: "pass", label: "通过", glyph: GLYPH.pass },
  violated: { key: "violated", label: "违反", glyph: GLYPH.violated },
  not_observable: { key: "not_observable", label: "未核实", glyph: GLYPH.notObservable },
  not_applicable: { key: "not_applicable", label: "不适用", glyph: GLYPH.notApplicable },
};

export function verdictOf(value) {
  return VERDICT[String(value || "")] || VERDICT.not_observable;
}

/* 维度档位。`unverified` 不是后端档位，见 tierOfDimension。 */
export const TIER = {
  bad: { key: "bad", label: "恶劣错误", glyph: GLYPH.violated },
  flaw: { key: "flaw", label: "瑕疵", glyph: GLYPH.flaw },
  ok: { key: "ok", label: "无问题", glyph: GLYPH.pass },
  unverified: { key: "unverified", label: "未完全核实", glyph: GLYPH.notObservable },
  na: { key: "na", label: "不适用", glyph: GLYPH.notApplicable },
};

/* 档位的展示顺序：坏的在前，"不适用" 垫底。计数条与筛选器都按它排。 */
export const TIER_ORDER = ["bad", "flaw", "unverified", "ok", "na"];

/**
 * 维度在界面上的档位。
 *
 * 关键分歧点：后端对「八条子判据全是 not_observable」的维度算出的 score 是 1.0，
 * 因为没有任何一条事实把它压下来。照搬就会印出一个绿色「无问题」，而真实情况是
 * 一条都没核实过 —— 这正是 native8 要消灭的假放心。所以只要还有子判据悬着，
 * 就不允许它以「无问题」示人，降级成 unverified 单列一档。
 * 已经查出缺陷的维度（0 / 0.5）保留原档位：缺陷是确定的，未核实的部分另行标注。
 */
export function tierOfDimension(dimension) {
  if (!dimension || dimension.applicable === false) return TIER.na;
  const unresolved = (dimension.unresolved_subcriteria || []).length;
  const score = dimension.score;
  if (score === 0) return TIER.bad;
  if (score === 0.5) return TIER.flaw;
  if (unresolved) return TIER.unverified;
  return TIER.ok;
}

/** 有几条子判据是「没核实」——每个维度卡都要单独报这个数。 */
export function unresolvedCount(dimension) {
  return ((dimension && dimension.unresolved_subcriteria) || []).length;
}
