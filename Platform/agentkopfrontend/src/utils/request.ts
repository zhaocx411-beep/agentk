import axios, { AxiosError, type AxiosRequestConfig, type InternalAxiosRequestConfig } from 'axios';
import { message } from './feedback';
import { useAppStore } from '@/store/app';
import type { ApiResponse } from '@/types/api';

/**
 * Axios 实例
 * - 基础地址从环境变量读取
 * - 超时时间 30s
 * - withCredentials 开启，自动携带 cookie（SSO 认证依赖）
 */
const request = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL,
  timeout: 30000,
  withCredentials: true, // 关键：携带 cookie 用于网关 SSO 认证
  headers: {
    'Content-Type': 'application/json',
    'X-Requested-With': 'XMLHttpRequest',
  },
});

/** 正在请求的数量，用于全局 loading 控制 */
let requestCount = 0;

/**
 * 显示全局 loading
 */
function showLoading() {
  if (requestCount === 0) {
    useAppStore.getState().setLoading(true);
  }
  requestCount++;
}

/**
 * 隐藏全局 loading
 */
function hideLoading() {
  requestCount--;
  if (requestCount <= 0) {
    requestCount = 0;
    useAppStore.getState().setLoading(false);
  }
}

/**
 * 请求拦截器
 * - 不注入 Authorization header，认证依赖 cookie
 * - 添加请求唯一标识
 * - 控制全局 loading
 * - GET 请求添加时间戳防缓存
 */
request.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    // 全局 loading（可通过 config.headers['X-No-Loading'] 跳过）
    if (!config.headers?.['X-No-Loading']) {
      showLoading();
    }

    // 添加请求唯一标识
    config.headers['X-Request-Id'] = `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;

    // GET 请求添加时间戳防缓存（暂时关闭）
    // if (config.method?.toUpperCase() === 'GET') {
    //   config.params = { ...config.params, _t: Date.now() };
    // }

    return config;
  },
  (error: AxiosError) => {
    hideLoading();
    return Promise.reject(error);
  },
);

/** 请求被 AbortController / 重复发起等原因取消时不提示错误 */
export function isRequestCanceled(error: unknown): boolean {
  if (!axios.isAxiosError(error)) {
    const msg = error instanceof Error ? error.message.toLowerCase() : '';
    return msg.includes('canceled') || msg.includes('cancelled') || msg.includes('aborted');
  }
  if (axios.isCancel(error)) return true;
  if (error.code === 'ERR_CANCELED') return true;
  if (error.name === 'CanceledError') return true;
  const msg = error.message?.toLowerCase() ?? '';
  return msg.includes('canceled') || msg.includes('cancelled') || msg.includes('aborted');
}

function headerFlag(
  headers: InternalAxiosRequestConfig['headers'] | undefined,
  name: string,
): boolean {
  if (!headers) return false;
  const value = typeof headers.get === 'function' ? headers.get(name) : headers[name];
  if (value == null || value === false) return false;
  const text = String(value).toLowerCase();
  return text !== '' && text !== '0' && text !== 'false';
}

/**
 * 响应拦截器
 * - 统一处理业务错误码
 * - 统一 message 错误提示（可用 headers['X-Skip-Error-Toast'] 跳过，供轮询自行处理）
 * - 401 仅提示（网关会处理重定向到 SSO 登录）
 */
request.interceptors.response.use(
  (response) => {
    hideLoading();

    const data = response.data as ApiResponse;

    // 业务层错误判断（根据后端约定的 code 字段）
    // 后端 code: 200 表示成功，非 200 均视为业务错误
    if (data.code !== undefined && data.code !== 200 && data.code !== 0) {
      if (!headerFlag(response.config.headers, 'X-Skip-Error-Toast')) {
        message.error(data.message || '请求失败');
      }
      const err = new Error(data.message || '请求失败') as Error & { code?: number };
      err.code = data.code;
      return Promise.reject(err);
    }

    return response;
  },
  (error: AxiosError<ApiResponse>) => {
    hideLoading();

    if (isRequestCanceled(error)) {
      return Promise.reject(error);
    }

    if (headerFlag(error.config?.headers, 'X-Skip-Error-Toast')) {
      return Promise.reject(error);
    }

    const { response } = error;

    if (response) {
      switch (response.status) {
        case 401:
          message.error('未授权访问');
          // 网关层会自动处理 SSO 重定向，前端无需手动跳转
          break;
        case 403:
          message.error('无权限访问');
          break;
        case 404:
          message.error('请求资源不存在');
          break;
        case 500:
          message.error('服务器错误');
          break;
        default:
          message.error(response.data?.message || `请求失败 (${response.status})`);
      }
    } else if (error.message?.includes('timeout')) {
      message.error('请求超时，请稍后重试');
    } else {
      message.error('网络异常，请检查网络连接');
    }

    return Promise.reject(error);
  },
);

/**
 * 通用请求方法 - 泛型封装
 * @param config Axios 请求配置
 * @returns 泛型响应数据
 */
export async function http<T = unknown>(config: AxiosRequestConfig): Promise<T> {
  const response = await request(config);
  const data = response.data as ApiResponse<T>;
  return data.data as T;
}

/** GET 请求 */
export function get<T = unknown>(url: string, params?: object, config?: AxiosRequestConfig) {
  return http<T>({ ...config, method: 'GET', url, params });
}

/** POST 请求 */
export function post<T = unknown>(url: string, data?: object, config?: AxiosRequestConfig) {
  return http<T>({ ...config, method: 'POST', url, data });
}

/** PUT 请求 */
export function put<T = unknown>(url: string, data?: object, config?: AxiosRequestConfig) {
  return http<T>({ ...config, method: 'PUT', url, data });
}

/** PATCH 请求 */
export function patch<T = unknown>(url: string, data?: object, config?: AxiosRequestConfig) {
  return http<T>({ ...config, method: 'PATCH', url, data });
}

/** DELETE 请求 */
export function del<T = unknown>(url: string, params?: object, config?: AxiosRequestConfig) {
  return http<T>({ ...config, method: 'DELETE', url, params });
}

export default request;
