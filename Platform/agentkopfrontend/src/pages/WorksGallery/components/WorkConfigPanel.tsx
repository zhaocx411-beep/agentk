import { Tag, Tooltip } from 'antd';
import { CodeOutlined, EyeOutlined, SettingOutlined, ToolOutlined } from '@ant-design/icons';
import type { PortfolioAssetDetail, PortfolioProfileTool } from '../types';
import styles from './WorkDetailModal.module.less';

interface WorkConfigPanelProps {
  detail: PortfolioAssetDetail;
}

function parseLegacyTools(value?: string): PortfolioProfileTool[] {
  if (!value) return [];
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item): item is string => typeof item === 'string')
      .map((name) => ({ name, ref: name }));
  } catch {
    return [];
  }
}

/** 展示作品运行时使用的模型、工具与角色指令 */
export function WorkConfigPanel({ detail }: WorkConfigPanelProps) {
  const model = detail.profile?.defaultModel || detail.session?.effectiveModel || detail.run?.model;
  const tools =
    detail.profile?.tools && detail.profile.tools.length > 0
      ? detail.profile.tools
      : parseLegacyTools(detail.session?.effectiveToolsJson);
  const sourceLabel = detail.agentName || detail.sourceName || detail.workflowName;
  const hasRuntimeConfig = Boolean(model || tools.length > 0 || detail.profile?.system);
  const hasSourceMeta = Boolean(sourceLabel || detail.workflowExecId || detail.sourceKind);

  return (
    <div className={styles.configArea}>
      {!hasRuntimeConfig && hasSourceMeta ? (
        <section className={styles.section}>
          <div className={styles.sectionLabel}>
            <SettingOutlined className={styles.sectionIcon} />
            来源
          </div>
          <div className={styles.toolTags}>
            {sourceLabel ? <Tag className={styles.toolTag}>{sourceLabel}</Tag> : null}
            {detail.sourceKind ? <Tag>{detail.sourceKind}</Tag> : null}
            {detail.source ? <Tag>{detail.source}</Tag> : null}
          </div>
          {detail.workflowExecId ? (
            <div className={`${styles.sectionContent} ${styles.promptContent}`}>
              执行 ID：{detail.workflowExecId}
            </div>
          ) : null}
        </section>
      ) : null}

      <section className={styles.section}>
        <div className={styles.sectionLabel}>
          <SettingOutlined className={styles.sectionIcon} />
          模型
        </div>
        {model ? (
          <div>
            <Tag className={styles.modelTag}>{model}</Tag>
          </div>
        ) : (
          <span className={styles.mutedText}>暂无模型信息</span>
        )}
      </section>

      <section className={styles.section}>
        <div className={styles.sectionLabel}>
          <ToolOutlined className={styles.sectionIcon} />
          工具
        </div>
        <div className={styles.toolTags}>
          {tools.length > 0 ? (
            tools.map((tool) => (
              <Tooltip key={tool.ref || tool.name} title={tool.instructions}>
                <Tag className={styles.toolTag}>{tool.name}</Tag>
              </Tooltip>
            ))
          ) : (
            <span className={styles.mutedText}>暂无工具信息</span>
          )}
        </div>
      </section>

      <section className={styles.section}>
        <div className={styles.sectionLabel}>
          <CodeOutlined className={styles.sectionIcon} />
          创作指令
        </div>
        <div className={`${styles.sectionContent} ${styles.promptContent}`}>
          {detail.promptText || '未记录创作指令'}
        </div>
      </section>

      {detail.profile?.system && (
        <section className={styles.section}>
          <div className={styles.sectionLabel}>
            <EyeOutlined className={styles.sectionIcon} />
            角色指令
          </div>
          <div className={`${styles.sectionContent} ${styles.systemPromptContent}`}>
            {detail.profile.system}
          </div>
        </section>
      )}
    </div>
  );
}
