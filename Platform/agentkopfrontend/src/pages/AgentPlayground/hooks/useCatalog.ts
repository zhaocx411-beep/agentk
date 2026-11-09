/**
 * 能力目录 + 模型目录加载 & 跨页面配置消费
 *
 * 维护：
 * - selectedModel / selectedToolModels：来自 GET /agentk/models
 * - selectedTools：侧边栏「新会话设置」的工具勾选
 * - systemPrompt：当前生效的 system prompt
 * - overrideYaml：会话级 Profile 文档原文，随 createSession 的 profile 字段发送
 */
import { useState, useEffect, useRef, useCallback } from 'react';
import { message } from '@/utils/feedback';
import { useAgentConfigStore } from '@/store/agentConfig';
import { pickDefaultChatModel } from '@/utils/models';
import { buildProfileYaml, patchProfileYamlTools } from '@/utils/profileYaml';
import { getCapabilities, getModels } from '../api';
import type { CapabilityCatalog, ModelCatalog } from '../types';

export function useCatalog() {
  const consumePendingConfig = useAgentConfigStore((s) => s.consumePendingConfig);
  const [catalog, setCatalog] = useState<CapabilityCatalog | null>(null);
  const [models, setModels] = useState<ModelCatalog | null>(null);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedToolModels, setSelectedToolModels] = useState<Record<string, string>>({});
  const [systemPrompt, setSystemPrompt] = useState('');
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  // 会话级 Profile 覆盖文档：消费 Pipeline 配置时写入，也可在 OverrideModal 中编辑
  const [overrideYaml, setOverrideYaml] = useState<string>('');

  // 标记是否消费了 Pipeline 配置，避免目录拉取完成后覆盖掉 Pipeline 带来的勾选
  const hasConsumedWorkflowConfigRef = useRef(false);

  const hasFetchedRef = useRef(false);
  useEffect(() => {
    if (hasFetchedRef.current) return;
    hasFetchedRef.current = true;

    Promise.all([getCapabilities(), getModels()])
      .then(([nextCatalog, nextModels]) => {
        setCatalog(nextCatalog);
        setModels(nextModels);
        setSelectedModel((prev) => pickDefaultChatModel(nextModels, prev));
        // 目录即本实例当前可用的工具集，未从 Pipeline 编辑器带配置进来时默认全部启用
        if (!hasConsumedWorkflowConfigRef.current) {
          setSelectedTools(nextCatalog.toolNames);
        }
      })
      .catch(() => {
        // 静默失败，侧边栏显示空态
      });
  }, []);

  // 消费跨页面传来的配置（Pipeline 编辑器 → 调试台）
  const hasConsumedRef = useRef(false);
  useEffect(() => {
    if (hasConsumedRef.current) return;
    hasConsumedRef.current = true;

    const config = consumePendingConfig();
    if (config && config.source === 'workflow-editor') {
      hasConsumedWorkflowConfigRef.current = true;
      // Pipeline 传来的 tools 有勾选时才回显；空数组表示用户未勾选任何工具
      setSelectedTools(config.tools);
      if (config.model) setSelectedModel(config.model);
      // 优先用 Pipeline 传来的 profileYaml；没传则按 systemPrompt + tools 现场构建
      if (config.profileYaml) {
        setOverrideYaml(config.profileYaml);
      } else if (config.systemPrompt || config.tools.length > 0) {
        setOverrideYaml(
          buildProfileYaml({ systemPrompt: config.systemPrompt, tools: config.tools }),
        );
      }
      if (config.systemPrompt) setSystemPrompt(config.systemPrompt);
      message.info('已从 Pipeline 编辑器加载配置');
    }
    // 仅在 mount 时消费一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 勾选变化时把 overrideYaml 的 tools 同步为当前选择，system / pipelines 原样保留
  // 跳过首次运行：目录加载前的初始态无意义
  const isInitialSyncRef = useRef(true);
  useEffect(() => {
    if (isInitialSyncRef.current) {
      isInitialSyncRef.current = false;
      return;
    }
    setOverrideYaml((prev) =>
      prev.trim() ? patchProfileYamlTools(prev, selectedTools, catalog) : prev,
    );
  }, [selectedTools, catalog]);

  /** 将 YAML 中每行的前导空格转为 Tab（2空格=1Tab） */
  const spacesToTabs = useCallback((text: string): string => {
    return text.replace(/^( +)/gm, (_, spaces: string) => {
      const tabCount = Math.floor(spaces.length / 2);
      const remainder = spaces.length % 2;
      return '\t'.repeat(tabCount) + ' '.repeat(remainder);
    });
  }, []);

  return {
    catalog,
    models,
    selectedModel,
    setSelectedModel,
    selectedToolModels,
    setSelectedToolModels,
    systemPrompt,
    setSystemPrompt,
    selectedTools,
    setSelectedTools,
    overrideYaml,
    setOverrideYaml,
    spacesToTabs,
  };
}
