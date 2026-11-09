import { queryDatasetTasks } from '@/pages/OfflineRun/api';
import type {
  DatasetTask,
  DatasetTaskQueryParams,
  DatasetTaskQueryResponse,
} from '@/pages/OfflineRun/types';
import { MATERIAL_FETCH_PAGE_SIZE, MATERIAL_PIPELINE, MATERIAL_USAGE } from './types';

export interface MaterialTasksQueryParams {
  current: number;
  pageSize: number;
  category?: string;
}

export const materialTasksQueryKey = (params: MaterialTasksQueryParams) =>
  ['material-library', 'tasks', params] as const;

/** 素材库分页查询（TEMPLATE + pipelineName=asset） */
export function queryMaterialTasksPage(
  params: MaterialTasksQueryParams,
): Promise<DatasetTaskQueryResponse> {
  const query: DatasetTaskQueryParams = {
    usage: MATERIAL_USAGE,
    pipelineName: MATERIAL_PIPELINE,
    current: params.current,
    pageSize: params.pageSize,
  };
  if (params.category) query.category = params.category;
  return queryDatasetTasks(query);
}

/** 拉齐某 category 的全部用例，供短剧剧名等二级筛选项（接口暂无 drama 参数） */
export async function queryAllMaterialTasksByCategory(category: string): Promise<DatasetTask[]> {
  const pageSize = MATERIAL_FETCH_PAGE_SIZE;
  const first = await queryMaterialTasksPage({ current: 1, pageSize, category });
  const records = [...(first.records ?? [])];
  const total = first.total ?? records.length;
  const pages = Math.max(1, first.pages ?? Math.ceil(total / pageSize));
  for (let current = 2; current <= pages; current += 1) {
    const next = await queryMaterialTasksPage({ current, pageSize, category });
    records.push(...(next.records ?? []));
  }
  return records;
}
