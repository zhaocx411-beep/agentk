import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClientProvider } from '@tanstack/react-query';
import { App as AntdApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import App from './App';
import { queryClient } from './services/queryClient';
import { themeConfig } from './styles/theme';
import 'virtual:svg-icons-register';
import './styles/global.less';

/** 获取路由 basename，去掉末尾斜杠以兼容访问路径有无尾斜杠的情况 */
const routerBase = (import.meta.env.VITE_ROUTER_BASE || '/').replace(/\/$/, '') || '/';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ConfigProvider locale={zhCN} theme={themeConfig}>
        <AntdApp>
          <BrowserRouter
            basename={routerBase}
            future={{ v7_startTransition: true, v7_relativeSplatPath: true }}
          >
            <App />
          </BrowserRouter>
        </AntdApp>
      </ConfigProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
