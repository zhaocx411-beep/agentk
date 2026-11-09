/**
 * Agent100 目录与配置
 * GET /agent/operation/backend/platform/agent100
 * GET /agent/operation/backend/platform/agent100/{agentId}/config
 */

export interface JsonSchemaNode {
  type?: string;
  properties?: Record<string, JsonSchemaNode>;
  required?: string[];
  enum?: Array<string | number | boolean>;
  default?: unknown;
  const?: unknown;
  minLength?: number;
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: number;
  minItems?: number;
  maxItems?: number;
  additionalProperties?: boolean;
  items?: JsonSchemaNode;
  pattern?: string;
}

export interface Agent100Summary {
  id: string;
  name: string;
  icon: string;
  accent: string;
  description: string;
}

export interface Agent100ListData {
  agents: Agent100Summary[];
  count: number;
}

export interface Agent100ModelConfig {
  defaults: Record<string, string>;
  options: Record<string, string[]>;
}

export interface Agent100Capability {
  id: string;
  published: boolean;
  required: boolean;
  group: string;
  activityOnly: boolean;
}

export interface Agent100Voice {
  id: string;
  description: string;
}

export interface Agent100TtsProvider {
  capabilityId?: string;
  defaultModel?: string;
  models?: string[];
  defaultVoiceId?: string;
  voices?: Agent100Voice[];
}

export interface Agent100TtsConfig {
  defaultProvider?: string;
  providers?: Record<string, Agent100TtsProvider>;
}

export interface Agent100FieldOption {
  id: string;
  label: string;
}

export interface Agent100Pipeline {
  name: string;
  displayName: string;
  capability: string;
  inputSchema: JsonSchemaNode;
}

export interface Agent100Config {
  agentId: string;
  models: Agent100ModelConfig;
  capabilities: Agent100Capability[];
  capabilityError: string;
  mediaError: string;
  voices: Record<string, Agent100Voice[]>;
  fieldOptions: Record<string, Agent100FieldOption[]>;
  tts?: Agent100TtsConfig;
  pipelines: Agent100Pipeline[];
}

export interface FieldOptionItem {
  value: string;
  label: string;
}

export interface SchemaFieldContext {
  fieldOptions: Record<string, Agent100FieldOption[]>;
  voices: Agent100Voice[];
  modelOptions: Record<string, string[]>;
  tts?: Agent100TtsConfig;
}

export type Agent100RunStatus = 'running' | 'completed' | 'failed' | 'cancelled';

export interface Agent100StartRequest {
  pipeline: string;
  title?: string;
  args: Record<string, unknown>;
  sessionId?: string;
}

export interface Agent100RunEvent {
  id: string;
  type: string;
  step?: string;
  message?: string;
  data?: Record<string, unknown>;
}

export interface Agent100RunRequestSnapshot {
  pipeline?: string;
  title?: string;
  args?: Record<string, unknown>;
}

export interface Agent100Execution {
  agentId: string;
  sessionId: string;
  executionId: string;
  pipeline: string;
  capabilityId?: string;
  status: Agent100RunStatus;
  error?: string;
  events?: Agent100RunEvent[];
  output?: Record<string, unknown> | null;
  request?: Agent100RunRequestSnapshot;
}

export interface Agent100HistoryItem {
  sessionId: string;
  executionId: string;
  pipeline: string;
  title?: string;
  status: Agent100RunStatus;
  error?: string;
  hasOutput?: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export interface Agent100HistoryData {
  agentId: string;
  items: Agent100HistoryItem[];
  count: number;
}
