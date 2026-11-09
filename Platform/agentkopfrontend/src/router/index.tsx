import { lazy } from 'react';
import { Navigate } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import MainLayout from '@/layouts/MainLayout';
import AuthGuard from './AuthGuard';
import AgentsIndexRedirect from './AgentsIndexRedirect';

/** 懒加载页面组件 */
const AgentPlayground = lazy(() => import('@/pages/AgentPlayground'));
const DatasetManage = lazy(() => import('@/pages/OfflineRun/DatasetManage'));
const OfflineRunJobs = lazy(() => import('@/pages/OfflineRun/JobManage'));
const SessionReview = lazy(() => import('@/pages/SessionReview'));
const SessionConversationReview = lazy(() => import('@/pages/SessionReview/ConversationDetail'));
const ModelArena = lazy(() => import('@/pages/ModelArena'));
const ModelArenaManage = lazy(() => import('@/pages/ModelArena/Manage'));
// Pipeline 编辑器（画布）暂时下线：画布编排结果尚无法编译为后端可执行的 definitionJson，
// 工作流 Agent 的配置能力由 /agents/wf/:workflowId 承担。恢复时放开此处与下方 workflow 路由即可。
// const WorkflowEditor = lazy(() => import('@/pages/WorkflowEditor'));
const WorkflowRunner = lazy(() => import('@/pages/WorkflowRunner'));
const PermissionControl = lazy(() => import('@/pages/PermissionControl'));
const AgentCenter = lazy(() => import('@/pages/AgentCenter'));
const CustomAgentCreate = lazy(() => import('@/pages/AgentCenter/CustomAgentCreate'));
const AgentDetail = lazy(() => import('@/pages/AgentCenter/AgentDetail'));
const WorkflowAgentDetail = lazy(
  () => import('@/pages/AgentCenter/WorkflowAgentDetail'),
);
const OfficialExperience = lazy(() => import('@/pages/AgentCenter/OfficialExperience'));
const OfficialMarketDetail = lazy(
  () => import('@/pages/AgentCenter/OfficialMarketDetail'),
);
const OfficialWorkflowConsole = lazy(
  () => import('@/pages/AgentCenter/OfficialWorkflowConsole'),
);
const ToolGallery = lazy(() => import('@/pages/ToolGallery'));
const ToolDetail = lazy(() => import('@/pages/ToolGallery/components/ToolDetail'));
const OssToolLibrary = lazy(() => import('@/pages/OssToolLibrary'));
const Agent100 = lazy(() => import('@/pages/Agent100'));
const Agent100Detail = lazy(() => import('@/pages/Agent100/AgentDetail'));
const WorksGallery = lazy(() => import('@/pages/WorksGallery'));
const MaterialLibrary = lazy(() => import('@/pages/MaterialLibrary'));
const Home = lazy(() => import('@/pages/Home'));

/** 路由元信息（用于页面标题等） */
export interface RouteMeta {
  title: string;
}

/** 带 meta 的路由配置 */
export interface MetaRouteObject extends Omit<RouteObject, 'children'> {
  meta?: RouteMeta;
  children?: MetaRouteObject[];
}

/**
 * 路由配置表
 * - 所有页面放在 MainLayout 下
 * - 每个路由可配置 meta.title，自动更新浏览器标题
 * - 无登录页：认证由网关 SSO 层处理
 */
