/**
 * ModelSelectSection - 对话模型选择
 * - 触发条：回显当前 defaultModel（来自 profile）
 * - 下拉列表：选项与说明来自 GET /agentk/models
 */
import { useMemo, useState } from 'react';
import { Dropdown, Input, Spin } from 'antd';
import { DownOutlined, SearchOutlined } from '@ant-design/icons';
import type { CatalogModel } from '@/pages/AgentPlayground/types';
import { ModelSelectCard } from './ModelSelectCard';
import styles from '../AgentDetail.module.less';

interface ModelSelectSectionProps {
  /** 接口下发的可选模型 */
  models: CatalogModel[];
  /** 当前选中值（profile 回显） */
  value: string;
  loading?: boolean;
  onChange: (model: string) => void;
}

function ModelSelectSection({ models, value, loading = false, onChange }: ModelSelectSectionProps) {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState('');

  const activeModel = useMemo(
    () => models.find((m) => m.name === value) ?? (value ? { name: value, kind: 'chat' as const } : null),
    [models, value],
  );

  const filtered = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    if (!q) return models;
    return models.filter(
      (m) =>
        m.name.toLowerCase().includes(q) ||
        (m.description?.toLowerCase().includes(q) ?? false),
    );
  }, [keyword, models]);

  const handleSelect = (name: string) => {
    onChange(name);
    setOpen(false);
    setKeyword('');
  };

  const panel = (
    <div className={styles.modelDropdown} onClick={(e) => e.stopPropagation()}>
      <div className={styles.modelDropdownHead}>
        <Input
          allowClear
          size="small"
          placeholder="搜索模型名称或说明"
          prefix={<SearchOutlined className={styles.modelSearchIcon} />}
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          className={styles.modelDropdownSearch}
          disabled={loading}
        />
      </div>
      <div className={styles.modelDropdownBody}>
        {loading ? (
          <div className={styles.modelEmpty}>
            <Spin size="small" />
            <span>加载模型列表…</span>
          </div>
        ) : null}
        {!loading && filtered.length > 0 ? (
          <>
            <div className={styles.modelDropdownGroup}>可选模型</div>
            {filtered.map((m) => (
              <ModelSelectCard
                key={m.name}
                model={m}
                active={m.name === value}
                onSelect={() => handleSelect(m.name)}
              />
            ))}
          </>
        ) : null}
        {!loading && filtered.length === 0 ? (
          <div className={styles.modelEmpty}>
            {models.length === 0 ? '暂无可用模型，请稍后重试' : '未找到匹配的模型，试试其他关键词'}
          </div>
        ) : null}
      </div>
    </div>
  );

  return (
    <Dropdown
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setKeyword('');
      }}
      trigger={['click']}
      popupRender={() => panel}
      placement="bottomLeft"
    >
      <button
        type="button"
        className={`${styles.modelTrigger} ${value ? styles.modelTriggerFilled : ''} ${open ? styles.modelTriggerOpen : ''}`}
        aria-expanded={open}
      >
        {value ? (
          <div className={styles.modelSummaryBody}>
            <div className={styles.modelSummaryLine}>
              <span className={styles.modelSummaryName}>{value}</span>
            </div>
            {activeModel?.description ? (
              <p className={styles.modelSummaryDesc}>{activeModel.description}</p>
            ) : null}
          </div>
        ) : (
          <span className={styles.modelSummaryEmpty}>
            {loading ? '加载模型中…' : '点击选择对话模型'}
          </span>
        )}
        <DownOutlined className={styles.modelTriggerArrow} />
      </button>
    </Dropdown>
  );
}

export { ModelSelectSection };
