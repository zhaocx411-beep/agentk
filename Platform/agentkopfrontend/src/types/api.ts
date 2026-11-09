/**
 * 统一 API 响应结构
 * 所有接口返回数据遵循此泛型格式
 */
export interface ApiResponse<T = unknown> {
  /** 业务状态码：0 表示成功 */
  code: number;
  /** 提示消息 */
  message: string;
  /** 响应数据 */
  data: T;
}

/**
 * 分页请求参数
 */
export interface PaginationParams {
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  pageSize: number;
}

/**
 * 分页响应数据（list 形态）
 */
export interface PaginationResponse<T = unknown> {
  /** 数据列表 */
  list: T[];
  /** 总条数 */
  total: number;
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  pageSize: number;
}

/**
 * 平台分页响应（records 形态）
 * 如 /platform/agent-loops：{ page | pageNum, pageSize, total, records }
 */
export interface PlatformPageResult<T = unknown> {
  page: number;
  pageSize: number;
  total: number;
  records: T[];
}

/**
 * 通用选项类型（用于下拉选择等）
 */
export interface OptionItem {
  label: string;
  value: string | number;
  disabled?: boolean;
  children?: OptionItem[];
}
