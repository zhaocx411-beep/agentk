/**
 * MineAgentPanel - 我的 Agent 列表
 * 说明与创建入口在 Tab 下方；列表区域超出时滚动
 */
import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Empty, Input, Pagination, Segmented, Select } from 'antd';
import { AppstoreOutlined, BarsOutlined, PlusOutlined, SearchOutlined } from '@ant-design/icons';
import { CardGridSkeleton } from '@/components';
import type { AgentCardData, MineAgentLayout, MineStatusFilter } from '../types';
import { MINE_AGENT_PAGE_SIZE, MINE_SKELETON_COUNT, MINE_STATUS_OPTIONS } from '../constants';
import { MINE_AGENT_EMPTY, MINE_AGENT_PANEL_TEXT } from '../mock';
import { filterMineAgentCards, parseMineLayout, parseMinePage, parseMineStatus } from '../utils';
import AgentCard from './AgentCard';
import styles from '../AgentCenter.module.less';

interface MineAgentPanelProps {
  /** Agent-Loop 卡片 */
  cards: AgentCardData[];
  loading: boolean;
  onCreate: () => void;
  onOpen: (data: AgentCardData) => void;
  onPageReset?: () => void;
}

function MineAgentPanel({
  cards,
  loading,
  onCreate,
  onOpen,
  onPageReset,
}: MineAgentPanelProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [keyword, setKeyword] = useState(() => searchParams.get('kw') ?? '');
  const status = parseMineStatus(searchParams.get('status'));
  const layout = parseMineLayout(searchParams.get('layout'));

  const filtered = useMemo(
    () => filterMineAgentCards(cards, keyword, status),
    [cards, keyword, status],
  );
  const isFiltered = Boolean(keyword.trim()) || status !== 'all';

  const totalPages = Math.max(1, Math.ceil(filtered.length / MINE_AGENT_PAGE_SIZE));
  const page = Math.min(parseMinePage(searchParams.get('page')), totalPages);

  const paged = useMemo(
    () => filtered.slice((page - 1) * MINE_AGENT_PAGE_SIZE, page * MINE_AGENT_PAGE_SIZE),
    [filtered, page],
  );

  const updateParams = (patch: Record<string, string | null>) => {
    const next = new URLSearchParams(searchParams);
    Object.entries(patch).forEach(([key, value]) => {
      if (value) next.set(key, value);
      else next.delete(key);
    });
    setSearchParams(next, { replace: true });
  };

  const resetFilters = () => {
    setKeyword('');
    updateParams({ kw: null, status: null, page: null });
    onPageReset?.();
  };

  const renderEmpty = () => {
    const noneCreated = !isFiltered && cards.length === 0;
    return (
      <div className={styles.empty}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
        <h3 className={styles.emptyTitle}>
          {noneCreated ? MINE_AGENT_EMPTY.title : MINE_AGENT_EMPTY.filteredTitle}
        </h3>
        <p className={styles.emptyDesc}>
          {noneCreated ? MINE_AGENT_EMPTY.desc : MINE_AGENT_EMPTY.filteredDesc}
        </p>
        {noneCreated ? (
          <Button type="primary" icon={<PlusOutlined />} onClick={onCreate}>
            {MINE_AGENT_EMPTY.action}
          </Button>
        ) : (
          <Button onClick={resetFilters}>{MINE_AGENT_EMPTY.filteredAction}</Button>
        )}
      </div>
    );
  };

  const renderList = () => {
    if (loading && cards.length === 0) {
      return (
        <CardGridSkeleton className={styles.grid} variant="info" count={MINE_SKELETON_COUNT} />
      );
    }
    if (filtered.length === 0) {
      return renderEmpty();
    }
    return (
      <div className={layout === 'grid' ? styles.grid : styles.list}>
        {paged.map((c) => (
          <AgentCard key={c.key} data={c} onClick={onOpen} />
        ))}
      </div>
    );
  };

  return (
    <div className={styles.panel}>
      <div className={styles.header}>
        <p className={styles.subtitle}>{MINE_AGENT_PANEL_TEXT.subtitle}</p>
        <Button type="primary" icon={<PlusOutlined />} onClick={onCreate}>
          {MINE_AGENT_PANEL_TEXT.createAction}
        </Button>
      </div>

      <div className={styles.toolbar}>
        <Input
          allowClear
          prefix={<SearchOutlined className={styles.searchIcon} />}
          placeholder="请输入名称或描述"
          className={styles.searchInput}
          value={keyword}
          onChange={(e) => {
            setKeyword(e.target.value);
            updateParams({ kw: e.target.value, page: null });
            onPageReset?.();
          }}
        />
        <Select<MineStatusFilter>
          value={status}
          className={styles.statusSelect}
          onChange={(v) => {
            updateParams({ status: v === 'all' ? null : v, page: null });
            onPageReset?.();
          }}
          options={MINE_STATUS_OPTIONS}
        />
        <div className={styles.toolbarRight}>
          <Segmented
            value={layout}
            onChange={(v) => updateParams({ layout: v === 'grid' ? null : (v as MineAgentLayout) })}
            options={[
              { value: 'grid', icon: <AppstoreOutlined /> },
              { value: 'list', icon: <BarsOutlined /> },
            ]}
          />
        </div>
      </div>

      {renderList()}

      {filtered.length > 0 && (
        <div className={styles.pagination}>
          <Pagination
            current={page}
            pageSize={MINE_AGENT_PAGE_SIZE}
            total={filtered.length}
            showTotal={(total) => `共 ${total} 条`}
            onChange={(next) => {
              updateParams({ page: next > 1 ? String(next) : null });
              onPageReset?.();
            }}
            showSizeChanger={false}
          />
        </div>
      )}
    </div>
  );
}

export default MineAgentPanel;