export const routes: MetaRouteObject[] = [
  {
    path: '/',
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    children: [
      // index 路由：默认进入平台首页
      {
        index: true,
        element: <Home />,
        meta: { title: '首页' },
      },
      // —— Agent 资产 ——
      // /agents 已拆为 official / mine 两条路由，此处仅做旧地址转发
      {
        path: 'agents',
        element: <AgentsIndexRedirect />,
      },
      // 两条路由复用同一组件，key 不同以强制重挂载，避免切换时沿用上一侧的滚动位置
      {
        path: 'agents/official',
        element: <AgentCenter key="official" scope="official" />,
        meta: { title: '可灵Agent中后台' },
      },
      {
        path: 'agents/mine',
        element: <AgentCenter key="mine" scope="mine" />,
        meta: { title: '我的 Agent' },
      },
      {
        path: 'agents/new',
        element: <CustomAgentCreate />,
        meta: { title: '自定义Agent' },
      },
      {
        path: 'tools',
        element: <ToolGallery />,
        meta: { title: '工具广场' },
      },
      {
        path: 'tools/:toolName',
        element: <ToolDetail />,
        meta: { title: '工具详情' },
      },
      {
        path: 'oss-tools',
        element: <OssToolLibrary />,
        meta: { title: '开源能力灵感库' },
      },
      {
        path: 'agent100',
        element: <Agent100 />,
        meta: { title: 'Agent100' },
      },
      {
        path: 'agent100/:agentId',
        element: <Agent100Detail />,
        meta: { title: 'Agent100' },
      },
      {
        path: 'works-gallery',
        element: <WorksGallery key="team" scope="team" />,
        meta: { title: '团队作品展示' },
      },
      {
        path: 'my-works',
        element: <WorksGallery key="mine" scope="mine" />,
        meta: { title: '我的作品集' },
      },
      {
        path: 'material-library',
        element: <MaterialLibrary />,
        meta: { title: '素材库' },
      },
      // 详情类路由：静态段优先级高于动态段，不会被 agents/:agentId 抢匹配
      {
        path: 'agents/official/chat/:sessionId',
        element: <OfficialExperience />,
        meta: { title: '官方 Agent 体验' },
      },
      {
        path: 'agents/official/console/:sessionId',
        element: <OfficialWorkflowConsole />,
        meta: { title: '工作流生成控制台' },
      },
      {
        path: 'agents/official/app/:cardKey',
        element: <OfficialMarketDetail />,
        meta: { title: 'Agent 详情' },
      },
      {
        path: 'agents/wf/:workflowId',
        element: <WorkflowAgentDetail />,
        meta: { title: '工作流 Agent 配置' },
      },
      {
        path: 'agents/:agentId',
        element: <AgentDetail />,
        meta: { title: 'Agent 配置' },
      },

      // —— 调试与运行 ——
      {
        path: 'agent-playground',
        element: <AgentPlayground />,
        meta: { title: 'Agent 调试台' },
      },
      // Pipeline 编辑器暂时下线，/workflow 会落到下方通配路由并回到 Agent 目录
      // {
      //   path: 'workflow',
      //   element: <WorkflowEditor />,
      //   meta: { title: 'Pipeline 编辑器' },
      // },
      {
        path: 'workflow-runner',
        element: <WorkflowRunner />,
        meta: { title: 'Pipeline 运行' },
      },

      // —— 批量跑数 ——
      // 原「离线跑数」页内的两个 Tab 拆成独立路由
      {
        path: 'offline-run',
        element: <Navigate to="/offline-run/datasets" replace />,
      },
      {
        path: 'offline-run/datasets',
        element: <DatasetManage />,
        meta: { title: '数据集管理' },
      },
      {
        path: 'offline-run/jobs',
        element: <OfflineRunJobs />,
        meta: { title: '离线跑数' },
      },
      {
        path: 'session-review',
        element: <SessionReview />,
        meta: { title: '数据回看' },
      },
      {
        path: 'session-review/:agentkSessionId',
        element: <SessionConversationReview />,
        meta: { title: '对话回看' },
      },
      {
        path: 'model-arena',
        element: <ModelArena />,
        meta: { title: '多模型对战' },
      },
      {
        path: 'model-arena/manage',
        element: <ModelArenaManage />,
        meta: { title: '模型对战管理' },
      },

      // —— 系统设置 ——
      {
        path: 'permission-control',
        element: <PermissionControl />,
        meta: { title: '权限控制' },
      },

      // 通配路由：访问不存在的路径时回到首页
      {
        path: '*',
        element: <Navigate to="/" replace />,
        meta: { title: '首页' },
      },
    ],
  },
];
