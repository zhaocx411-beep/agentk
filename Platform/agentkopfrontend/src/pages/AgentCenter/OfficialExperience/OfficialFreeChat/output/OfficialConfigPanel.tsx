/** 官方自主 Agent 配置内容：嵌入右侧上下文栏。 */
import { useMemo } from 'react';
import { Empty, Spin, Tag, Tooltip } from 'antd';
import { ApiOutlined, RobotOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { agentLoopVersionsKey, getAgentLoopVersions } from '@/services/agentLoop';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import type { BaseProfile } from '@/pages/AgentPlayground/types';
import { toToolNames } from '@/utils/profile';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import styles from '../OfficialFreeChat.module.less';

interface OfficialConfigPanelProps {
  item: OfficialCatalogCardData | null;
  sseState: SSEState;
}

function asProfile(raw: unknown): BaseProfile | null {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
  return raw as BaseProfile;
}

function OfficialConfigPanel({ item, sseState }: OfficialConfigPanelProps) {
  const agentLoopId = item?.agentLoopId;
  const {
    data: versions,
    isLoading,
    isError,
  } = useQuery({
    queryKey: agentLoopVersionsKey(agentLoopId ?? -1),
    queryFn: () => getAgentLoopVersions(agentLoopId as number),
    enabled: agentLoopId != null,
    staleTime: 5 * 60 * 1000,
  });

  const version = useMemo(() => {
    if (!versions?.length) return null;
    return versions.find((row) => row.id === item?.agentLoopVersionId) ?? versions[0];
  }, [versions, item?.agentLoopVersionId]);

  const profile = useMemo(() => asProfile(version?.profileYaml), [version]);
  const tools = useMemo(() => toToolNames(profile?.tools), [profile]);
  const systemPrompt = profile?.system?.trim() || '';
  const model = profile?.defaultModel?.trim() || '未指定（由服务端缺省）';

  return (
    <div className={styles.configBody}>
      <p className={styles.configIntro}>当前体验所使用的 Agent 参数</p>
      {isLoading ? (
        <div className={styles.configStatus}>
          <Spin size="small" />
        </div>
      ) : isError || !item ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无配置信息" />
      ) : (
        <>
          <section className={styles.configSection}>
            <h3 className={styles.configSectionTitle}>基本信息</h3>
            <dl className={styles.configMeta}>
              <div className={styles.configMetaRow}>
                <dt>名称</dt>
                <dd>{item.name}</dd>
              </div>
              <div className={styles.configMetaRow}>
                <dt>类型</dt>
                <dd>Agent-Loop</dd>
              </div>
              <div className={styles.configMetaRow}>
                <dt>版本</dt>
                <dd>
                  {version
                    ? version.profileVersionLabel || `v${version.version}`
                    : item.skills[0] || '—'}
                </dd>
              </div>
              {version?.status ? (
                <div className={styles.configMetaRow}>
                  <dt>状态</dt>
                  <dd>{version.status === 'published' ? '已发布' : version.status}</dd>
                </div>
              ) : null}
            </dl>
          </section>

          <section className={styles.configSection}>
            <h3 className={styles.configSectionTitle}>
              <RobotOutlined /> 默认模型
            </h3>
            <p className={styles.configModel}>{model}</p>
            {sseState.runSnapshot?.profileVersion ? (
              <p className={styles.configHint}>
                本次运行 profile：{sseState.runSnapshot.profileVersion}
              </p>
            ) : null}
          </section>

          <section className={styles.configSection}>
            <h3 className={styles.configSectionTitle}>
              <ApiOutlined /> 可用工具
              <span className={styles.configCount}>{tools.length}</span>
            </h3>
            {tools.length === 0 ? (
              <p className={styles.configEmpty}>未配置工具</p>
            ) : (
              <div className={styles.configTags}>
                {tools.map((tool) => (
                  <Tooltip key={tool} title={tool}>
                    <Tag className={styles.configTag}>{tool}</Tag>
                  </Tooltip>
                ))}
              </div>
            )}
          </section>

          {item.skills.length > 0 ? (
            <section className={styles.configSection}>
              <h3 className={styles.configSectionTitle}>能力标签</h3>
              <div className={styles.configTags}>
                {item.skills.map((skill) => (
                  <Tag key={skill} className={styles.configSkillTag}>
                    {skill}
                  </Tag>
                ))}
              </div>
            </section>
          ) : null}

          <section className={styles.configSection}>
            <h3 className={styles.configSectionTitle}>系统提示词</h3>
            {systemPrompt ? (
              <pre className={styles.configPrompt}>{systemPrompt}</pre>
            ) : (
              <p className={styles.configEmpty}>未配置</p>
            )}
          </section>

          {item.description ? (
            <section className={styles.configSection}>
              <h3 className={styles.configSectionTitle}>说明</h3>
              <p className={styles.configDesc}>{item.detail || item.description}</p>
            </section>
          ) : null}
        </>
      )}
    </div>
  );
}

export default OfficialConfigPanel;
