/**
 * ToolSelectSection - 已选工具可直接点卡片改参数；「+」打开官方工具广场
 */
import { useMemo, useState } from 'react';
import { Button } from 'antd';
import { PlusOutlined, ToolOutlined } from '@ant-design/icons';
import type { OfficialToolItem } from '@/types/officialTools';
import { AddToolModal } from './AddToolModal';
import { EditToolDefaultsModal } from './EditToolDefaultsModal';
import { SelectedToolCard } from './SelectedToolCard';
import type { ToolDefaultsMap } from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

interface ToolSelectSectionProps {
  tools: string[];
  toolDefaults: ToolDefaultsMap;
  officialTools: OfficialToolItem[];
  onChange: (tools: string[], toolDefaults: ToolDefaultsMap) => void;
}

function ToolSelectSection({
  tools,
  toolDefaults,
  officialTools,
  onChange,
}: ToolSelectSectionProps) {
  const [addOpen, setAddOpen] = useState(false);
  const [editToolName, setEditToolName] = useState<string | null>(null);

  const toolByName = useMemo(() => {
    const map = new Map<string, OfficialToolItem>();
    for (const tool of officialTools) {
      map.set(tool.name, tool);
    }
    return map;
  }, [officialTools]);

  return (
    <div className={styles.toolSection}>
      <div className={styles.toolSectionHead}>
        <div className={styles.toolSectionLabel}>
          工具
          <span className={styles.fieldHint}>已启用 {tools.length} 个</span>
        </div>
        <Button
          type="text"
          size="small"
          icon={<PlusOutlined />}
          onClick={() => setAddOpen(true)}
          className={styles.toolAddBtn}
        >
          添加
        </Button>
      </div>
      <p className={styles.fieldTip}>
        点击工具或“编辑参数”可调整默认值；必填与可选参数来自官方接口
      </p>

      {tools.length > 0 ? (
        <div className={styles.toolGrid}>
          {tools.map((name) => {
            const info = toolByName.get(name);
            return (
              <SelectedToolCard
                key={name}
                name={name}
                description={info?.description}
                kind={info?.kind}
                paramCount={info?.parameters?.length ?? 0}
                onConfigure={() => setEditToolName(name)}
                onRemove={() => {
                  const nextDefaults = { ...toolDefaults };
                  delete nextDefaults[name];
                  onChange(
                    tools.filter((t) => t !== name),
                    nextDefaults,
                  );
                }}
              />
            );
          })}
        </div>
      ) : (
        <button type="button" className={styles.toolEmpty} onClick={() => setAddOpen(true)}>
          <ToolOutlined className={styles.toolEmptyIcon} />
          <span>尚未添加工具</span>
          <span className={styles.toolEmptyHint}>点击此处或右上角「添加」打开官方工具广场</span>
        </button>
      )}

      <AddToolModal
        open={addOpen}
        selected={tools}
        toolDefaults={toolDefaults}
        onClose={() => setAddOpen(false)}
        onChange={onChange}
      />

      <EditToolDefaultsModal
        toolName={editToolName}
        toolDefaults={toolDefaults}
        onClose={() => setEditToolName(null)}
        onSave={(name, defaults) => {
          const nextDefaults: ToolDefaultsMap = { ...toolDefaults };
          if (Object.keys(defaults).length > 0) {
            nextDefaults[name] = defaults;
          } else {
            delete nextDefaults[name];
          }
          onChange(tools, nextDefaults);
        }}
      />
    </div>
  );
}

export { ToolSelectSection };
