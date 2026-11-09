import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const publicUrl = new URL(
  process.env.AGENTK_DOCS_PUBLIC_URL?.trim() ||
    'https://localhost:3000/docs/',
);
if (
  publicUrl.protocol !== 'https:' ||
  publicUrl.username !== '' ||
  publicUrl.password !== '' ||
  publicUrl.pathname !== '/docs/' ||
  publicUrl.search !== '' ||
  publicUrl.hash !== ''
) {
  throw new Error(
    'AGENTK_DOCS_PUBLIC_URL must be an HTTPS URL with the exact path /docs/',
  );
}

const config: Config = {
  title: 'AgentK',
  tagline: 'Distributed, durable agentic runtime',
  future: {
    v4: true,
  },
  url: publicUrl.origin,
  baseUrl: publicUrl.pathname,
  onBrokenLinks: 'throw',
  i18n: {
    defaultLocale: 'zh-Hans',
    locales: ['zh-Hans'],
  },
  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],
  plugins: [
    [
      '@docusaurus/plugin-client-redirects',
      {
        redirects: [
          {from: '/sdk', to: '/development'},
          {from: '/quickstart', to: '/client/quickstart'},
          {from: '/guides/chat-client', to: '/client/guides/chat-client'},
          {from: '/sdk/transports', to: '/client/transports'},
          {from: '/sdk/client-go', to: '/client/sdk/go'},
          {from: '/sdk/client-python', to: '/client/sdk/python'},
          {from: '/sdk/client-java', to: '/client/sdk/java'},
          {
            from: ['/guides/provide-workflow', '/worker/guides/provide-workflow'],
            to: '/worker/guides/capability',
          },
          {
            from: ['/guides/provide-tool', '/worker/guides/provide-tool'],
            to: '/worker/guides/tool',
          },
          {from: '/sdk/worker-go', to: '/worker/sdk/go'},
          {from: '/sdk/worker-python', to: '/worker/sdk/python'},
        ],
      },
    ],
    './plugins/capability-proxy/index.ts',
    [
      './plugins/ai-docs/index.ts',
      {
        summary: 'AgentK 是分布式、持久化的 Agentic Runtime。',
        guidance: [
          '先阅读 AgentK 概览，再根据职责选择 Client 开发或 Worker 开发。',
          'Client 开发面向调用 AgentK 的下游应用；Worker 开发面向发布 Capability 的能力提供方。',
          '接入前理解 Capability、Profile 和 Session 的边界。',
          '以下链接均为当前构建自动生成的 Markdown。',
        ],
      },
    ],
  ],
  themeConfig: {
    colorMode: {
      defaultMode: 'light',
      disableSwitch: true,
    },
    navbar: {
      title: 'AgentK',
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: '了解 AgentK',
        },
        {
          type: 'docSidebar',
          sidebarId: 'clientSidebar',
          position: 'left',
          label: 'Client 开发',
        },
        {
          type: 'docSidebar',
          sidebarId: 'workerSidebar',
          position: 'left',
          label: 'Worker 开发',
        },
        {
          to: '/reference',
          position: 'left',
          label: 'API Reference',
        },
        {
          to: '/capabilities',
          position: 'left',
          label: '能力目录',
          className: 'navbar-capability-catalog',
        },
        {
          type: 'docSidebar',
          sidebarId: 'platformSidebar',
          position: 'left',
          label: '平台',
        },
      ],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
