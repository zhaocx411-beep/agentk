import { useEffect, useState } from 'react';
import { Button, Input, Popover, Select } from 'antd';
import { FilterOutlined } from '@ant-design/icons';
import type { WorksMediaType, WorksSource } from '../types';
import styles from '../WorksGallery.module.less';

export interface WorkFilters {
  mediaType?: WorksMediaType;
  sourceKind?: string;
  source?: WorksSource;
  userId?: string;
}

interface AgentNameFilterProps {
  value?: string;
  options: string[];
  loading?: boolean;
  onChange: (agentName: string) => void;
}

interface WorkFiltersPopoverProps {
  value: WorkFilters;
  onChange: (value: WorkFilters) => void;
  showAuthor?: boolean;
}

/** 必选作用域：不可清空，由页面保证始终有值 */
export function AgentNameFilter({
  value,
  options,
  loading = false,
  onChange,
}: AgentNameFilterProps) {
  const hasOptions = options.length > 0;

  return (
    <label className={`${styles.toolbarField} ${styles.agentField}`}>
      <span className={styles.toolbarLabel}>
        Agent 名称<span className={styles.requiredMark}>*</span>
      </span>
      <Select
        showSearch
        loading={loading}
        disabled={!loading && !hasOptions}
        placeholder={hasOptions ? '请选择 Agent' : '暂无 Agent'}
        aria-label="Agent 名称"
        aria-required={true}
        value={value}
        options={options.map((name) => ({ label: name, value: name }))}
        filterOption={(input, option) =>
          String(option?.label ?? '')
            .toLowerCase()
            .includes(input.toLowerCase())
        }
        onChange={(agentName) => {
          if (agentName) onChange(agentName);
        }}
        className={styles.agentSelect}
      />
    </label>
  );
}

export function WorkFiltersPopover({
  value,
  onChange,
  showAuthor = true,
}: WorkFiltersPopoverProps) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<WorkFilters>(value);
  const activeCount = Object.values(value).filter(Boolean).length;

  useEffect(() => {
    if (open) setDraft(value);
  }, [open, value]);

  const applyFilters = () => {
    onChange({
      ...draft,
      userId: draft.userId?.trim() || undefined,
    });
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      trigger="click"
      placement="bottomRight"
      arrow={false}
      content={
        <div className={styles.filterPanel}>
          <div className={styles.filterPanelHeader}>
            <span className={styles.filterPanelTitle}>更多筛选</span>
            <Button
              type="link"
              size="small"
              disabled={Object.values(draft).every((item) => !item)}
              onClick={() => setDraft({})}
            >
              清空
            </Button>
          </div>
          <div className={styles.filterFields}>
            <label className={styles.filterField}>
              <span>媒体类型</span>
              <Select
                allowClear
                placeholder="全部类型"
                value={draft.mediaType}
                options={[
                  { label: '图片', value: 'image' },
                  { label: '视频', value: 'video' },
                ]}
                onChange={(mediaType) => setDraft((current) => ({ ...current, mediaType }))}
              />
            </label>
            <label className={styles.filterField}>
              <span>来源类型</span>
              <Select
                allowClear
                placeholder="全部来源"
                value={draft.sourceKind}
                options={[
                  { label: '自主规划', value: 'agent_loop' },
                  { label: '工作流', value: 'workflow' },
                ]}
                onChange={(sourceKind) => setDraft((current) => ({ ...current, sourceKind }))}
              />
            </label>
            <label className={styles.filterField}>
              <span>运行来源</span>
              <Select
                allowClear
                placeholder="全部来源"
                value={draft.source}
                options={[
                  { label: '在线问答', value: 'online' },
                  { label: '离线批量跑数', value: 'offline' },
                ]}
                onChange={(source) => setDraft((current) => ({ ...current, source }))}
              />
            </label>
            {showAuthor && (
              <label className={styles.filterField}>
                <span>创作者</span>
                <Input
                  allowClear
                  placeholder="输入用户 ID"
                  value={draft.userId}
                  onChange={(event) =>
                    setDraft((current) => ({ ...current, userId: event.target.value }))
                  }
                  onPressEnter={applyFilters}
                />
              </label>
            )}
          </div>
          <div className={styles.filterPanelFooter}>
            <Button onClick={() => setOpen(false)}>取消</Button>
            <Button type="primary" onClick={applyFilters}>
              应用筛选
            </Button>
          </div>
        </div>
      }
    >
      <Button
        icon={<FilterOutlined />}
        className={`${styles.filterBtn} ${activeCount ? styles.filterBtnActive : ''}`}
      >
        更多筛选{activeCount ? ` · ${activeCount}` : ''}
      </Button>
    </Popover>
  );
}
