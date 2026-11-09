// 默认使用真实 cut-apiserver；只有测试/离线演示显式设置
// VITE_USE_MOCK_API=1 时才启用内存 mock，避免验收误跑 mock。
import { HttpCutApiClient } from "./http";
import { MockCutApiClient } from "./mock";
import type { CutApiClient } from "./client";

const useMockApi = import.meta.env.VITE_USE_MOCK_API === "1";
const devUser = import.meta.env.VITE_AGENTCUT_USER?.trim() || "dev";

export const api: CutApiClient = useMockApi
  ? new MockCutApiClient()
  : new HttpCutApiClient({ user: devUser });

export * from "./client";
