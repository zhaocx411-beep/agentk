/* 分栏拖拽：用鼠标拖动栏与栏之间的那条线来改栏的大小。
 *
 * 管三条线 —— 侧栏|中栏、中栏|右栏（都是竖线、改宽度），
 * 以及右栏内部 判定区|问题定位（横线、改高度）。
 *
 * 四个必须遵守的约束，改动前先读：
 *
 * 1. **尺寸只写在 CSS 变量上**，不直接写元素的 style.width / style.height。
 *    收起功能（.shell.nav-collapsed）靠覆盖 flex-basis 生效，如果这里把尺寸
 *    写成行内样式，收起就再也压不下去了 —— 行内样式赢过任何类选择器。
 *
 * 2. **拖动过程中必须持续通知外部重新量视频**。视频上的 grounding 框是相对
 *    播放器内容矩形的百分比，任何一条线动了，框的绝对位置就全偏。只在松手时
 *    通知会让框在拖动的整个过程里错位，看起来像 bug。
 *
 * 3. **上限是算出来的，不是常数**。它取决于容器还剩多少空间，并且要给对侧
 *    留够保底（RESULT_MIN / VPANE_MIN）。写死一个 max 会让小窗口下对侧被挤没。
 *
 * 4. **判定区默认没有高度**，它按内容长高、封顶 54vh（见 studio.css 的 .vd）。
 *    只有用户真的拖过之后才切成显式高度 —— 一上来就钉死高度的话，短报告下面
 *    会挂一大块空白。所以它的"复位"是回到自动高度，而不是回到某个数字。
 */

/* 每条线管一个尺寸。
 *   axis  —— "x" 改宽度（拖左右），"y" 改高度（拖上下）
 *   prop  —— 它写的 CSS 变量
 *   def   —— 双击复位的目标；null 表示"复位 = 回到自动尺寸"
 *   max   —— 硬上限；null 表示只受"给对侧留够保底"的限制 */
const PANES = {
  nav: { axis: "x", prop: "--sidebar-w", def: 262, min: 200, max: 460 },
  input: { axis: "x", prop: "--control-w", def: 420, min: 330, max: 820 },
  verdict: { axis: "y", prop: "--vd-h", def: null, min: 140, max: null },
};

/* 对侧的保底尺寸。低于这个数，那一侧的内容就排不下了：
 * 右栏要放得下视频 + 标签面板并排，问题定位要放得下播放器本身。 */
const RESULT_MIN = 360;
const VPANE_MIN = 260;

/* 键盘每按一次挪多少。16px 是一次能看出来又不会跳过头的步长。 */
const KEY_STEP = 16;

const STORE_KEY = (which) => `studio-${which}-size`;

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

/**
 * @param {{ onResize?: () => void }} options
 *   onResize 在拖动过程中每帧调用一次，以及键盘/复位后调用一次。
 */
