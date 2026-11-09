/**
 * 编辑已添加工具的默认参数（不经过「添加工具」列表）
 * 参数声明（必填/可选/枚举）来自 GET .../official/tools
 */
import { useEffect, useMemo, useRef } from 'react';
import { Button, Empty, Modal, Spin } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { OFFICIAL_TOOLS_QUERY_KEY, getOfficialTools } from '@/services/agentLoop';
import { spacing } from '@/styles/design-tokens';
import { ToolDefaultsForm } from './ToolDefaultsForm';
import type { ToolDefaultsMap } from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

interface EditToolDefaultsModalProps {
  toolName: string | null;
  toolDefaults: ToolDefaultsMap;
  onClose: () => void;
  onSave: (toolName: string, defaults: Record<string, unknown>) => void;
}

function EditToolDefaultsModal({
  toolName,
  toolDefaults,
  onClose,
  onSave,
}: EditToolDefaultsModalProps) {
  const defaultsRef = useRef<Record<string, unknown>>({});
  const open = toolName != null;

  const { data, isLoading, isError } = useQuery({
    queryKey: OFFICIAL_TOOLS_QUERY_KEY,
    queryFn: getOfficialTools,
    enabled: open,
    staleTime: 5 * 60 * 1000,
  });

  const tool = useMemo(
    () => (toolName ? (data?.tools ?? []).find((item) => item.name === toolName) : undefined),
    [data?.tools, toolName],
  );

  useEffect(() => {
    defaultsRef.current = {};
  }, [toolName]);

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title="编辑默认参数"
      width={spacing.section * 10}
      className={styles.compactModal}
      destroyOnHidden
      footer={
        <>
          <Button onClick={onClose}>取消</Button>
          <Button
            type="primary"
            disabled={!tool}
            onClick={() => {
              if (!toolName || !tool) return;
              onSave(toolName, defaultsRef.current);
              onClose();
            }}
          >
            保存参数
          </Button>
        </>
      }
    >
      {isLoading ? (
        <div className={styles.modelEmpty}>
          <Spin size="small" />
          <span>加载官方参数配置…</span>
        </div>
      ) : null}
      {isError ? <div className={styles.modelEmpty}>官方工具配置加载失败，请稍后重试</div> : null}
      {!isLoading && !isError && !tool ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="官方目录中未找到该工具，暂无法编辑参数"
        />
      ) : null}
      {tool ? (
        <ToolDefaultsForm
          tool={tool}
          existingDefaults={toolDefaults[tool.name]}
          valuesRef={defaultsRef}
        />
      ) : null}
    </Modal>
  );
}

export { EditToolDefaultsModal };
