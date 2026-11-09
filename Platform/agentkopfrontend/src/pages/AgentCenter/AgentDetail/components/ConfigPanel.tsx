/**
 * ConfigPanel - 详情页左侧配置面板
 * 对话模型 + 角色指令 + 能力扩展（工具弹窗）+ Pipeline / HTTP 能力铺开
 * 模型选项来自 GET /agentk/models 的 chatModels
 */
import { Input, Tooltip } from 'antd';
import {
  ApiOutlined,
  AppstoreOutlined,
  NodeIndexOutlined,
  QuestionCircleOutlined,
  UserOutlined,
} from '@ant-design/icons';
import type { AgentDetailData } from '../../types';
import type { CatalogModel } from '@/pages/AgentPlayground/types';
import type { OfficialToolItem } from '@/types/officialTools';
import { HttpToolCard, PipelineCard } from './CapabilityCards';
import { ModelSelectSection } from './ModelSelectSection';
import { ToolSelectSection } from './ToolSelectSection';
import type { ToolDefaultsMap } from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

interface ConfigPanelProps {
  detail: AgentDetailData;
  system: string;
  defaultModel: string;
  chatModels: CatalogModel[];
  modelsLoading?: boolean;
  tools: string[];
  toolDefaults: ToolDefaultsMap;
  officialTools: OfficialToolItem[];
  onSystemChange: (v: string) => void;
  onDefaultModelChange: (v: string) => void;
  onToolsChange: (tools: string[], toolDefaults: ToolDefaultsMap) => void;
}

export function RolePromptContent({
  system,
  onSystemChange,
}: Pick<ConfigPanelProps, 'system' | 'onSystemChange'>) {
  return (
    <section className={styles.promptPanel}>
      <div className={styles.sectionTitle}>
        <UserOutlined className={styles.sectionIcon} />
        角色指令
        <Tooltip title="System Prompt：定义 Agent 的角色、能力边界与工具使用规则">
          <QuestionCircleOutlined className={styles.helpIcon} />
        </Tooltip>
        <span className={styles.charCount}>{system.length} 字</span>
      </div>
      <Input.TextArea
        value={system}
        onChange={(e) => onSystemChange(e.target.value)}
        autoSize={{ minRows: 18, maxRows: 28 }}
        placeholder="定义 Agent 的角色与行为准则"
        className={styles.promptArea}
      />
    </section>
  );
}

export function CapabilityConfigContent({
  detail,
  defaultModel,
  chatModels,
  modelsLoading = false,
  tools,
  toolDefaults,
  officialTools,
  onDefaultModelChange,
  onToolsChange,
}: Omit<ConfigPanelProps, 'system' | 'onSystemChange'>) {
  return (
    <div className={styles.capabilityPanel}>
      {/* 对话模型：摘要回显 profile，下拉选项来自 /agentk/models */}
      <section className={styles.section}>
        <div className={styles.sectionTitle}>
          对话模型
          <Tooltip title="当前值回显自配置；下拉选项来自模型目录 /agentk/models">
            <QuestionCircleOutlined className={styles.helpIcon} />
          </Tooltip>
        </div>
        <ModelSelectSection
          models={chatModels}
          value={defaultModel}
          loading={modelsLoading}
          onChange={onDefaultModelChange}
        />
      </section>

      {/* 能力扩展 */}
      <section className={styles.section}>
        <div className={styles.sectionTitle}>
          <AppstoreOutlined className={styles.sectionIcon} />
          能力扩展
        </div>

        <ToolSelectSection
          tools={tools}
          toolDefaults={toolDefaults}
          officialTools={officialTools}
          onChange={onToolsChange}
        />
      </section>

      {/* Pipeline 能力 */}
      {detail.pipelines.length > 0 && (
        <section className={styles.section}>
          <div className={styles.sectionTitle}>
            <NodeIndexOutlined className={styles.sectionIcon} />
            Pipeline 能力
            <span className={styles.fieldHint}>{detail.pipelines.length} 个</span>
          </div>
          <div className={styles.capabilityList}>
            {detail.pipelines.map((p) => (
              <PipelineCard
                key={p.ref || p.name}
                pipeline={p}
                catalogPipeline={detail.catalogPipelines.find((c) => c.ref === p.ref)}
              />
            ))}
          </div>
        </section>
      )}

      {/* HTTP 工具 */}
      {detail.httpTools.length > 0 && (
        <section className={styles.section}>
          <div className={styles.sectionTitle}>
            <ApiOutlined className={styles.sectionIcon} />
            自定义 HTTP 工具
            <span className={styles.fieldHint}>{detail.httpTools.length} 个</span>
          </div>
          <div className={styles.capabilityList}>
            {detail.httpTools.map((t) => (
              <HttpToolCard key={t.name} tool={t} />
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
