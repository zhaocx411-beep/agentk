import { OSS_TOOLS } from './data';
import type { OssTool } from './types';

export const OSS_TOOLS_QUERY_KEY = ['oss-tools'] as const;

/**
 * 首期使用人工维护的数据；保留异步边界，未来可直接替换为后端接口。
 */
export async function getOssTools(): Promise<OssTool[]> {
  return OSS_TOOLS;
}
