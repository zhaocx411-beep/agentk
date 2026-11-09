/**
 * 会话配置弹窗
 * 首次创建会话（或点「开始新会话」/配置 chip）前弹出，确认本段对话使用的配置：
 *  - 自由配置：现场选择模型 / 工具 / override
 *  - 已有 Agent：选择 Agent + 版本，实时预览配置总览
 */
import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Empty, Modal, Segmented, Select, Spin } from 'antd';
import { message } from '@/utils/feedback';
import {
  AGENT_LOOPS_QUERY_KEY,
  agentLoopVersionsKey,
  getAgentLoops,
  getAgentLoopVersions,
} from '@/services/agentLoop';
import type { AgentLoopVersion } from '@/types/agentLoop';
import type { CapabilityCatalog, ModelCatalog } from '../../types';
import { SESSION_CONFIG_MODE_OPTIONS } from '../../constants';
import FreeConfigPanel from '../inspector/FreeConfigPanel';
import VersionConfigOverview from '../version/VersionConfigOverview';
import styles from '../../AgentPlayground.module.less';

/** Agent 配置来源的元信息（版本管理「加载调试」/ 新建 Agent 回调等场景使用） */
export interface AgentConfigMeta {
  agentId: number;
  agentName: string;
  versionId: number;
  version: number;
  status: string;
}

/** 当前生效的会话配置来源（sidebar 卡片与输入框 chip 展示用） */
export type SessionConfigSource = { type: 'free' } | ({ type: 'agent' } & AgentConfigMeta);

/** 弹窗确认时的选择 */
export type SessionConfigChoice =
  | { type: 'free' }
  | { type: 'agent'; agentId: number; agentName: string; version: AgentLoopVersion };

interface SessionConfigModalProps {
  open: boolean;
  /** 当前生效配置：打开弹窗时据此回显选中态 */
  configSource: SessionConfigSource;
  onClose: () => void;
  /** 确认选择（父级负责创建会话），返回是否成功 */
  onConfirm: (choice: SessionConfigChoice) => Promise<boolean>;
  catalog: CapabilityCatalog | null;
  models: ModelCatalog | null;
  selectedModel: string;
  setSelectedModel: (m: string) => void;
  selectedTools: string[];
  setSelectedTools: (fn: (prev: string[]) => string[]) => void;
  selectedToolModels: Record<string, string>;
  setSelectedToolModels: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  onOpenOverride: () => void;
}

