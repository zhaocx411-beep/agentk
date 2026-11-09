import { get } from '@/utils/request';
import {
  cancelKeffectsTask,
  CLOTHING_DUPE_TASK_TYPE,
  CLOTHING_DUPE_WORKFLOW_KEY,
  createKeffectsTask,
  getKeffectsTask,
  isKeffectsTaskActive,
  normalizeKeffectsTaskOutput,
  type KeffectsTask,
} from './keffectsTasks';

export { CLOTHING_DUPE_TASK_TYPE, CLOTHING_DUPE_WORKFLOW_KEY };

const CLOTHING_DUPE_BASE = '/agent/operation/backend/platform/keffects/clothing-dupe';
export const CLOTHING_DUPE_VOICE_PACKS_QUERY_KEY = ['keffects', 'clothing-dupe', 'voice-packs'] as const;

export interface ClothingDupeVoicePack {
  displayName: string;
  voiceId: string;
}

export function getClothingDupeVoicePacks() {
  return get<ClothingDupeVoicePack[]>(`${CLOTHING_DUPE_BASE}/voice-packs`);
}

export type ClothingDupeStatus =
  | 'SUBMITTED'
  | 'PROCESSING'
  | 'QUEUED'
  | 'RUNNING'
  | 'SUCCEEDED'
  | 'FAILED'
  | 'CANCELLING'
  | 'CANCELLED';

export type ClothingDupeContent =
  | { type: 'prompt'; text: string }
  | { type: 'source_video' | 'product_image' | 'model_image' | 'bgm'; url: string }
  | { type: 'voice'; voiceId: string };

export interface ClothingDupeCreateRequest {
  taskType: typeof CLOTHING_DUPE_TASK_TYPE;
  requestId: string;
  input: {
    contents: ClothingDupeContent[];
    settings: {
      resolution: '720p' | '1080p';
      aspectRatio: '9:16' | '2:3' | '3:4' | '1:1' | '4:3' | '3:2' | '16:9' | '21:9';
    };
  };
}

export interface ClothingDupeOutputAsset {
  bucket?: string;
  key?: string;
  url?: string;
  fileUrl?: string;
  blobUrl?: string;
  cdnUrl?: string;
}

export type ClothingDupeOutput = Record<string, ClothingDupeOutputAsset | string | null>;
export type ClothingDupeTask = KeffectsTask<ClothingDupeOutput>;

function asHttpUrl(value: string | undefined): string {
  const trimmed = (value || '').trim();
  if (!trimmed) return '';
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  if (trimmed.startsWith('//')) return `https:${trimmed}`;
  return '';
}

/** 从产物对象或字符串解析可预览 URL（统一优先 blobUrl） */
export function resolveClothingDupeAssetUrl(
  value: ClothingDupeOutputAsset | string | null | undefined,
): string {
  if (typeof value === 'string') return asHttpUrl(value);
  if (!value) return '';
  return (
    asHttpUrl(value.blobUrl) ||
    asHttpUrl(value.url) ||
    asHttpUrl(value.fileUrl) ||
    asHttpUrl(value.cdnUrl) ||
    ''
  );
}

export function createClothingDupeTask(body: ClothingDupeCreateRequest) {
  return createKeffectsTask<ClothingDupeOutput>(body).then(normalizeKeffectsTaskOutput);
}

export function getClothingDupeTask(taskId: string) {
  return getKeffectsTask<ClothingDupeOutput>(taskId);
}

export function cancelClothingDupeTask(taskId: string) {
  return cancelKeffectsTask<ClothingDupeOutput>(taskId);
}

export function isClothingDupeWorkflow(workflowKey?: string): boolean {
  return workflowKey === CLOTHING_DUPE_WORKFLOW_KEY;
}

export function isClothingDupeActive(status?: string): boolean {
  return isKeffectsTaskActive(status);
}
