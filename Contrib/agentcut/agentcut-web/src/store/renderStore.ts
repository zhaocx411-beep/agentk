// 导出状态（设计文档第 10 节 / 阶段 D）：POST /renders 发起，
// 短轮询 GET /renders/{renderKey}（2.5s 一次）直到 completed/failed。
// 故意不是 React Hook：导出请求本身不属于任何一个组件的生命周期
// （设计 8.3 节——用户可以边导出边继续和 Agent 聊天，切换 Tab/组件卸载
// 都不应该打断轮询），用模块级 store + 模块级 timer 实现。

import { create } from "zustand";

import { ApiError, api } from "@/remote";
import type { RenderProgress } from "@/remote/client";

export type RenderPhase =
  | "idle"
  | "starting"
  | "running"
  | "completed"
  | "failed";

export interface RenderClientState {
  phase: RenderPhase;
  projectId: string | null;
  renderKey: string | null;
  revision: number | null;
  progress: RenderProgress | null;
  outputUri: string | null;
  warnings: string[];
  workflowStatus: string | null;
  serverError: string | null;
  errorMessage: string | null;
  /** 发起一次导出（幂等：同一份文档重复点击，后端按 renderKey 去重）。 */
  start(projectId: string): void;
  /** 清空状态，回到可以再次点击"导出"的 idle 态（不中止已发出的后端渲染）。 */
  reset(): void;
}

const POLL_INTERVAL_MS = 2500;

let pollTimer: ReturnType<typeof setTimeout> | null = null;
// 每次 start() 自增；异步回调里核对代际，避免旧一轮 start 的迟到响应
// 覆盖新一轮的状态（例如用户很快点了两次"重试"）。
let generation = 0;

function stopPolling(): void {
  if (pollTimer !== null) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
}

function errorMessageOf(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return err.body.error || fallback;
  if (err instanceof Error) return err.message;
  return fallback;
}

function failRender(
  errorMessage: string,
  extras: Partial<
    Pick<RenderClientState, "workflowStatus" | "serverError" | "warnings">
  > = {},
): void {
  stopPolling();
  useRenderStore.setState({
    phase: "failed",
    errorMessage,
    ...extras,
  });
}

function schedulePoll(projectId: string, renderKey: string, gen: number): void {
  pollTimer = setTimeout(async () => {
    pollTimer = null;
    try {
      const status = await api.getRenderStatus(projectId, renderKey);
      if (gen !== generation || useRenderStore.getState().projectId !== projectId) return;

      const warnings = status.warnings ?? status.progress?.warnings ?? [];
      if (status.status === "running") {
        useRenderStore.setState({
          phase: "running",
          progress: status.progress ?? null,
          revision: status.revision ?? useRenderStore.getState().revision,
          warnings,
          workflowStatus: status.workflowStatus ?? null,
          serverError: status.error ?? null,
          errorMessage: null,
        });
        // 串行轮询：只有当前请求结束后才安排下一次，慢请求不会堆积。
        schedulePoll(projectId, renderKey, gen);
        return;
      }

      if (status.status === "completed") {
        if (!status.outputUri) {
          failRender("导出已完成，但后端未返回成片地址", {
            warnings,
            workflowStatus: status.workflowStatus ?? null,
            serverError: status.error ?? null,
          });
          return;
        }
        stopPolling();
        useRenderStore.setState({
          phase: "completed",
          progress: status.progress ?? null,
          outputUri: status.outputUri,
          revision: status.revision ?? useRenderStore.getState().revision,
          warnings,
          workflowStatus: status.workflowStatus ?? null,
          serverError: status.error ?? null,
          errorMessage: null,
        });
        return;
      }

      const serverError = status.error?.trim() || null;
      failRender(serverError ?? "渲染失败，请重试", {
        warnings,
        workflowStatus: status.workflowStatus ?? null,
        serverError,
      });
    } catch (err) {
      if (gen !== generation || useRenderStore.getState().projectId !== projectId) return;
      const message = errorMessageOf(err, "查询导出状态失败");
      failRender(message, {
        serverError: err instanceof ApiError ? err.body.error || null : null,
      });
    }
  }, POLL_INTERVAL_MS);
}

export const useRenderStore = create<RenderClientState>(() => ({
  phase: "idle",
  projectId: null,
  renderKey: null,
  revision: null,
  progress: null,
  outputUri: null,
  warnings: [],
  workflowStatus: null,
  serverError: null,
  errorMessage: null,

  start(projectId) {
    stopPolling();
    const gen = ++generation;
    useRenderStore.setState({
      phase: "starting",
      projectId,
      renderKey: null,
      revision: null,
      progress: null,
      outputUri: null,
      warnings: [],
      workflowStatus: null,
      serverError: null,
      errorMessage: null,
    });
    void api.startRender(projectId).then(
      (res) => {
        if (gen !== generation || useRenderStore.getState().projectId !== projectId) return;
        useRenderStore.setState({
          phase: "running",
          renderKey: res.renderKey,
          revision: res.revision,
        });
        schedulePoll(projectId, res.renderKey, gen);
      },
      (err: unknown) => {
        if (gen !== generation || useRenderStore.getState().projectId !== projectId) return;
        const message = errorMessageOf(err, "发起导出失败");
        failRender(message, {
          serverError: err instanceof ApiError ? err.body.error || null : null,
        });
      },
    );
  },

  reset() {
    stopPolling();
    generation++;
    useRenderStore.setState({
      phase: "idle",
      projectId: null,
      renderKey: null,
      revision: null,
      progress: null,
      outputUri: null,
      warnings: [],
      workflowStatus: null,
      serverError: null,
      errorMessage: null,
    });
  },
}));
