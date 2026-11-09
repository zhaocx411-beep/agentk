/**
 * 自由配置面板：模型选择 + 工具勾选 + 工具子模型 + 定制配置入口
 * 从 Sidebar 抽取，供「会话配置」弹窗的自由配置模式复用
 * 模型选项一律来自 GET /agentk/models
 */
import { Button, Checkbox, Select, Tooltip } from 'antd';
import { pickDefaultChatModel, toSelectOptions, toolModelOptions } from '@/utils/models';
import type { CapabilityCatalog, ModelCatalog } from '../../types';
import styles from '../../AgentPlayground.module.less';

interface FreeConfigPanelProps {
  catalog: CapabilityCatalog | null;
  models: ModelCatalog | null;
  selectedModel: string;
  setSelectedModel: (m: string) => void;
  selectedTools: string[];
  setSelectedTools: (fn: (prev: string[]) => string[]) => void;
  selectedToolModels: Record<string, string>;
  setSelectedToolModels: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  /** 打开定制配置（override）弹窗 */
  onOpenOverride: () => void;
}

function FreeConfigPanel({
  catalog,
  models,
  selectedModel,
  setSelectedModel,
  selectedTools,
  setSelectedTools,
  selectedToolModels,
  setSelectedToolModels,
  onOpenOverride,
}: FreeConfigPanelProps) {
  const descriptions = new Map((catalog?.tools ?? []).map((tool) => [tool.name, tool.description]));
  // 目录工具 ∪ 定制配置里额外声明的工具（如 httpTools），让 override 的工具也能看到勾选状态
  const toolNames = [...new Set([...(catalog?.toolNames ?? []), ...selectedTools])];
  const defaultChat = pickDefaultChatModel(models);
  const chatOptions = toSelectOptions(models?.chatModels ?? [], defaultChat);

  return (
    <div>
      <div className={styles.sidebarSubSection}>
        <div className={styles.sidebarLabel}>模型</div>
        <Select
          value={selectedModel || undefined}
          onChange={setSelectedModel}
          placeholder={chatOptions.length ? '选择对话模型' : '暂无可用模型'}
          disabled={chatOptions.length === 0}
          popupMatchSelectWidth={false}
          dropdownStyle={{ minWidth: 240 }}
          options={chatOptions}
          className={styles.select}
          showSearch
          optionFilterProp="label"
        />
      </div>

      <div className={styles.sidebarSubSection}>
        <div className={styles.sidebarLabel}>本次可用工具</div>
        <div className={styles.toolCheckboxes}>
          {toolNames.map((name) => {
            const pinned = selectedToolModels[name];
            const toolModelList = toolModelOptions(models, name, pinned);
            const hasModelOptions = toolModelList.length > 0;
            const isChecked = selectedTools.includes(name);
            return (
              <div key={name} className={styles.toolCheckboxRow}>
                <Tooltip title={descriptions.get(name)} placement="right">
                  <Checkbox
                    checked={isChecked}
                    onChange={(e) => {
                      if (e.target.checked) {
                        setSelectedTools((prev) => [...prev, name]);
                      } else {
                        setSelectedTools((prev) => prev.filter((item) => item !== name));
                      }
                    }}
                    className={styles.toolCheckbox}
                  >
                    {name}
                  </Checkbox>
                </Tooltip>
                {hasModelOptions && isChecked && (
                  <div className={styles.toolModelSelectWrap}>
                    <span className={styles.toolModelLabel}>模型</span>
                    <Select
                      size="small"
                      value={selectedToolModels[name] || toolModelList[0]}
                      onChange={(model) =>
                        setSelectedToolModels((prev) => ({ ...prev, [name]: model }))
                      }
                      popupMatchSelectWidth={false}
                      dropdownStyle={{ minWidth: 280 }}
                      options={toolModelList.map((m) => ({ label: m, value: m }))}
                      className={styles.toolModelSelect}
                      showSearch
                      optionFilterProp="label"
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      <Button type="default" block className={styles.overrideBtn} onClick={onOpenOverride}>
        定制配置（override）
      </Button>
    </div>
  );
}

export default FreeConfigPanel;
