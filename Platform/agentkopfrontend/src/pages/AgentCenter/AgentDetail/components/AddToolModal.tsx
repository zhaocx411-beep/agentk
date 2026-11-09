/**
 * AddToolModal - 从官方工具广场选工具
 * 点「添加」后填写默认参数，确认后写入 tools + toolDefaults
 */
import { useMemo, useRef, useState } from 'react';
import { Button, Empty, Input, Modal, Select, Spin, Tooltip } from 'antd';
import {
  ArrowLeftOutlined,
  CheckOutlined,
  EditOutlined,
  PlusOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { OFFICIAL_TOOLS_QUERY_KEY, getOfficialTools } from '@/services/agentLoop';
import { spacing } from '@/styles/design-tokens';
import { matchToolName, uniqueByName } from '@/utils/text';
import type { OfficialToolItem } from '@/types/officialTools';
import { ACCENT_CLASS } from '../lib/accentStyles';
import { getToolMeta } from '../lib/toolMeta';
import { ToolDefaultsForm } from './ToolDefaultsForm';
import type { ToolDefaultsMap } from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

interface AddToolModalProps {
  open: boolean;
  selected: string[];
  toolDefaults: ToolDefaultsMap;
  onClose: () => void;
  onChange: (tools: string[], toolDefaults: ToolDefaultsMap) => void;
}

function AddToolModal({ open, selected, toolDefaults, onClose, onChange }: AddToolModalProps) {
  const [keyword, setKeyword] = useState('');
  const [category, setCategory] = useState<string>('all');
  const [pending, setPending] = useState<OfficialToolItem | null>(null);
  const defaultsRef = useRef<Record<string, unknown>>({});

  const { data, isLoading, isError } = useQuery({
    queryKey: OFFICIAL_TOOLS_QUERY_KEY,
    queryFn: getOfficialTools,
    enabled: open,
    staleTime: 5 * 60 * 1000,
  });

  const tools = useMemo(() => uniqueByName(data?.tools ?? []), [data?.tools]);

  const categories = useMemo(() => {
    const set = new Set<string>();
    for (const tool of tools) {
      const kind = tool.kind?.trim();
      if (kind) set.add(kind);
    }
    return Array.from(set).sort();
  }, [tools]);

  const filtered = useMemo(() => {
    return tools.filter((tool) => {
      const kind = tool.kind?.trim() || '';
      if (category !== 'all' && kind !== category) return false;
      return matchToolName(tool.name, keyword);
    });
  }, [tools, category, keyword]);

  const removeTool = (name: string) => {
    const nextTools = selected.filter((t) => t !== name);
    const nextDefaults = { ...toolDefaults };
    delete nextDefaults[name];
    onChange(nextTools, nextDefaults);
  };

  const confirmAdd = () => {
    if (!pending) return;
    // 必填参数不拦截：未填的留给 Agent 对话中补齐
    const filled = defaultsRef.current;
    const nextTools = selected.includes(pending.name) ? selected : [...selected, pending.name];
    const nextDefaults: ToolDefaultsMap = { ...toolDefaults };
    if (Object.keys(filled).length > 0) {
      nextDefaults[pending.name] = filled;
    } else {
      delete nextDefaults[pending.name];
    }
    onChange(nextTools, nextDefaults);
    setPending(null);
  };

  const resetUi = () => {
    setKeyword('');
    setCategory('all');
    setPending(null);
    defaultsRef.current = {};
  };

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={
        pending ? (
          <div className={styles.addToolTitle}>
            <button
              type="button"
              className={styles.addToolBack}
              onClick={() => setPending(null)}
              aria-label="返回工具列表"
            >
              <ArrowLeftOutlined />
            </button>
            <span>{selected.includes(pending.name) ? '编辑默认参数' : '填写默认参数'}</span>
          </div>
        ) : (
          <div className={styles.addToolTitle}>
            <span>添加工具</span>
            <span className={styles.addToolCount}>已添加 {selected.length} 个</span>
          </div>
        )
      }
      footer={
        pending ? (
          <Button type="primary" onClick={confirmAdd}>
            {selected.includes(pending.name) ? '保存参数' : '确认添加'}
          </Button>
        ) : (
          <Button type="primary" onClick={onClose}>
            完成
          </Button>
        )
      }
      width={spacing.section * 10}
      className={styles.compactModal}
      destroyOnHidden
      afterClose={resetUi}
    >
      {pending ? (
        <ToolDefaultsForm
          tool={pending}
          existingDefaults={toolDefaults[pending.name]}
          valuesRef={defaultsRef}
        />
      ) : (
        <>
          <div className={styles.addToolToolbar}>
            <Input
              allowClear
              placeholder="搜索工具名称"
              prefix={<SearchOutlined className={styles.modelSearchIcon} />}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              className={styles.addToolSearch}
            />
            <Select
              value={category}
              onChange={setCategory}
              className={styles.addToolCategory}
              options={[
                { value: 'all', label: '全部分类' },
                ...categories.map((c) => ({ value: c, label: c })),
              ]}
            />
          </div>

          <div className={styles.addToolList}>
            {isLoading ? (
              <div className={styles.modelEmpty}>
                <Spin size="small" />
                <span>加载官方工具…</span>
              </div>
            ) : null}
            {isError ? <div className={styles.modelEmpty}>工具列表加载失败，请稍后重试</div> : null}
            {!isLoading &&
              !isError &&
              filtered.map((tool, index) => {
                const meta = getToolMeta(tool.name);
                const Icon = meta.Icon;
                const added = selected.includes(tool.name);
                const kind = tool.kind?.trim() || '';
                const desc = tool.description?.trim() || '';
                const paramCount = tool.parameters?.length ?? 0;
                return (
                  <div key={`${tool.name}-${index}`} className={styles.addToolRow}>
                    <div className={`${styles.toolIcon} ${ACCENT_CLASS[meta.accent]}`}>
                      <Icon />
                    </div>
                    <div className={styles.addToolRowBody}>
                      <div className={styles.toolTitleRow}>
                        <span className={styles.toolName}>{tool.name}</span>
                        {kind ? <span className={styles.toolCategory}>{kind}</span> : null}
                        {paramCount > 0 ? (
                          <span className={styles.softTag}>{paramCount} 个参数</span>
                        ) : null}
                      </div>
                      <p className={styles.addToolRowDesc}>{desc}</p>
                    </div>
                    {added ? (
                      <div className={styles.addToolRowActions}>
                        <Tooltip title="配置默认参数">
                          <Button
                            type="text"
                            size="small"
                            icon={<EditOutlined />}
                            onClick={() => setPending(tool)}
                            aria-label={`配置工具 ${tool.name}`}
                          />
                        </Tooltip>
                        <Button
                          type="text"
                          size="small"
                          icon={<CheckOutlined />}
                          onClick={() => removeTool(tool.name)}
                        >
                          已添加
                        </Button>
                      </div>
                    ) : (
                      <Button
                        type="text"
                        size="small"
                        icon={<PlusOutlined />}
                        onClick={() => setPending(tool)}
                      >
                        添加
                      </Button>
                    )}
                  </div>
                );
              })}
            {!isLoading && !isError && filtered.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未找到匹配的工具" />
            ) : null}
          </div>
        </>
      )}
    </Modal>
  );
}

export { AddToolModal };
