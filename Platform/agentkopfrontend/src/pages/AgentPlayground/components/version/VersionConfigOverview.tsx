/**
 * 版本配置总览（可视化）
 * 从 Profile 文档渲染：系统提示词 / 可用工具 / Pipeline。
 * 供版本详情弹窗与「会话配置」弹窗复用
 */
import { useMemo } from 'react';
import { Space, Tag } from 'antd';
import { ApiOutlined } from '@ant-design/icons';
import { parseProfileYaml, sessionProfileToYaml } from '@/utils/profileYaml';
import type { BaseProfile } from '../../types';
import styles from '../../AgentPlayground.module.less';

function VersionConfigOverview({ profile }: { profile: BaseProfile | string }) {
  const yaml = useMemo(
    () => (typeof profile === 'string' ? profile : sessionProfileToYaml(profile)),
    [profile],
  );
  const parsed = useMemo(() => parseProfileYaml(yaml), [yaml]);

  const tools = parsed.tools || [];
  const pipelines = parsed.pipelines || [];

  return (
    <div>
      {/* 系统提示词 */}
      <div className={styles.detailSection}>
        <div className={styles.detailSectionTitle}>系统提示词</div>
        {parsed.systemPrompt ? (
          <div className={styles.detailSystemPrompt}>{parsed.systemPrompt}</div>
        ) : (
          <span className={styles.detailEmpty}>未配置</span>
        )}
      </div>

      {/* 可用工具 */}
      <div className={styles.detailSection}>
        <div className={styles.detailSectionTitle}>可用工具</div>
        {tools.length === 0 ? (
          <span className={styles.detailEmpty}>未配置</span>
        ) : (
          <Space size={[8, 8]} wrap>
            {tools.map((tool) => (
              <Tag key={tool} className={styles.toolTag}>
                <ApiOutlined /> {tool}
              </Tag>
            ))}
          </Space>
        )}
      </div>

      {/* Pipeline */}
      <div className={styles.detailSection}>
        <div className={styles.detailSectionTitle}>Pipeline</div>
        {pipelines.length > 0 ? (
          <Space size={[8, 8]} wrap>
            {pipelines.map((pipeline) => (
              <Tag key={pipeline.name} color="processing">
                {pipeline.displayName || pipeline.name}
              </Tag>
            ))}
          </Space>
        ) : (
          <span className={styles.detailEmpty}>未配置</span>
        )}
      </div>
    </div>
  );
}

export default VersionConfigOverview;
