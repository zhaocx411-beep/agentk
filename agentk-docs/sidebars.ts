import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    'intro',
    'development',
    'mcp',
    {
      type: 'category',
      label: '核心概念',
      items: [
        'concepts/runtime-model',
        'concepts/capability-tool-pipeline',
        'concepts/profile-session',
        'concepts/events-stream-artifacts',
        'concepts/capability-execution-progress',
        'concepts/request-lane',
      ],
    },
    {
      type: 'category',
      label: '完整示例',
      items: [
        'examples/go-storyboard-job-api',
        'examples/python-storyboard-mcp',
      ],
    },
  ],
  clientSidebar: [
    'client/index',
    'client/quickstart',
    {
      type: 'category',
      label: 'Client 指南',
      items: [
        'client/guides/invoke-capability',
        'client/guides/chat-client',
        'client/guides/request-lane',
      ],
    },
    {
      type: 'category',
      label: 'Client SDK',
      items: [
        'client/transports',
        'client/sdk/go',
        'client/sdk/python',
        'client/sdk/java',
      ],
    },
  ],
  workerSidebar: [
    'worker/index',
    {
      type: 'category',
      label: 'Worker 指南',
      items: [
        'worker/guides/capability',
        'worker/guides/tool',
        'worker/guides/worker-lane',
      ],
    },
    {
      type: 'category',
      label: 'Worker SDK',
      items: ['worker/sdk/go', 'worker/sdk/python'],
    },
  ],
  referenceSidebar: [
    'reference/overview',
    {
      type: 'category',
      label: 'Go',
      items: [
        'reference/go/client',
        'reference/go/worker',
        'reference/go/protocol',
      ],
    },
    {
      type: 'category',
      label: 'Python',
      items: [
        'reference/python/client',
        'reference/python/worker',
        'reference/python/protocol',
      ],
    },
    {
      type: 'category',
      label: 'Java',
      items: [
        'reference/java/client',
        'reference/java/protocol',
      ],
    },
  ],
  platformSidebar: ['platform/index'],
};

export default sidebars;
