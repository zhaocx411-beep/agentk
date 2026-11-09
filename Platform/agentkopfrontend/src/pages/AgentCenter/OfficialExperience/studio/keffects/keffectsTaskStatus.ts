/**
 * keffects 任务状态 → 生成记录状态
 */
import { isKeffectsTaskActive, type KeffectsTask } from '@/services/keffectsTasks';
import type { GenerationRecord } from '../lib/types';

export function statusFromKeffectsTask(
  task: KeffectsTask | null | undefined,
): GenerationRecord['status'] {
  if (!task?.status) return null;
  if (isKeffectsTaskActive(task.status)) return 'running';
  if (task.status === 'SUCCEEDED') return 'done';
  if (task.status === 'FAILED') return 'failed';
  if (task.status === 'CANCELLED' || task.status === 'CANCELLING') return 'cancelled';
  return null;
}
