import { useMemo, useState } from 'react';
import { Alert, Button, Empty, Input, Pagination, Skeleton, Tag, Tooltip } from 'antd';
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { RiGalleryLine } from 'react-icons/ri';
import {
  myPortfolioQueryKey,
  portfolioQueryKey,
  queryMyPortfolio,
  queryPortfolio,
} from './api';
import type { PortfolioQueryParams } from './types';
import { WorkCard } from './components/WorkCard';
import {
  AgentNameFilter,
  WorkFiltersPopover,
  type WorkFilters,
} from './components/WorkFiltersPopover';
import WorkDetailModal from './components/WorkDetailModal';
import { useRequiredAgentName } from './useRequiredAgentName';
import styles from './WorksGallery.module.less';

const PAGE_SIZE = 20;

interface WorksGalleryProps {
  scope?: 'team' | 'mine';
}

/** 作品展示页面：按创作轮次检索并展示正式作品数据 */
function WorksGallery({ scope = 'team' }: WorksGalleryProps) {
  const isMine = scope === 'mine';
  const [inputValue, setInputValue] = useState('');
  const [searchKeyword, setSearchKeyword] = useState('');
  const [filters, setFilters] = useState<WorkFilters>({});
  const [page, setPage] = useState(1);
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null);
  const { agentNames, agentNamesLoading, agentName, agentReady, selectAgent } =
    useRequiredAgentName(isMine);

  const queryParams = useMemo<PortfolioQueryParams>(
    () => ({
      promptKeyword: searchKeyword.trim() || undefined,
      assetType: filters.mediaType,
      sourceKind: filters.sourceKind,
      source: filters.source,
      agentName,
      userId: isMine ? undefined : filters.userId,
      page,
      pageSize: PAGE_SIZE,
    }),
    [agentName, filters, isMine, page, searchKeyword],
  );

  const { data, isLoading, isFetching, isError, refetch } = useQuery({
    queryKey: isMine ? myPortfolioQueryKey(queryParams) : portfolioQueryKey(queryParams),
    queryFn: () => (isMine ? queryMyPortfolio(queryParams) : queryPortfolio(queryParams)),
    enabled: agentReady,
    placeholderData: (previous) => previous,
  });

  const list = data?.records ?? [];
  const total = data?.total ?? 0;
  const selectedRound = selectedIndex !== null ? (list[selectedIndex] ?? null) : null;
  const showSkeleton = !agentReady ? agentNamesLoading || agentNames.length > 0 : isLoading;
  const emptyDescription =
    agentNames.length === 0
      ? '还没有可展示的作品，完成创作后会按 Agent 出现在这里'
      : '该 Agent 下暂无符合条件的创作作品';

  const handleSearch = () => {
    setSearchKeyword(inputValue);
    setSelectedIndex(null);
    setPage(1);
  };

  const handleFiltersChange = (nextFilters: WorkFilters) => {
    setFilters(nextFilters);
    setSelectedIndex(null);
    setPage(1);
  };

  const handleReset = () => {
    setInputValue('');
    setSearchKeyword('');
    setFilters({});
    setSelectedIndex(null);
    setPage(1);
  };

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div className={styles.titleBlock}>
          <h1 className={styles.title}>{isMine ? '我的作品集' : '团队作品展示'}</h1>
          <div className={styles.subtitle}>
            <RiGalleryLine className={styles.subtitleIcon} />
            {isMine ? '查看我生成过的全部创意作品' : '浏览、检索并复用团队生成的创意作品'}
          </div>
        </div>
        <Tag className={styles.totalTag}>{total} 组创作</Tag>
      </div>

      <div className={styles.toolbar}>
        <label className={`${styles.toolbarField} ${styles.searchField}`}>
          <span className={styles.toolbarLabel}>创作指令</span>
          <Input
            className={styles.searchInput}
            placeholder="输入关键词"
            allowClear
            value={inputValue}
            onChange={(event) => setInputValue(event.target.value)}
            onPressEnter={handleSearch}
          />
        </label>
        <AgentNameFilter
          value={agentName}
          options={agentNames}
          loading={agentNamesLoading}
          onChange={(nextAgent) => {
            selectAgent(nextAgent);
            setSelectedIndex(null);
            setPage(1);
          }}
        />
        <Button
          type="primary"
          icon={<SearchOutlined />}
          className={styles.searchBtn}
          loading={isFetching && !isLoading}
          onClick={handleSearch}
        >
          搜索
        </Button>
        <WorkFiltersPopover value={filters} onChange={handleFiltersChange} showAuthor={!isMine} />
        <Tooltip title="重置搜索与更多筛选">
          <Button icon={<ReloadOutlined />} onClick={handleReset} className={styles.filterBtn} />
        </Tooltip>
      </div>

      {isError && (
        <Alert
          type="error"
          showIcon
          message="作品加载失败"
          action={<Button onClick={() => refetch()}>重新加载</Button>}
          className={styles.errorAlert}
        />
      )}

      <div className={styles.gridWrapper}>
        <Pagination
          className={styles.pagination}
          current={page}
          pageSize={PAGE_SIZE}
          total={total}
          onChange={(nextPage) => {
            setSelectedIndex(null);
            setPage(nextPage);
          }}
          showSizeChanger={false}
        />

        {showSkeleton ? (
          <div className={styles.grid}>
            {Array.from({ length: 10 }, (_, index) => (
              <Skeleton.Node active className={styles.cardSkeleton} key={index} />
            ))}
          </div>
        ) : list.length > 0 ? (
          <div className={styles.grid} aria-busy={isFetching}>
            {list.map((item, index) => (
              <WorkCard key={item.roundKey} item={item} onClick={() => setSelectedIndex(index)} />
            ))}
          </div>
        ) : (
          !isError && (
            <div className={styles.empty}>
              <Empty description={emptyDescription} />
            </div>
          )
        )}

        {total > PAGE_SIZE && (
          <Pagination
            className={styles.pagination}
            current={page}
            pageSize={PAGE_SIZE}
            total={total}
            onChange={(nextPage) => {
              setSelectedIndex(null);
              setPage(nextPage);
            }}
            showSizeChanger={false}
          />
        )}
      </div>

      <WorkDetailModal
        key={selectedRound?.roundKey ?? 'closed'}
        round={selectedRound}
        hasPrev={selectedIndex !== null && selectedIndex > 0}
        hasNext={selectedIndex !== null && selectedIndex < list.length - 1}
        onPrev={() =>
          setSelectedIndex((index) => (index !== null && index > 0 ? index - 1 : index))
        }
        onNext={() =>
          setSelectedIndex((index) =>
            index !== null && index < list.length - 1 ? index + 1 : index,
          )
        }
        onClose={() => setSelectedIndex(null)}
      />
    </div>
  );
}

export default WorksGallery;
