/**
 * 工作流 Agent 左侧配置：编辑 definitionJson（与 Pipeline 草稿同源）
 */
import { Input, Tooltip } from 'antd';
import { NodeIndexOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import styles from '../AgentDetail/AgentDetail.module.less';
import localStyles from './WorkflowAgentDetail.module.less';

interface WorkflowConfigPanelProps {
  definitionJson: string;
  onChange: (value: string) => void;
}

function WorkflowConfigPanel({ definitionJson, onChange }: WorkflowConfigPanelProps) {
  return (
    <div className={localStyles.workflowConfigPanel}>
      <section className={localStyles.workflowSection}>
        <div className={styles.sectionTitle}>
          <NodeIndexOutlined className={styles.sectionIcon} />
          工作流定义
          <Tooltip title="对应 Pipeline 的 definitionJson，右侧运行会使用当前编辑内容">
            <QuestionCircleOutlined className={styles.helpIcon} />
          </Tooltip>
        </div>
        <p className={styles.fieldTip}>
          有自定义 workflows 时右侧按当前定义出表单；空对象则走默认 profile 的 Pipeline 列表。
        </p>
        <div className={localStyles.jsonEditorWrap}>
          <Input.TextArea
            value={definitionJson}
            onChange={(e) => onChange(e.target.value)}
            autoSize={{ minRows: 12, maxRows: 30 }}
            className={localStyles.jsonEditor}
            spellCheck={false}
            aria-label="工作流 definitionJson"
          />
        </div>
      </section>
    </div>
  );
}

export default WorkflowConfigPanel;
