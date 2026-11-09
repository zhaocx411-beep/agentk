/// <reference types="vite/client" />

// vite-plugin-svg-icons 虚拟模块声明
declare module 'virtual:svg-icons-register' {
  const component: void;
  export default component;
}

declare module 'virtual:svg-icons-names' {
  const names: string[];
  export default names;
}

/** Vite 环境变量类型声明 */
interface ImportMetaEnv {
  /** 应用标题 */
  readonly VITE_APP_TITLE: string;
  /** API 基础地址 */
  readonly VITE_API_BASE_URL: string;
  /** 开发服务器端口 */
  readonly VITE_PORT: string;
  /** 当前环境标识 */
  readonly VITE_APP_ENV: string;
  /** Vite proxy 代理目标地址（仅开发环境使用） */
  readonly VITE_PROXY_TARGET?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
