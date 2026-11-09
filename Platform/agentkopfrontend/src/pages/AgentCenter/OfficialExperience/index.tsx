/**
 * OfficialExperience - 官方 Agent 使用页
 * - 自主规划：OfficialFreeChat
 * - 工作流：CreationStudio（左表单 + 右 Inspector，非弹窗）
 * 「立即使用」可先带占位 session 进页；quick session 等到真正发起时再创建
 */
import { useMemo } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Spin } from 'antd';
import { useQuery } from '@tanstack/react-query';
import {
  OFFICIAL_DEFAULTS_QUERY_KEY,
  getOfficialDefaults,
  getOfficialWorkflowUiSchema,
  officialWorkflowSchemaKey,
} from '../api';
import {
  buildOfficialChatPath,
  parseExperienceType,
  resolveOfficialCard,
  toOfficialLoopCard,
  toOfficialWorkflowCard,
} from '../utils';
import { pickOfficialUiFields } from '@/utils/officialUiFields';
import { resolveKeffectsKind } from '@/services/keffectsTasks';
import ExperienceHeader from './ExperienceHeader';
import OfficialFreeChat from './OfficialFreeChat';
import CreationStudio from './studio/CreationStudio';
import ClothingDupeStudio from './studio/clothingDupe/ClothingDupeStudio';
import GoodsScraperStudio from './studio/keffects/GoodsScraperStudio';
import GoodsStudioStudio from './studio/keffects/GoodsStudioStudio';
import { useOfficialExperienceChat } from './useOfficialExperienceChat';
import { useOfficialTemplate } from './useOfficialTemplate';
import styles from './OfficialExperience.module.less';

function OfficialExperience() {
  const { sessionId = '' } = useParams<{ sessionId: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const catalogType = parseExperienceType(searchParams.get('type'));
  const agentLoopId = Number(searchParams.get('agentLoopId') || '') || undefined;
  const workflowId = Number(searchParams.get('workflowId') || '') || undefined;
  const workflowKey = searchParams.get('workflowKey') || undefined;
  const isWorkflow = catalogType === 'workflow';
  const { data, isLoading: catalogLoading } = useQuery({
    queryKey: OFFICIAL_DEFAULTS_QUERY_KEY,
    queryFn: getOfficialDefaults,
  });

  const item = useMemo(() => {
    const loops = (data?.agentLoops ?? []).map(toOfficialLoopCard);
    const workflows = (data?.workflowAgents ?? []).map(toOfficialWorkflowCard);
    return resolveOfficialCard(catalogType, agentLoopId, workflowId, workflowKey, loops, workflows);
  }, [data, catalogType, agentLoopId, workflowId, workflowKey]);

  const keffectsKind = useMemo(
    // keffects 是 workflow 专用任务；Agent-Loop 的名称/描述即使命中关键词，
    // 也必须保留 sessions/query 历史会话查询。
    () => (isWorkflow ? resolveKeffectsKind(workflowKey, item) : null),
    [isWorkflow, workflowKey, item],
  );

  const {
    data: schemaData,
    isLoading: schemaLoading,
    isFetching: schemaFetching,
    isError: schemaError,
  } = useQuery({
    queryKey: officialWorkflowSchemaKey(workflowKey || ''),
    queryFn: () => getOfficialWorkflowUiSchema(workflowKey!),
    enabled: isWorkflow && !!workflowKey && !keffectsKind,
    staleTime: 5 * 60 * 1000,
  });

  /** 仅使用 capabilities 下发的 fields / uiSchema.fields / inputSchema，无内置表单 */
  const schemaFields = useMemo(() => pickOfficialUiFields(schemaData), [schemaData]);
  const schemaPending =
    isWorkflow && !!workflowKey && !keffectsKind && (schemaLoading || schemaFetching);

  const chat = useOfficialExperienceChat({
    sessionId,
    item,
    // keffects 用 pipeline-execs 历史；agent_loop / 普通 workflow 用 sessions/query
    loadSessions: !keffectsKind,
  });
  const { usingTemplate, applyTemplate } = useOfficialTemplate(item);

  const openSession = (sid: string) => {
    if (!item) return;
    navigate(buildOfficialChatPath(sid, item), { replace: true });
  };

  if (!sessionId) {
    return (
      <div className={styles.loading}>
        <Spin />
      </div>
    );
  }

  if (catalogLoading && !item) {
    return (
      <div className={styles.loading}>
        <Spin />
      </div>
    );
  }

  const studioHeader = (
    <ExperienceHeader
      item={item}
      isRunning={chat.sseState.isRunning}
      hasPendingJobs={chat.sseState.jobs.some((job) => job.status === 'submitted')}
      hasError={!!chat.sseState.error}
      usingTemplate={usingTemplate}
      onUseTemplate={() => void applyTemplate()}
    />
  );

  return (
    <div className={`${styles.page} ${isWorkflow ? styles.pageStudio : styles.pageChat}`}>
      {isWorkflow ? (
        keffectsKind === 'clothing-dupe' ? (
          <ClothingDupeStudio header={studioHeader} pipelineName={item?.pipelineName} />
        ) : keffectsKind === 'goods-scraper' ? (
          <GoodsScraperStudio header={studioHeader} pipelineName={item?.pipelineName} />
        ) : keffectsKind === 'goods-studio' ? (
          <GoodsStudioStudio header={studioHeader} pipelineName={item?.pipelineName} />
        ) : schemaPending && schemaFields.length === 0 ? (
          <div className={styles.loading}>
            <Spin tip={schemaError ? '配置加载失败，重试中…' : '加载创作参数…'}>
              <div className={styles.loadingPlaceholder} />
            </Spin>
          </div>
        ) : (
          <CreationStudio
            header={studioHeader}
            sessionId={sessionId}
            catalogType="workflow"
            workflowKey={workflowKey || item?.workflowKey}
            pipelineName={item?.pipelineName}
            schemaFields={schemaFields}
            agentLoopId={item?.agentLoopId}
            sessions={chat.sessions}
            historyLoading={chat.isLoadingHistory}
            onOpenSession={openSession}
            onRefreshSessions={chat.refreshSessions}
            enableSessionPaging
            sessionsLoading={chat.sessionsLoading}
            isLoadingMoreSessions={chat.isLoadingMoreSessions}
            sessionsHasMore={chat.sessionsHasMore}
            historyListRef={chat.historyListRef}
            onHistoryScroll={chat.handleHistoryScroll}
            sseState={chat.sseState}
            sseActions={chat.sseActions}
          />
        )
      ) : (
        <OfficialFreeChat
          sessionId={sessionId}
          item={item}
          chat={chat}
          usingTemplate={usingTemplate}
          onCustomize={() => void applyTemplate()}
        />
      )}
    </div>
  );
}

export default OfficialExperience;
