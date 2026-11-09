import { useCallback, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Drawer, Form, Input, Slider, Button, Space, Empty, Tag, Checkbox } from 'antd';
import { DeleteOutlined, ExportOutlined, InfoCircleOutlined } from '@ant-design/icons';
import { ModelNameSelect } from '@/components';
import { MODELS_QUERY_KEY, fetchModels } from '@/services/workflow';
import { toSelectOptions } from '@/utils/models';
import { useWorkflowStore } from '../store';
import { useCatalog } from '../hooks/useCatalog';
import type { AgentNodeType } from '../types';
import { AGENT_NODE_TYPE_CONFIG } from '../constants';
import styles from './NodeConfigPanel.module.less';

/** 节点配置侧边面板 - 按节点类型分区段渲染 */
function NodeConfigPanel() {
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const nodes = useWorkflowStore((s) => s.nodes);
  const updateNodeData = useWorkflowStore((s) => s.updateNodeData);
  const deleteNode = useWorkflowStore((s) => s.deleteNode);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const exportWorkflow = useWorkflowStore((s) => s.exportWorkflow);

  // 拉取能力目录，提供真实的工具列表
  const { catalog } = useCatalog();
  const { data: modelCatalog, isLoading: modelsLoading } = useQuery({
    queryKey: MODELS_QUERY_KEY,
    queryFn: fetchModels,
  });

  const selectedNode = nodes.find((n) => n.id === selectedNodeId);

  // 工具选项：来自能力目录，对齐 playground 的「可用工具」列表
  // 目录未加载时返回空数组，不使用任何内置降级列表（避免展示后端不存在的工具）
  const toolOptions = useMemo(() => catalog?.toolNames ?? [], [catalog]);
  const chatModelOptions = useMemo(
    () => toSelectOptions(modelCatalog?.chatModels ?? []),
    [modelCatalog],
  );

  const handleClose = useCallback(() => {
    setSelectedNode(null);
  }, [setSelectedNode]);

  const handleDelete = useCallback(() => {
    if (selectedNodeId) {
      deleteNode(selectedNodeId);
    }
  }, [selectedNodeId, deleteNode]);

  if (!selectedNode) {
    return (
      <Drawer title="节点配置" placement="right" width={360} open={false} onClose={handleClose}>
        <Empty description="点击画布节点查看配置" />
      </Drawer>
    );
  }

  const { data } = selectedNode;
  const typeConfig = AGENT_NODE_TYPE_CONFIG[data.type as AgentNodeType];

  return (
    <Drawer
      title={
        <Space>
          <span>{typeConfig?.icon}</span>
          <span>节点配置 · {data.label}</span>
        </Space>
      }
      placement="right"
      width={380}
      open={!!selectedNodeId}
      onClose={handleClose}
      className={styles.panel}
      extra={
        <Space>
          <Button
            icon={<ExportOutlined />}
            size="small"
            onClick={() => {
              const wf = exportWorkflow();
              const blob = new Blob([JSON.stringify(wf, null, 2)], { type: 'application/json' });
              const url = URL.createObjectURL(blob);
              const a = document.createElement('a');
              a.href = url;
              a.download = 'pipeline.json';
              a.click();
              URL.revokeObjectURL(url);
            }}
          >
            导出
          </Button>
          {data.type !== 'start' && data.type !== 'end' && (
            <Button icon={<DeleteOutlined />} size="small" danger onClick={handleDelete}>
              删除
            </Button>
          )}
        </Space>
      }
    >
      {/* 节点基本信息 */}
      <div className={styles.nodeInfo}>
        <div className={styles.nodeInfoRow}>
          <span>类型：</span>
          <span className={styles.nodeInfoValue}>
            {typeConfig?.icon} {typeConfig?.label}
          </span>
        </div>
        <div className={styles.nodeInfoRow}>
          <span>节点 ID：</span>
          <span className={styles.nodeInfoValue}>{selectedNode.id}</span>
        </div>
        {data.outputVar && (
          <div className={styles.nodeInfoRow}>
            <span>输出变量：</span>
            <Tag color="purple" style={{ fontSize: 11 }}>
              {data.outputVar}
            </Tag>
          </div>
        )}
      </div>

      {/* 基础配置区段 */}
      <div className={styles.section}>
        <div className={styles.sectionTitle}>基础配置</div>
        <Form layout="vertical" size="small">
          <Form.Item label="节点名称">
            <Input
              value={data.label}
              onChange={(e) => updateNodeData(selectedNode.id, { label: e.target.value })}
              placeholder="给节点起个名字"
            />
          </Form.Item>
          <Form.Item label="描述">
            <Input.TextArea
              value={data.description ?? ''}
              rows={2}
              onChange={(e) => updateNodeData(selectedNode.id, { description: e.target.value })}
              placeholder="说明这个节点做什么（可选）"
            />
          </Form.Item>
          {data.type !== 'start' && data.type !== 'end' && (
            <Form.Item
              label="输出变量名"
              tooltip="下游节点可通过此变量名引用本节点的输出（前端编排能力）"
            >
              <Input
                value={data.outputVar ?? ''}
                onChange={(e) => updateNodeData(selectedNode.id, { outputVar: e.target.value })}
                placeholder="如 llm_output"
              />
            </Form.Item>
          )}
        </Form>
      </div>

      {/* LLM 节点专属配置 */}
      {data.type === 'llm' && (
        <div className={styles.section}>
          <div className={styles.sectionTitle}>模型配置</div>
          <Form layout="vertical" size="small">
            <Form.Item label="模型">
              <ModelNameSelect
                allowClear
                loading={modelsLoading}
                value={data.model || undefined}
                onChange={(model) => updateNodeData(selectedNode.id, { model })}
                placeholder="选择或输入对话模型（可清空用实例默认）"
                options={chatModelOptions}
              />
              <div className={styles.fieldHint}>选项来自 /agentk/models 的 chatModels</div>
            </Form.Item>
            <Form.Item label="System Prompt">
              <Input.TextArea
                value={data.prompt ?? ''}
                rows={5}
                onChange={(e) => updateNodeData(selectedNode.id, { prompt: e.target.value })}
                placeholder="输入系统提示词，定义 Agent 的人设和行为..."
              />
            </Form.Item>
            <Form.Item label={`Temperature: ${data.temperature ?? 0.7}`}>
              <Slider
                min={0}
                max={2}
                step={0.1}
                value={data.temperature ?? 0.7}
                onChange={(v) => updateNodeData(selectedNode.id, { temperature: v })}
              />
              <div className={styles.fieldHint}>值越低输出越确定，值越高越有创造性</div>
            </Form.Item>
          </Form>
        </div>
      )}

      {/* 工具节点专属配置 */}
      {data.type === 'tool' && (
        <div className={styles.section}>
          <div className={styles.sectionTitle}>工具配置</div>
          <Form layout="vertical" size="small">
            <Form.Item
              label="可用工具"
              tooltip="对齐 agent-k playground 新会话设置：可多选，选项来自实例能力目录"
            >
              <div className={styles.toolCheckboxes}>
                {toolOptions.length === 0 && (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="无可用工具，请检查能力目录"
                    className={styles.toolEmpty}
                  />
                )}
                {toolOptions.map((t) => (
                  <Checkbox
                    key={t}
                    checked={data.toolNames?.includes(t) ?? false}
                    onChange={(e) => {
                      const prev = data.toolNames ?? [];
                      const next = e.target.checked ? [...prev, t] : prev.filter((x) => x !== t);
                      updateNodeData(selectedNode.id, { toolNames: next });
                    }}
                    className={styles.toolCheckbox}
                  >
                    {t}
                  </Checkbox>
                ))}
              </div>
              <div className={styles.fieldHint}>
                可选工具来自实例能力目录，共 {toolOptions.length} 个 · 已选{' '}
                {data.toolNames?.length ?? 0} 个
              </div>
            </Form.Item>
          </Form>
        </div>
      )}

      {/* 条件节点专属配置 */}
      {data.type === 'condition' && (
        <div className={styles.section}>
          <div className={styles.sectionTitle}>条件配置</div>
          <Form layout="vertical" size="small">
            <Form.Item label="条件表达式" tooltip="根据表达式结果决定走 yes / no 分支">
              <Input
                value={data.conditionExpr ?? ''}
                onChange={(e) => updateNodeData(selectedNode.id, { conditionExpr: e.target.value })}
                placeholder="例：intent === 'search'"
              />
            </Form.Item>
            <div className={styles.varHint}>
              <InfoCircleOutlined /> 支持引用上游变量，如 {'{{llm_node.llm_output}}'}
            </div>
          </Form>
        </div>
      )}

      {/* 子流程节点专属配置 */}
      {data.type === 'subflow' && (
        <div className={styles.section}>
          <div className={styles.sectionTitle}>子流程配置</div>
          <Form layout="vertical" size="small">
            <Form.Item label="子流程 ID">
              <Input
                value={data.subflowId ?? ''}
                onChange={(e) => updateNodeData(selectedNode.id, { subflowId: e.target.value })}
                placeholder="关联子 Pipeline ID"
              />
            </Form.Item>
          </Form>
        </div>
      )}

      {/* Start / End 节点提示 */}
      {(data.type === 'start' || data.type === 'end') && (
        <div className={styles.section}>
          <div className={styles.sectionTitle}>说明</div>
          <div className={styles.fieldHint}>
            {data.type === 'start'
              ? '开始节点是 Pipeline 入口，用户输入从这里进入。一个 Pipeline 只能有一个开始节点。'
              : '结束节点是 Pipeline 出口，运行结果从这里输出。一个 Pipeline 只能有一个结束节点。'}
          </div>
        </div>
      )}
    </Drawer>
  );
}

export default NodeConfigPanel;
