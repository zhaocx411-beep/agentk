export type ConceptDefinition = {
  label: string;
  description: string;
  href: string;
};

export const concepts = {
  capability: {
    label: 'Capability',
    description:
      '带有稳定 ID 和输入输出契约的可执行领域能力，由 Temporal Activity 或 Workflow 实现。',
    href: '/concepts/capability-tool-pipeline#capability',
  },
  tool: {
    label: 'Tool',
    description:
      'Capability 的模型调用入口，由 Agent Loop 中的模型选择并等待执行结果。',
    href: '/concepts/capability-tool-pipeline#tool',
  },
  pipeline: {
    label: 'Pipeline',
    description:
      'Capability 的显式调用入口，由下游应用选择并作为独立 Execution 运行。',
    href: '/concepts/capability-tool-pipeline#pipeline',
  },
  profile: {
    label: 'Profile',
    description:
      '创建 Session 时提交并冻结的 Agent 定义，包含模型、指令以及可用的 Tool 和 Pipeline。',
    href: '/concepts/profile-session#profile',
  },
  session: {
    label: 'Session',
    description:
      '由一份冻结 Profile 创建的持续运行上下文，承载按顺序发生的 Execution 与 Event。',
    href: '/concepts/profile-session#session',
  },
  execution: {
    label: 'Execution',
    description:
      'Session 中一次完整的 Agent Loop 或 Pipeline 运行，拥有独立状态和 exe_... ID。',
    href: '/concepts/runtime-model#execution',
  },
  eventLog: {
    label: 'Event Log',
    description:
      'Session 中只追加的持久事件记录，用于历史回读、断线恢复和最终状态校准。',
    href: '/concepts/events-stream-artifacts#event-log',
  },
  artifact: {
    label: 'Artifact',
    description:
      'Tool 或 Pipeline 产生的结构化产物引用，随成功事件或 Pipeline 输出返回。',
    href: '/concepts/events-stream-artifacts#artifact',
  },
} as const satisfies Record<string, ConceptDefinition>;

export type ConceptId = keyof typeof concepts;