export function createPaneResizer({ onResize } = {}) {
  const win = document.querySelector(".win");
  const shell = document.querySelector(".shell");
  if (!win || !shell) return { wire() {} };

  const resultCol = shell.querySelector(".result-col");
  const resultHead = shell.querySelector(".result-head");
  const verdictPane = shell.querySelector(".vd");

  const notify = typeof onResize === "function" ? onResize : () => {};

  const handleOf = (which) => shell.querySelector(`.splitter[data-resize="${which}"]`);

  /** 这条线还能拉到多大：容器的余量减去对侧保底。 */
  function maxFor(which) {
    const spec = PANES[which];
    let room = spec.max ?? Infinity;

    if (which === "input") {
      // 中栏的余量 = 右栏宽度 - 侧栏 - 两条竖线 - 右栏保底
      const sidebar = shell.classList.contains("nav-collapsed") ? 0 : read("nav");
      room = shell.clientWidth - sidebar - 2 - RESULT_MIN;
    } else if (which === "verdict") {
      // 判定区的余量 = 右栏可用高度 - 顶部标题条 - 横线 - 问题定位保底
      if (!resultCol) return spec.min;
      const head = resultHead ? resultHead.offsetHeight : 0;
      room = resultCol.clientHeight - head - 1 - VPANE_MIN;
    }

    return clamp(Math.floor(Math.min(room, spec.max ?? Infinity)), spec.min, Infinity);
  }

  /** 当前尺寸。优先读已写入的变量，其次读存档；都没有就现场量。 */
  function read(which) {
    const spec = PANES[which];
    const raw = win.style.getPropertyValue(spec.prop) || localStorage.getItem(STORE_KEY(which));
    const parsed = parseInt(String(raw), 10);
    if (Number.isFinite(parsed)) return parsed;
    // def 为 null 的（判定区）没有默认数字，只能量当前渲染出来的高度 ——
    // 这也正是"第一次拖动"的起点：从它现在长成的样子接着拖。
    if (spec.def !== null) return spec.def;
    return verdictPane ? Math.round(verdictPane.getBoundingClientRect().height) : spec.min;
  }

  function write(which, value, { persist = true, cascade = true } = {}) {
    const spec = PANES[which];
    const next = clamp(Math.round(value), spec.min, maxFor(which));
    win.style.setProperty(spec.prop, `${next}px`);
    if (persist) localStorage.setItem(STORE_KEY(which), String(next));

    // 判定区要额外挂一个标记：CSS 只在有标记时才让 --vd-h 生效，
    // 没标记就走原来的"按内容长高、封顶 54vh"。
    if (which === "verdict" && resultCol) resultCol.dataset.vdSized = "1";

    const handle = handleOf(which);
    if (handle) handle.setAttribute("aria-valuenow", String(next));

    // 侧栏变宽会吃掉中栏的可用余量，中栏得跟着收，否则右栏被挤到保底以下。
    // cascade:false 断掉回环 —— 中栏收窄不需要反过来动侧栏。
    if (cascade && which === "nav") write("input", read("input"), { persist, cascade: false });
    return next;
  }

  /** 复位。有 def 的回到 def，没有的（判定区）交还给自动高度。 */
  function reset(which) {
    const spec = PANES[which];
    if (spec.def !== null) {
      write(which, spec.def);
      return;
    }
    win.style.removeProperty(spec.prop);
    localStorage.removeItem(STORE_KEY(which));
    if (resultCol) delete resultCol.dataset.vdSized;
    const handle = handleOf(which);
    if (handle) handle.removeAttribute("aria-valuenow");
  }

  function drag(handle, which, startEvent) {
    const axis = PANES[which].axis;
    const startPos = axis === "x" ? startEvent.clientX : startEvent.clientY;
    const startSize = read(which);
    let frame = 0;

    // 捕获指针，这样拖出这条线之外（甚至拖到窗口外）也不丢事件。
    // 会抛的场合是真实存在的（指针已经不活跃），但抛了不该让整个拖动失效 ——
    // 没有捕获时事件仍会冒到 handle 上，只是拖太快可能脱靶。
    try {
      handle.setPointerCapture(startEvent.pointerId);
    } catch {
      /* 没拿到捕获也继续，降级成普通拖动 */
    }
    handle.dataset.dragging = "1";
    document.body.classList.add(axis === "x" ? "resizing-x" : "resizing-y");

    const onMove = (event) => {
      const pos = axis === "x" ? event.clientX : event.clientY;
      write(which, startSize + (pos - startPos), { persist: false });
      // 每帧最多量一次：pointermove 的触发频率远高于屏幕刷新，
      // 逐条事件都去量视频尺寸会让拖动明显发涩。
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        notify();
      });
    };

    const onUp = () => {
      handle.removeEventListener("pointermove", onMove);
      handle.removeEventListener("pointerup", onUp);
      handle.removeEventListener("pointercancel", onUp);
      if (frame) window.cancelAnimationFrame(frame);
      delete handle.dataset.dragging;
      document.body.classList.remove("resizing-x", "resizing-y");
      // 落盘用最终值；拖动期间刻意不写 localStorage，避免几百次同步写。
      write(which, read(which));
      notify();
    };

    handle.addEventListener("pointermove", onMove);
    handle.addEventListener("pointerup", onUp);
    handle.addEventListener("pointercancel", onUp);
  }

  function wire() {
    Object.keys(PANES).forEach((which) => {
      const spec = PANES[which];
      // 存档值也要过一遍 clamp：窗口可能比上次存档时小了很多。
      // 没有存档的判定区不要碰 —— 它得留在自动高度。
      if (spec.def !== null || localStorage.getItem(STORE_KEY(which))) {
        write(which, read(which));
      }

      const handle = handleOf(which);
      if (!handle) return;

      handle.addEventListener("pointerdown", (event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        drag(handle, which, event);
      });

      // 双击复位，是 macOS 里所有可拖动分隔线的通用约定
      handle.addEventListener("dblclick", () => {
        reset(which);
        notify();
      });

      // 键盘可达：分隔线是 role="separator"，方向键即调尺寸
      handle.addEventListener("keydown", (event) => {
        const keys = spec.axis === "x"
          ? { ArrowLeft: -KEY_STEP, ArrowRight: KEY_STEP }
          : { ArrowUp: -KEY_STEP, ArrowDown: KEY_STEP };
        const delta = keys[event.key];
        if (delta === undefined && event.key !== "Home") return;
        event.preventDefault();
        if (event.key === "Home") reset(which);
        else write(which, read(which) + delta);
        notify();
      });
    });

    // 窗口变小后，原先合法的尺寸可能已经把对侧挤到保底以下，重新收一遍。
    // 判定区仍在自动高度时别动它，那样会把它意外钉死。
    window.addEventListener("resize", () => {
      write("input", read("input"));
      if (resultCol && resultCol.dataset.vdSized === "1") write("verdict", read("verdict"));
    });
  }

  return { wire };
}
