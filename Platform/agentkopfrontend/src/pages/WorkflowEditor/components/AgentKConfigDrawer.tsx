/**
 * Agent-k 全局配置抽屉
 * UI 对齐 AgentPlayground 的新会话设置：对话模型 + 工具多选 + System Prompt
 * 这份配置在「调试运行」时随 AgentConfigPayload 导入到 playground，覆盖新会话的 profile
 */
import { useMemo } from 'react';
import { Drawer, Form, Input, Checkbox, Button, Space, Tag, Divider, Empty } from 'antd';
import { SyncOutlined, InfoCircleOutlined } from '@ant-design/icons';
import { ModelNameSelect } from '@/components';
import { useModelCatalog } from '@/hooks/useModelCatalog';
import { chatModelOptions, pickDefaultChatModel, toSelectOptions } from '@/utils/models';
import { useWorkflowStore } from '../store';
import { useCatalog } from '../hooks/useCatalog';
import styles from './AgentKConfigDrawer.module.less';

const { TextArea } = Input;

interface AgentKConfigDrawerProps {
  open: boolean;
  onClose: () => void;
}

function AgentKConfigDrawer({ open, onClose }: AgentKConfigDrawerProps) {
  const agentKConfig = useWorkflowStore((s) => s.agentKConfig);
  const setAgentKConfig = useWorkflowStore((s) => s.setAgentKConfig);
  const syncAgentKConfigFromCanvas = useWorkflowStore((s) => s.syncAgentKConfigFromCanvas);
  const { catalog } = useCatalog();
  const { catalog: modelCatalog, isLoading: modelsLoading } = useModelCatalog({ enabled: open });

  // 工具选项：来自能力目录，对齐 playground 的「可用工具」列表
  // 目录未加载时返回空数组，不使用任何内置降级列表（避免展示后端不存在的工具）
  const toolOptions = useMemo(() => catalog?.toolNames ?? [], [catalog]);
  const chatOptions = useMemo(
    () =>
      toSelectOptions(
        chatModelOptions(modelCatalog, agentKConfig.model),
        pickDefaultChatModel(modelCatalog),
      ),
    [modelCatalog, agentKConfig.model],
  );

  const handleToolToggle = (tool: string, checked: boolean) => {
    if (checked) {
      setAgentKConfig({ tools: [...agentKConfig.tools, tool] });
    } else {
      setAgentKConfig({ tools: agentKConfig.tools.filter((t) => t !== tool) });
    }
  };

  const handleSyncFromCanvas = () => {
    syncAgentKConfigFromCanvas();
  };

  return (
    <Drawer
      title="Agent-k 配置"
      placement="right"
      width={420}
      open={open}
      onClose={onClose}
      className={styles.panel}
      extra={
        <Space>
          <Button
            icon={<SyncOutlined />}
            size="small"
            onClick={handleSyncFromCanvas}
            title="从画布的 LLM/Tool 节点提取配置"
          >
            从画布同步
          </Button>
        </Space>
      }
    >
      <div className={styles.hintBar}>
        <InfoCircleOutlined />
        <span>
          这份配置与 agent-k playground 的「新会话设置」对齐：工具多选、System
          Prompt。点击「调试运行」时会随会话请求发送，覆盖 playground 的 profile。
        </span>
      </div>

      {catalog ? (
        <div className={styles.profileMeta}>
          <Tag color="blue" className={styles.profileTag}>
            能力目录
          </Tag>
          <span className={styles.profileHint}>
            工具 {catalog.tools.length} · Pipeline {catalog.pipelines.length}
          </span>
        </div>
      ) : (
        <div className={styles.profileMeta}>
          <Tag color="default" className={styles.profileTag}>
            能力目录未加载
          </Tag>
          <span className={styles.profileHint}>无可用工具，请检查后端能力目录</span>
        </div>
      )}

      <Divider className={styles.divider} />

      <Form layout="vertical" size="small">
        <Form.Item label="对话模型" className={styles.formItem}>
          <ModelNameSelect
            allowClear
            loading={modelsLoading}
            placeholder="请选择或输入对话模型"
            value={agentKConfig.model || undefined}
            options={chatOptions}
            onChange={(model) => setAgentKConfig({ model })}
          />
          <div className={styles.fieldHint}>选项来自 /agentk/models 的 chatModels</div>
        </Form.Item>

        <Form.Item label="本次可用工具" className={styles.formItem}>
          <div className={styles.toolCheckboxes}>
            {toolOptions.length === 0 && (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="无可用工具"
                className={styles.toolEmpty}
              />
            )}
            {toolOptions.map((t) => (
              <Checkbox
                key={t}
                checked={agentKConfig.tools.includes(t)}
                onChange={(e) => handleToolToggle(t, e.target.checked)}
                className={styles.toolCheckbox}
              >
                {t}
              </Checkbox>
            ))}
          </div>
          <div className={styles.fieldHint}>
            对应 playground 新会话设置中的「本次可用工具」多选，已选 {agentKConfig.tools.length} 个
          </div>
        </Form.Item>

        <Form.Item label="System Prompt" className={styles.formItem}>
          <TextArea
            value={agentKConfig.systemPrompt}
            onChange={(e) => setAgentKConfig({ systemPrompt: e.target.value })}
            rows={8}
            placeholder="输入系统提示词，定义 Agent 的人设和行为..."
            className={styles.textarea}
          />
          <div className={styles.fieldHint}>
            这段 System Prompt 会写入会话 profile YAML 的 system 字段，覆盖新会话的能力配置
          </div>
        </Form.Item>
      </Form>
    </Drawer>
  );
}

export default AgentKConfigDrawer;