function SessionConfigModal({
  open,
  configSource,
  onClose,
  onConfirm,
  catalog,
  models,
  selectedModel,
  setSelectedModel,
  selectedTools,
  setSelectedTools,
  selectedToolModels,
  setSelectedToolModels,
  onOpenOverride,
}: SessionConfigModalProps) {
  const [mode, setMode] = useState<'free' | 'agent'>('free');
  const [agentId, setAgentId] = useState<number | null>(null);
  const [versionId, setVersionId] = useState<number | null>(null);
  const [confirming, setConfirming] = useState(false);

  const { data: loops = [] } = useQuery({
    queryKey: AGENT_LOOPS_QUERY_KEY,
    queryFn: getAgentLoops,
    enabled: open,
  });

  const { data: versions = [], isLoading: versionsLoading } = useQuery({
    queryKey: agentLoopVersionsKey(agentId ?? -1),
    queryFn: () => getAgentLoopVersions(agentId as number),
    enabled: open && agentId !== null,
  });

  // 打开时按当前生效配置回显：agent 模式选中对应 Agent 与版本；free 模式保留上次选择
  // 依赖 configSource：加载调试会先写 configSource 再开弹窗，必须读到最新来源，
  // 不能只靠 open，否则会和「默认选第一个 Agent」抢状态
  useEffect(() => {
    if (!open) return;
    setMode(configSource.type);
    if (configSource.type === 'agent') {
      setAgentId(configSource.agentId);
      setVersionId(configSource.versionId);
    }
  }, [open, configSource]);

  // 仅自由配置、且尚未选中时默认第一个；加载调试带来的 agent 来源不要抢写
  useEffect(() => {
    if (!open || configSource.type === 'agent' || agentId !== null || loops.length === 0) return;
    setAgentId(loops[0].id);
  }, [open, configSource, agentId, loops]);

  // 草稿不允许调试，下拉只展示非草稿版本（与版本管理抽屉的「加载调试」可见性一致）
  const publishedVersions = useMemo(() => versions.filter((v) => v.status !== 'draft'), [versions]);

  // 版本列表加载后默认选中最新已发布版本；加载调试已指定的 versionId 仍在列表里则保留
  useEffect(() => {
    if (!open || publishedVersions.length === 0) return;
    if (versionId != null && publishedVersions.some((v) => v.id === versionId)) return;
    // agent 来源且版本还在加载：先别用空列表/旧缓存把指定 versionId 清掉
    if (configSource.type === 'agent' && versionsLoading) return;
    setVersionId(publishedVersions[0].id);
  }, [open, publishedVersions, versionId, configSource, versionsLoading]);

  const selectedVersion = publishedVersions.find((v) => v.id === versionId) ?? null;
  const selectedLoop = loops.find((l) => l.id === agentId) ?? null;

  const handleConfirm = async () => {
    if (mode === 'agent' && (!selectedVersion || agentId === null)) {
      message.warning('请选择要使用的 Agent 与版本');
      return;
    }
    setConfirming(true);
    try {
      const ok = await onConfirm(
        mode === 'free'
          ? { type: 'free' }
          : {
              type: 'agent',
              agentId: agentId as number,
              agentName: selectedLoop?.name ?? '',
              version: selectedVersion as AgentLoopVersion,
            },
      );
      if (ok) onClose();
    } finally {
      setConfirming(false);
    }
  };

  return (
    <Modal
      title="会话配置"
      open={open}
      onOk={handleConfirm}
      onCancel={onClose}
      okText="开始使用"
      cancelText="取消"
      confirmLoading={confirming}
      width={760}
      classNames={{ body: styles.configModalBody }}
    >
      <Segmented
        block
        value={mode}
        onChange={(v) => setMode(v as 'free' | 'agent')}
        options={SESSION_CONFIG_MODE_OPTIONS}
        className={styles.configModeSegmented}
      />

      {mode === 'free' ? (
        <FreeConfigPanel
          catalog={catalog}
          models={models}
          selectedModel={selectedModel}
          setSelectedModel={setSelectedModel}
          selectedTools={selectedTools}
          setSelectedTools={setSelectedTools}
          selectedToolModels={selectedToolModels}
          setSelectedToolModels={setSelectedToolModels}
          onOpenOverride={onOpenOverride}
        />
      ) : loops.length === 0 ? (
        <Empty description="暂无 Agent，可先在左栏「新建 Agent」创建一个" />
      ) : (
        <div>
          <div className={styles.configAgentPicker}>
            <Select
              value={agentId}
              onChange={(id) => {
                setAgentId(id);
                setVersionId(null);
              }}
              placeholder="选择 Agent"
              popupMatchSelectWidth={false}
              options={loops.map((l) => ({
                label: `${l.name}${l.latestVersion > 0 ? `（线上 v${l.latestVersion}）` : '（未发布）'}`,
                value: l.id,
              }))}
              className={styles.configAgentSelect}
            />
            <Select
              value={versionId}
              onChange={setVersionId}
              placeholder="选择版本"
              loading={versionsLoading}
              popupMatchSelectWidth={false}
              options={publishedVersions.map((v) => ({
                label: `v${v.version} · ${v.status === 'published' ? '已发布' : '草稿'} · ${v.createdAt}`,
                value: v.id,
              }))}
              className={styles.configVersionSelect}
            />
          </div>

          {versionsLoading ? (
            <div className={styles.configPreviewLoading}>
              <Spin />
            </div>
          ) : selectedVersion ? (
            <div className={styles.configPreview}>
              <VersionConfigOverview profile={selectedVersion.profileYaml} />
            </div>
          ) : (
            <Empty
              description={
                versions.length > 0
                  ? '该 Agent 暂无已发布版本，请先在「版本管理」中发布'
                  : '该 Agent 暂无版本'
              }
            />
          )}
        </div>
      )}
    </Modal>
  );
}

export default SessionConfigModal;
