/**
 * Pipeline / HTTP 能力只读卡片
 */
import { Tag } from 'antd';
import { ApiOutlined, NodeIndexOutlined } from '@ant-design/icons';
import { formatTimeout } from '@/utils/format';
import type { CatalogPipeline } from '@/pages/AgentPlayground/types';
import type { HttpToolDef, ProfilePipelineRef } from '../../types';
import styles from '../AgentDetail.module.less';

interface PipelineCardProps {
  pipeline: ProfilePipelineRef;
  /** 目录中的同 ref 声明，提供描述与超时等只读信息 */
  catalogPipeline?: CatalogPipeline;
}

function PipelineCard({ pipeline, catalogPipeline }: PipelineCardProps) {
  const description = catalogPipeline?.description;
  return (
    <article className={styles.capabilityCard}>
      <div className={styles.capabilityIcon}>
        <NodeIndexOutlined />
      </div>
      <div className={styles.capabilityBody}>
        <div className={styles.capabilityTitleRow}>
          <span className={styles.capabilityName}>{pipeline.displayName || pipeline.name}</span>
          <Tag className={styles.capabilityTag}>Pipeline</Tag>
        </div>
        {description && <p className={styles.capabilityDesc}>{description}</p>}
        <div className={styles.capabilityMeta}>
          <span>
            <em>ref</em> {pipeline.ref}
          </span>
          {catalogPipeline?.timeoutSeconds !== undefined && (
            <span>
              <em>timeout</em> {formatTimeout(catalogPipeline.timeoutSeconds)}
            </span>
          )}
        </div>
      </div>
    </article>
  );
}

function HttpToolCard({ tool }: { tool: HttpToolDef }) {
  const required = tool.inputSchema?.required ?? [];
  const properties = Object.entries(tool.inputSchema?.properties ?? {});
  return (
    <article className={styles.capabilityCard}>
      <div className={`${styles.capabilityIcon} ${styles.capabilityIconHttp}`}>
        <ApiOutlined />
      </div>
      <div className={styles.capabilityBody}>
        <div className={styles.capabilityTitleRow}>
          <span className={styles.capabilityName}>{tool.name}</span>
          <Tag className={styles.capabilityTag}>HTTP</Tag>
          {tool.async && <Tag className={styles.capabilityTagAsync}>异步</Tag>}
        </div>
        {tool.description && <p className={styles.capabilityDesc}>{tool.description}</p>}
        {tool.http?.url && (
          <div className={styles.httpLine}>
            <Tag className={styles.httpMethod}>{tool.http.method ?? 'POST'}</Tag>
            <span className={styles.httpUrl}>{tool.http.url}</span>
          </div>
        )}
        {properties.length > 0 && (
          <div className={styles.schemaList}>
            {properties.map(([name, prop]) => (
              <div key={name} className={styles.schemaItem}>
                <span className={styles.schemaName}>
                  {name}
                  {required.includes(name) && <span className={styles.schemaRequired}>*</span>}
                </span>
                <span className={styles.schemaType}>{prop.type ?? 'any'}</span>
                {prop.description && <span className={styles.schemaDesc}>{prop.description}</span>}
              </div>
            ))}
          </div>
        )}
        {tool.async && (
          <div className={styles.capabilityMeta}>
            <span>
              <em>status</em> {tool.async.statusField ?? '-'}
            </span>
            <span>
              <em>done</em> [{(tool.async.doneValues ?? []).join(', ')}]
            </span>
            <span>
              <em>failed</em> [{(tool.async.failedValues ?? []).join(', ')}]
            </span>
          </div>
        )}
      </div>
    </article>
  );
}

export { PipelineCard, HttpToolCard };
