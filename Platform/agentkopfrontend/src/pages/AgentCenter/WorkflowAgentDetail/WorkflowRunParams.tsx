/**
 * Pipeline 运行参数区：表单 / JSON，逻辑对齐 Pipeline 运行页
 * 模型字段选项统一来自 GET /agentk/models
 */
import { Input, InputNumber, Segmented } from 'antd';
import { WorkflowParamSelect } from '@/components';
import type { WorkflowDefinition } from '@/pages/AgentPlayground/types';
import styles from './WorkflowAgentDetail.module.less';

interface WorkflowRunParamsProps {
  selectedWorkflow: WorkflowDefinition | null;
  workflowParams: Record<string, string | number>;
  setWorkflowParams: React.Dispatch<React.SetStateAction<Record<string, string | number>>>;
  paramMode: 'form' | 'json';
  setParamMode: (mode: 'form' | 'json') => void;
  rawArgsJson: string;
  setRawArgsJson: (text: string) => void;
}

function WorkflowRunParams({
  selectedWorkflow,
  workflowParams,
  setWorkflowParams,
  paramMode,
  setParamMode,
  rawArgsJson,
  setRawArgsJson,
}: WorkflowRunParamsProps) {
  if (!selectedWorkflow) return null;

  return (
    <div className={styles.paramBlock}>
      <div className={styles.paramHead}>
        <span className={styles.paramTitle}>运行参数 · {selectedWorkflow.name}</span>
        {selectedWorkflow.params.length > 0 && (
          <Segmented
            size="small"
            value={paramMode}
            onChange={(v) => setParamMode(v as 'form' | 'json')}
            options={[
              { label: '表单', value: 'form' },
              { label: 'JSON', value: 'json' },
            ]}
          />
        )}
      </div>
      {paramMode === 'json' || selectedWorkflow.params.length === 0 ? (
        <Input.TextArea
          value={rawArgsJson}
          onChange={(e) => setRawArgsJson(e.target.value)}
          placeholder='{"key": "value"}'
          autoSize={{ minRows: 2, maxRows: 6 }}
          className={styles.paramInput}
        />
      ) : (
        <div className={styles.paramList}>
          {selectedWorkflow.params.map((p) => (
            <div key={p.name} className={styles.paramItem}>
              <label className={styles.paramLabel}>
                {p.label}
                {p.required && <span className={styles.paramRequired}>*</span>}
              </label>
              {p.type === 'array' || p.type === 'object' ? (
                <Input.TextArea
                  value={workflowParams[p.name] as string}
                  onChange={(e) =>
                    setWorkflowParams((prev) => ({ ...prev, [p.name]: e.target.value }))
                  }
                  placeholder={p.placeholder || p.label}
                  autoSize={{ minRows: 2, maxRows: 4 }}
                  className={styles.paramInput}
                />
              ) : p.type === 'number' ? (
                <InputNumber
                  value={workflowParams[p.name] as number}
                  onChange={(v) => setWorkflowParams((prev) => ({ ...prev, [p.name]: v ?? 0 }))}
                  min={p.min}
                  max={p.max}
                  placeholder={p.placeholder}
                  className={styles.paramInput}
                />
              ) : p.type === 'select' && (p.optionsFrom || p.options) ? (
                <WorkflowParamSelect
                  param={p}
                  value={workflowParams[p.name] as string}
                  onChange={(v) => setWorkflowParams((prev) => ({ ...prev, [p.name]: v }))}
                  className={styles.paramInput}
                />
              ) : (
                <Input
                  value={workflowParams[p.name] as string}
                  onChange={(e) =>
                    setWorkflowParams((prev) => ({ ...prev, [p.name]: e.target.value }))
                  }
                  placeholder={p.placeholder}
                  className={styles.paramInput}
                />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export default WorkflowRunParams;
