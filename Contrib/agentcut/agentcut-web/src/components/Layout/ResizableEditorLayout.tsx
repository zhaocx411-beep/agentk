import { useEffect, useRef, useState } from "react";

import { AgentPanel } from "@/components/AgentPanel/AgentPanel";
import { MediaLibrary } from "@/components/MediaLibrary/MediaLibrary";
import { PreviewPanel } from "@/components/Preview/PreviewPanel";
import { TimelinePanel } from "@/components/Timeline/TimelinePanel";

const STORAGE_KEY = "agentcut.editor-layout.v2";
const DEFAULT_LAYOUT = { agent: 32, media: 35, timeline: 38 };
const MIN_AGENT = 18;
const MAX_AGENT = 60;
const MIN_MEDIA = 20;
const MIN_PREVIEW = 35;
const MIN_TIMELINE = 22;
const MIN_UPPER = 26;

interface EditorLayout {
  agent: number;
  media: number;
  timeline: number;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function loadLayout(): EditorLayout {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "") as Partial<EditorLayout>;
    const agent = clamp(Number(parsed.agent), MIN_AGENT, MAX_AGENT);
    const media = clamp(Number(parsed.media), MIN_MEDIA, 100 - MIN_PREVIEW);
    const timeline = clamp(Number(parsed.timeline), MIN_TIMELINE, 100 - MIN_UPPER);
    if ([agent, media, timeline].every(Number.isFinite)) return { agent, media, timeline };
  } catch {
    // 首次打开或旧值损坏时使用稳定默认布局。
  }
  return DEFAULT_LAYOUT;
}

function ResizeHandle({
  orientation,
  label,
  onPointerDown,
  onKeyDown,
  onReset,
}: {
  orientation: "horizontal" | "vertical";
  label: string;
  onPointerDown(event: React.PointerEvent<HTMLDivElement>): void;
  onKeyDown(event: React.KeyboardEvent<HTMLDivElement>): void;
  onReset(): void;
}) {
  return (
    <div
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-orientation={orientation}
      title="拖动调整面板大小；双击恢复默认布局"
      className={`group relative z-30 bg-[#F2F2F0] outline-none ${
        orientation === "vertical" ? "cursor-row-resize" : "cursor-col-resize"
      }`}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDoubleClick={onReset}
    >
      <span
        className={`pointer-events-none absolute bg-[#D8D6D2] ${
          orientation === "vertical"
            ? "left-0 top-1/2 h-px w-full -translate-y-1/2"
            : "left-1/2 top-0 h-full w-px -translate-x-1/2"
        }`}
      />
    </div>
  );
}

export function ResizableEditorLayout() {
  const [layout, setLayout] = useState<EditorLayout>(loadLayout);
  const outerRef = useRef<HTMLDivElement>(null);
  const upperRef = useRef<HTMLDivElement>(null);
  const rightWorkspaceRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layout));
  }, [layout]);

  function startDrag(
    event: React.PointerEvent<HTMLDivElement>,
    move: (clientX: number, clientY: number) => void,
  ) {
    event.preventDefault();
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.userSelect = "none";
    const onMove = (pointer: PointerEvent) => move(pointer.clientX, pointer.clientY);
    const finish = () => {
      document.body.style.userSelect = previousUserSelect;
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", finish);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", finish, { once: true });
    window.addEventListener("pointercancel", finish, { once: true });
  }

  function moveFirstColumn(clientX: number) {
    const rect = outerRef.current?.getBoundingClientRect();
    if (!rect?.width) return;
    const agent = clamp(
      ((clientX - rect.left) / rect.width) * 100,
      MIN_AGENT,
      MAX_AGENT,
    );
    setLayout((current) => ({ ...current, agent }));
  }

  function moveSecondColumn(clientX: number) {
    const rect = upperRef.current?.getBoundingClientRect();
    if (!rect?.width) return;
    const media = clamp(
      ((clientX - rect.left) / rect.width) * 100,
      MIN_MEDIA,
      100 - MIN_PREVIEW,
    );
    setLayout((current) => ({ ...current, media }));
  }

  function moveTimeline(clientY: number) {
    const rect = rightWorkspaceRef.current?.getBoundingClientRect();
    if (!rect?.height) return;
    const timeline = clamp(
      ((rect.bottom - clientY) / rect.height) * 100,
      MIN_TIMELINE,
      100 - MIN_UPPER,
    );
    setLayout((current) => ({ ...current, timeline }));
  }

  const right = 100 - layout.agent;
  const preview = 100 - layout.media;

  return (
    <main
      ref={outerRef}
      className="grid min-h-0 flex-1 overflow-hidden bg-[#F2F2F0]"
      style={{
        gridTemplateColumns: `${layout.agent}fr 5px ${right}fr`,
      }}
    >
      <section className="min-h-0 min-w-0 overflow-hidden bg-[#F2F2F0]">
        <AgentPanel />
      </section>
      <ResizeHandle
        orientation="horizontal"
        label="调整 AI 面板和剪辑工作区宽度"
        onPointerDown={(event) => startDrag(event, (x) => moveFirstColumn(x))}
        onKeyDown={(event) => {
          if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
          event.preventDefault();
          setLayout((current) => ({
            ...current,
            agent: clamp(
              current.agent + (event.key === "ArrowLeft" ? -2 : 2),
              MIN_AGENT,
              MAX_AGENT,
            ),
          }));
        }}
        onReset={() => setLayout(DEFAULT_LAYOUT)}
      />

      <div
        ref={rightWorkspaceRef}
        className="grid min-h-0 min-w-0 overflow-hidden"
        style={{
          gridTemplateRows: `${100 - layout.timeline}fr 5px ${layout.timeline}fr`,
        }}
      >
        <div
          ref={upperRef}
          className="grid min-h-0 min-w-0 overflow-hidden"
          style={{
            gridTemplateColumns: `${layout.media}fr 5px ${preview}fr`,
          }}
        >
          <section className="min-h-0 min-w-0 overflow-hidden bg-[#F2F2F0]">
            <MediaLibrary />
          </section>
          <ResizeHandle
            orientation="horizontal"
            label="调整素材面板和预览面板宽度"
            onPointerDown={(event) => startDrag(event, (x) => moveSecondColumn(x))}
            onKeyDown={(event) => {
              if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
              event.preventDefault();
              setLayout((current) => ({
                ...current,
                media: clamp(
                  current.media + (event.key === "ArrowLeft" ? -2 : 2),
                  MIN_MEDIA,
                  100 - MIN_PREVIEW,
                ),
              }));
            }}
            onReset={() => setLayout(DEFAULT_LAYOUT)}
          />
          <section className="min-h-0 min-w-0 overflow-hidden bg-[#F2F2F0]">
            <PreviewPanel />
          </section>
        </div>

        <ResizeHandle
          orientation="vertical"
          label="调整预览区和时间线高度"
          onPointerDown={(event) => startDrag(event, (_x, y) => moveTimeline(y))}
          onKeyDown={(event) => {
            if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
            event.preventDefault();
            setLayout((current) => ({
              ...current,
              timeline: clamp(
                current.timeline + (event.key === "ArrowUp" ? 2 : -2),
                MIN_TIMELINE,
                100 - MIN_UPPER,
              ),
            }));
          }}
          onReset={() => setLayout(DEFAULT_LAYOUT)}
        />

        <section className="min-h-0 overflow-hidden bg-[#EDECEA]">
          <TimelinePanel />
        </section>
      </div>
    </main>
  );
}
