/**
 * ToolGallery - 工具广场
 * 推荐按分类展示全部工具；点击卡片进入工具详情页
 */
import { useMemo, useRef, useState } from 'react';
import { Empty, Input } from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { CardGridSkeleton } from '@/components';
import { OFFICIAL_TOOLS_QUERY_KEY, getOfficialTools } from './api';
import { TOOL_SKELETON_COUNT } from './constants';
import ToolAllPanel from './components/ToolAllPanel';
import ToolSection from './components/ToolSection';
import type { OfficialToolCardData, ToolGalleryView } from './types';
import { TOOL_GALLERY_TEXT } from './mock';
import {
  buildToolDetailPath,
  filterToolCards,
  groupToolsByKind,
  parseToolGalleryView,
  toToolCard,
} from './utils';
import styles from './ToolGallery.module.less';

function ToolGallery() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const pageRef = useRef<HTMLDivElement>(null);
  const [keyword, setKeyword] = useState(() => searchParams.get('q') ?? '');
  const view = parseToolGalleryView(searchParams.get('view'));
  const kind = searchParams.get('kind') || searchParams.get('category') || undefined;

  const { data, isLoading } = useQuery({
    queryKey: OFFICIAL_TOOLS_QUERY_KEY,
    queryFn: getOfficialTools,
  });

  const cards = useMemo(() => (data?.tools ?? []).map(toToolCard), [data?.tools]);
  const matched = useMemo(() => filterToolCards(cards, keyword), [cards, keyword]);
  const groups = useMemo(() => groupToolsByKind(matched), [matched]);
  const allItems = kind ? matched.filter((item) => item.kind === kind) : matched;
  const isEmpty = !isLoading && cards.length === 0;
  const isSearchEmpty = !isLoading && cards.length > 0 && matched.length === 0;

  const scrollPageTop = () => {
    pageRef.current?.scrollTo({ top: 0 });
  };

  const patchParams = (patch: Record<string, string | null>) => {
    const next = new URLSearchParams(searchParams);
    Object.entries(patch).forEach(([key, value]) => {
      if (value) next.set(key, value);
      else next.delete(key);
    });
    setSearchParams(next, { replace: true });
  };

  const updateView = (next: ToolGalleryView, nextKind?: string) => {
    patchParams({
      view: next === 'all' ? 'all' : null,
      kind: next === 'all' && nextKind ? nextKind : null,
      category: null,
    });
    scrollPageTop();
  };

  const handleSearch = (value: string) => {
    setKeyword(value);
    patchParams({ q: value.trim() || null });
  };

  const handleOpenTool = (item: OfficialToolCardData) => {
    navigate(buildToolDetailPath(item.name));
  };

  const renderContent = () => {
    if (isLoading) {
      return (
        <CardGridSkeleton className={styles.grid} variant="compact" count={TOOL_SKELETON_COUNT} />
      );
    }
    if (isEmpty) {
      return (
        <div className={styles.empty}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
          <h3 className={styles.emptyTitle}>{TOOL_GALLERY_TEXT.emptyTitle}</h3>
          <p className={styles.emptyDesc}>{TOOL_GALLERY_TEXT.emptyDesc}</p>
        </div>
      );
    }
    if (isSearchEmpty) {
      return (
        <div className={styles.empty}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
          <h3 className={styles.emptyTitle}>{TOOL_GALLERY_TEXT.emptySearchTitle}</h3>
          <p className={styles.emptyDesc}>{TOOL_GALLERY_TEXT.emptySearchDesc}</p>
        </div>
      );
    }
    if (view === 'all') {
      return (
        <ToolAllPanel
          items={allItems}
          kind={kind}
          onOpen={handleOpenTool}
          onBack={() => updateView('recommend')}
        />
      );
    }
    return groups.map((group) => (
      <ToolSection
        key={group.kind}
        title={group.kind}
        items={group.items}
        onOpen={handleOpenTool}
      />
    ));
  };

  return (
    <div className={styles.page} ref={pageRef} data-catalog-scroll>
      <section className={styles.gallery}>
        <header className={styles.hero}>
          <h2 className={styles.heroTitle}>
            {TOOL_GALLERY_TEXT.heroTitle.lead}
            <em className={styles.heroAccent}>{TOOL_GALLERY_TEXT.heroTitle.accent}</em>
            {TOOL_GALLERY_TEXT.heroTitle.tail}
          </h2>
          <p className={styles.heroDesc}>{TOOL_GALLERY_TEXT.heroDesc}</p>
          <Input
            allowClear
            prefix={<SearchOutlined className={styles.searchIcon} />}
            placeholder={TOOL_GALLERY_TEXT.searchPlaceholder}
            className={styles.search}
            value={keyword}
            onChange={(event) => handleSearch(event.target.value)}
            aria-label={TOOL_GALLERY_TEXT.searchPlaceholder}
          />
        </header>

        {renderContent()}
      </section>
    </div>
  );
}

export default ToolGallery;
