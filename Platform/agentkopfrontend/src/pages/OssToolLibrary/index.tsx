import { useMemo, useState } from 'react';
import { Alert, Button, Empty } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { CardGridSkeleton } from '@/components';
import { matchKeyword, normalizeKeyword } from '@/utils/text';
import { OSS_TOOLS_QUERY_KEY, getOssTools } from './api';
import { OssToolCard } from './components/OssToolCard';
import { OssToolDetailDrawer } from './components/OssToolDetailDrawer';
import { OssToolFilters } from './components/OssToolFilters';
import type { OssTool, OssToolFilters as Filters } from './types';
import styles from './OssToolLibrary.module.less';

const EMPTY_FILTERS: Filters = { keyword: '' };

function OssToolLibrary() {
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS);
  const [selectedTool, setSelectedTool] = useState<OssTool>();
  const {
    data = [],
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: OSS_TOOLS_QUERY_KEY,
    queryFn: getOssTools,
  });

  const options = useMemo(
    () => ({
      categories: [...new Set(data.map((tool) => tool.category))].sort(),
      forms: [...new Set(data.map((tool) => tool.form))].sort(),
      licenses: [...new Set(data.map((tool) => tool.license))].sort(),
    }),
    [data],
  );

  const visibleTools = useMemo(() => {
    const keyword = normalizeKeyword(filters.keyword);
    return data.filter(
      (tool) =>
        (!filters.category || tool.category === filters.category) &&
        (!filters.form || tool.form === filters.form) &&
        (!filters.license || tool.license === filters.license) &&
        matchKeyword(
          [tool.name, tool.summary, tool.capabilities, tool.category, tool.form, ...tool.tags],
          keyword,
        ),
    );
  }, [data, filters]);

  const resetFilters = () => setFilters(EMPTY_FILTERS);

  return (
    <main className={styles.page}>
      <div className={styles.container}>
        <header className={styles.hero}>
          <p className={styles.contextLabel}>开源生态 · 官方 Agent 能力前瞻</p>
          <h1 className={styles.title}>开源能力灵感库</h1>
          <p className={styles.description}>
            探索视频、音频与内容创作领域的优秀开源项目，了解它们能做什么、如何使用。我们将持续吸收其中的能力与实践，并逐步应用到官方
            Agent 中；你也可以先从这里寻找灵感。
          </p>
        </header>

        <Alert
          className={styles.notice}
          type="info"
          message="部分能力正在规划接入官方 Agent"
          description="当前项目由外部社区维护，使用前请查看项目文档、运行环境与许可证。我们会持续评估，并将合适的能力逐步应用到官方 Agent 中。"
        />

        <div className={styles.filterPanel}>
          <OssToolFilters
            filters={filters}
            categories={options.categories}
            forms={options.forms}
            licenses={options.licenses}
            onChange={setFilters}
            onReset={resetFilters}
          />
          <div className={styles.resultMeta} aria-live="polite">
            {isLoading ? '正在加载工具…' : `找到 ${visibleTools.length} 个工具`}
          </div>
        </div>

        {isLoading ? (
          <CardGridSkeleton className={styles.grid} variant="compact" count={8} />
        ) : null}

        {isError ? (
          <div className={styles.stateFrame}>
            <Alert type="error" showIcon message="工具目录加载失败" />
            <Button onClick={() => refetch()}>重新加载</Button>
          </div>
        ) : null}

        {!isLoading && !isError && visibleTools.length > 0 ? (
          <section className={styles.grid} aria-label="开源能力灵感列表">
            {visibleTools.map((tool) => (
              <OssToolCard key={tool.id} tool={tool} onSelect={setSelectedTool} />
            ))}
          </section>
        ) : null}

        {!isLoading && !isError && visibleTools.length === 0 ? (
          <div className={styles.stateFrame}>
            <Empty description="没有符合当前条件的工具" />
            <Button onClick={resetFilters}>清除筛选</Button>
          </div>
        ) : null}
      </div>

      <OssToolDetailDrawer
        tool={selectedTool}
        open={Boolean(selectedTool)}
        onClose={() => setSelectedTool(undefined)}
      />
    </main>
  );
}

export default OssToolLibrary;
