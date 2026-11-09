import { useEffect } from 'react';
import { App as AntdApp } from 'antd';
import { useRoutes, type RouteObject } from 'react-router-dom';
import { bindFeedback } from './utils/feedback';
import { routes } from './router';

/**
 * 应用根组件
 * 使用 useRoutes 渲染路由配置
 */
function App() {
  const element = useRoutes(routes as RouteObject[]);

  // 把带主题的 message / modal 实例交给全局桥接，非组件模块（如 request.ts）也能用
  const feedbackApi = AntdApp.useApp();
  useEffect(() => bindFeedback(feedbackApi), [feedbackApi]);

  return element;
}

export default App;
