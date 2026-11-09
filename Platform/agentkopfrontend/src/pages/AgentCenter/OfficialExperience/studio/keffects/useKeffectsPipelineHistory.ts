/**
 * keffects 工作台：pipeline-execs 分页历史
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  PIPELINE_EXECS_PAGE_SIZE,
  queryPipelineExecsPage,
  type PipelineExecListRecord,
} from '@/services/pipelineExecs';
import { execRecordKey, pipelineExecsToRecords } from './pipelineExecHistory';
import { execToTaskId } from './keffectsTaskRuntime';
import type { GenerationRecord } from '../lib/types';

export function recordMatchesTaskId(record: GenerationRecord, taskId: string): boolean {
  return record.id === taskId || record.runId === taskId;
}

export function findExecByKey(
  execs: PipelineExecListRecord[],
  id: string,
): PipelineExecListRecord | null {
  return (
    execs.find((exec) => execRecordKey(exec) === id || execToTaskId(exec) === id) ?? null
  );
}

interface UseKeffectsPipelineHistoryOptions {
  pipelineName?: string;
  enabled?: boolean;
}

export function useKeffectsPipelineHistory({
  pipelineName,
  enabled = true,
}: UseKeffectsPipelineHistoryOptions) {
  const [execs, setExecs] = useState<PipelineExecListRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const historyListRef = useRef<HTMLDivElement>(null);
  const pageRef = useRef(0);
  const loadedKeyRef = useRef<string | undefined>(undefined);

  const mergeExecs = useCallback((prev: PipelineExecListRecord[], incoming: PipelineExecListRecord[]) => {
    if (!incoming.length) return prev;
    const ids = new Set(prev.map((item) => item.publicId || String(item.id)));
    const appended = incoming.filter((item) => !ids.has(item.publicId || String(item.id)));
    return appended.length ? [...prev, ...appended] : prev;
  }, []);

  const fetchPage = useCallback(
    (page: number) => {
      const name = pipelineName?.trim();
      if (!name) {
        return Promise.resolve({
          records: [],
          page: 1,
          pageSize: PIPELINE_EXECS_PAGE_SIZE,
          total: 0,
          hasMore: false,
        });
      }
      return queryPipelineExecsPage({ pipelineName: name, page });
    },
    [pipelineName],
  );

  const loadFirstPage = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetchPage(1);
      setExecs(res.records);
      pageRef.current = res.page;
      setHasMore(res.hasMore);
    } catch {
      setExecs([]);
      pageRef.current = 0;
      setHasMore(false);
    } finally {
      setLoading(false);
    }
  }, [fetchPage]);

  useEffect(() => {
    if (!enabled) return;
    const key = pipelineName?.trim() || '';
    if (!key) {
      setExecs([]);
      setHasMore(false);
      return;
    }
    if (loadedKeyRef.current === key) return;
    loadedKeyRef.current = key;
    setSelectedId(null);
    void loadFirstPage();
  }, [enabled, pipelineName, loadFirstPage]);

  const refresh = useCallback(() => {
    loadedKeyRef.current = undefined;
    void loadFirstPage();
  }, [loadFirstPage]);

  /** 重新生成后：刷新列表并选中最新一条（按 taskId / agentkExecId 匹配） */
  const focusTask = useCallback(
    async (taskId: string) => {
      setSelectedId(taskId);
      const name = pipelineName?.trim();
      if (!name) return;
      try {
        const res = await fetchPage(1);
        setExecs(res.records);
        pageRef.current = res.page;
        setHasMore(res.hasMore);
        loadedKeyRef.current = name;
        const match = findExecByKey(res.records, taskId);
        setSelectedId(match ? execRecordKey(match) : taskId);
        historyListRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
      } catch {
        setSelectedId(taskId);
      }
    },
    [fetchPage, pipelineName],
  );

  const handleHistoryScroll = useCallback(() => {
    const el = historyListRef.current;
    if (!el || isLoadingMore || !hasMore || loading) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight >= 20) return;

    setIsLoadingMore(true);
    fetchPage(pageRef.current + 1)
      .then((res) => {
        if (res.records.length) {
          setExecs((prev) => mergeExecs(prev, res.records));
          pageRef.current = res.page;
        }
        setHasMore(res.hasMore && res.records.length > 0);
      })
      .catch(() => undefined)
      .finally(() => setIsLoadingMore(false));
  }, [fetchPage, hasMore, isLoadingMore, loading, mergeExecs]);

  const records: GenerationRecord[] = useMemo(() => pipelineExecsToRecords(execs), [execs]);

  const findExec = useCallback(
    (id: string) => findExecByKey(execs, id),
    [execs],
  );

  const selectRecord = useCallback((id: string) => {
    setSelectedId(id);
  }, []);

  return {
    execs,
    records,
    selectedId,
    selectRecord,
    findExec,
    loading,
    hasMore,
    isLoadingMore,
    historyListRef,
    handleHistoryScroll,
    refresh,
    focusTask,
  };
}
