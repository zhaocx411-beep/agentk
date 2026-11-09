// 当前项目上下文（projectId / sessionId）。App 启动 ensureProject() 成功后
// 写入，AgentPanel / MediaLibrary / Timeline 等所有需要调后端的组件从这里读，
// 不再依赖 props 层层下传。
import { create } from "zustand";

import { api } from "@/remote";
import type { ProjectRef } from "@/remote";

import { resetAgentSync } from "./agentSync";
import { useHistoryStore } from "./historyStore";
import { usePlaybackStore } from "./playbackStore";
import { usePreviewOverridesStore } from "./previewOverridesStore";
import { useRenderStore } from "./renderStore";
import {
  flushSequenceViewSave,
  resetSequenceViewSave,
  useSequenceStore,
} from "./sequenceStore";
import { resetSubmitQueue, setSubmitQueueProject } from "./submitOps";
import { useTimelineStore } from "./timelineStore";
import { toast } from "./toastStore";
import { refreshTranscriptView, resetTranscriptStore } from "./transcriptStore";

let switchGeneration = 0;
let switchController: AbortController | null = null;

export interface ProjectState {
  project: ProjectRef | null;
  projects: ProjectRef[];
  loading: boolean;
  /** 启动引导失败时的错误信息（如后端未启动），供 UI 显示。 */
  bootstrapError: string | null;
  setProject(project: ProjectRef): void;
  loadProjects(): Promise<void>;
  switchProject(projectId: string): Promise<void>;
  createProject(title?: string): Promise<void>;
  renameCurrent(title: string): Promise<void>;
  setBootstrapError(message: string): void;
}

export const useProjectStore = create<ProjectState>((set, get) => ({
  project: null,
  projects: [],
  loading: false,
  bootstrapError: null,
  setProject(project) {
    setSubmitQueueProject(project.projectId, useSequenceStore.getState().activeTimelineId);
    resetTranscriptStore(project.projectId);
    set((s) => ({
      project,
      projects: s.projects.some((candidate) => candidate.projectId === project.projectId)
        ? s.projects.map((candidate) =>
            candidate.projectId === project.projectId ? project : candidate,
          )
        : [...s.projects, project],
      bootstrapError: null,
    }));
  },
  async loadProjects() {
    set({ loading: true, bootstrapError: null });
    try {
      let projects = await api.listProjects();
      if (projects.length === 0) {
        projects = [await api.createProject({ title: "我的第一个项目" })];
      }
      projects.sort((a, b) => b.createdAtUnixMs - a.createdAtUnixMs);
      set({ projects });
      const current = get().project;
      const target =
        (current && projects.find((item) => item.projectId === current.projectId)) ?? projects[0];
      await get().switchProject(target.projectId);
    } catch (err) {
      set({ bootstrapError: err instanceof Error ? err.message : String(err) });
    } finally {
      set({ loading: false });
    }
  },
  async switchProject(projectId) {
    const target = get().projects.find((item) => item.projectId === projectId);
    if (!target || target.projectId === get().project?.projectId) return;
    try {
      await flushSequenceViewSave();
    } catch (error) {
      toast.error(`保存当前时间线视图失败：${error instanceof Error ? error.message : String(error)}`);
      return;
    }
    resetSequenceViewSave();
    const generation = ++switchGeneration;
    switchController?.abort();
    const controller = new AbortController();
    switchController = controller;
    const previousProject = get().project;
    const previousTimeline = useTimelineStore.getState();
    const previousDoc = previousTimeline.doc;
    const previousRevision = previousTimeline.confirmedRevision;
    // 项目级导出状态不能跨项目复用；同时使旧 POST/轮询回调的 generation 失效。
    useRenderStore.getState().reset();
    resetSubmitQueue();
    setSubmitQueueProject(null);
    useTimelineStore.getState().resetTimeline();
    useHistoryStore.getState().clear();
    usePreviewOverridesStore.getState().reset();
    resetAgentSync();
    resetTranscriptStore();
    usePlaybackStore.getState().pause();
    usePlaybackStore.getState().setCurrentFrame(0);
    usePlaybackStore.getState().setSourcePreview(null);
    set({ loading: true, bootstrapError: null });
    try {
      const snapshot = await api.getTimeline(projectId, { signal: controller.signal });
      if (generation !== switchGeneration || controller.signal.aborted) return;
      useTimelineStore.getState().setConfirmed(snapshot.doc, snapshot.revision);
      useSequenceStore.getState().hydrate(target.projectId, snapshot);
      const sequence = useSequenceStore.getState();
      usePlaybackStore.getState().setCurrentFrame(
        sequence.timelines.find((timeline) => timeline.timelineId === sequence.activeTimelineId)
          ?.currentFrame ?? 0,
      );
      setSubmitQueueProject(target.projectId, sequence.activeTimelineId);
      set({ project: target });
      resetTranscriptStore(target.projectId);
      void refreshTranscriptView(target.projectId, snapshot.revision);
    } catch (err) {
      if (
        generation !== switchGeneration ||
        controller.signal.aborted ||
        (err instanceof Error && err.name === "AbortError")
      ) {
        return;
      }
      const message = err instanceof Error ? err.message : String(err);
      if (previousDoc) {
        useTimelineStore.getState().setConfirmed(previousDoc, previousRevision);
      }
      setSubmitQueueProject(
        previousProject?.projectId ?? null,
        useSequenceStore.getState().activeTimelineId,
      );
      resetAgentSync(previousProject?.projectId ?? null);
      resetTranscriptStore(previousProject?.projectId ?? null);
      if (previousProject && previousDoc) {
        void refreshTranscriptView(previousProject.projectId, previousRevision);
      }
      set({ bootstrapError: message });
      toast.error(`切换项目失败：${message}`);
    } finally {
      if (switchController === controller) switchController = null;
      if (generation === switchGeneration) set({ loading: false });
    }
  },
  async createProject(title = "未命名项目") {
    set({ loading: true });
    try {
      const created = await api.createProject({ title });
      set((s) => ({ projects: [created, ...s.projects] }));
      await get().switchProject(created.projectId);
    } catch (err) {
      toast.error(`创建项目失败：${err instanceof Error ? err.message : String(err)}`);
    } finally {
      set({ loading: false });
    }
  },
  async renameCurrent(title) {
    const current = get().project;
    if (!current) return;
    if (useTimelineStore.getState().pendingOps.length > 0) {
      toast.warning("等待时间线修改同步完成后再重命名项目");
      return;
    }
    const trimmed = title.trim();
    if (!trimmed || trimmed === current.title) return;
    set({ loading: true });
    try {
      const renamed = await api.renameProject(current.projectId, {
        title: trimmed,
        requestId: crypto.randomUUID(),
      });
      const snapshot = await api.getTimeline(current.projectId);
      useTimelineStore.getState().setConfirmed(snapshot.doc, snapshot.revision);
      useSequenceStore.getState().hydrate(current.projectId, snapshot);
      set((s) => ({
        project: renamed,
        projects: s.projects.map((item) =>
          item.projectId === renamed.projectId ? renamed : item,
        ),
      }));
      useHistoryStore.getState().clear();
      toast.success("项目已重命名");
    } catch (err) {
      toast.error(`重命名失败：${err instanceof Error ? err.message : String(err)}`);
    } finally {
      set({ loading: false });
    }
  },
  setBootstrapError(message) {
    set({ bootstrapError: message });
  },
}));
