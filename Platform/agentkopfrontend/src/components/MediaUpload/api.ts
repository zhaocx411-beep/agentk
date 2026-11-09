import {
  MATERIAL_USAGE,
  MATERIAL_PIPELINE,
} from '@/pages/MaterialLibrary/types';
import { myPortfolioQueryKey } from '@/pages/WorksGallery/api';
import type { PortfolioPage, PortfolioQueryParams } from '@/pages/WorksGallery/types';
import type { DatasetTaskQueryResponse } from '@/pages/OfflineRun/types';
import {
  myUploadsQueryKey,
  queryMyUploads,
  type MyUploadsQueryParams,
  type MyUploadsPage,
} from '@/services/media';
import { get, post } from '@/utils/request';

export { myPortfolioQueryKey, myUploadsQueryKey };

/** 素材弹层网格分页，与作品库 / 历史上传列表一致 */
export const MEDIA_PICKER_PAGE_SIZE = 20;

const NO_LOADING = { headers: { 'X-No-Loading': '1' } };

export function fetchPickerUploads(params: MyUploadsQueryParams): Promise<MyUploadsPage> {
  return queryMyUploads(params);
}

export function fetchPickerPortfolio(params: PortfolioQueryParams): Promise<PortfolioPage> {
  return post<PortfolioPage>(
    '/agent/operation/backend/platform/chat/my-portfolio/query',
    params,
    NO_LOADING,
  );
}

/** 素材库：按接口 current + category 分页，页码与底部分页器一致 */
export function fetchPickerMaterialsPage(
  current: number,
  pageSize: number,
  category?: string,
): Promise<DatasetTaskQueryResponse> {
  return get<DatasetTaskQueryResponse>(
    '/agent/operation/backend/datasets/tasks/query',
    {
      usage: MATERIAL_USAGE,
      pipelineName: MATERIAL_PIPELINE,
      current,
      pageSize,
      ...(category ? { category } : {}),
    },
    NO_LOADING,
  );
}
