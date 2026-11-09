import { load as loadYaml } from 'js-yaml';
import type { BaseProfile, Session } from '@/pages/AgentPlayground/types';
import { sanitizeProfile } from '@/utils/profile';

type ProfileSnapshot = BaseProfile | Session['profileYaml'];

interface PreviewSessionDecision {
  sessionId: string | null;
  dirty: boolean;
  currentProfile: BaseProfile;
  sessionProfile?: ProfileSnapshot;
  sessionVersionId?: number;
  currentVersionId?: number;
}

function parseProfile(value: ProfileSnapshot): BaseProfile | null {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return sanitizeProfile(value);
  }
  if (typeof value !== 'string' || !value.trim()) return null;
  try {
    const parsed = loadYaml(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? sanitizeProfile(parsed)
      : null;
  } catch {
    return null;
  }
}

function sortRecordDeep(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortRecordDeep);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .filter(([, item]) => item !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => [key, sortRecordDeep(item)]),
  );
}

function comparableProfile(value: BaseProfile): unknown {
  const profile = sanitizeProfile(value);
  return sortRecordDeep({
    system: (profile.system ?? '').replace(/\r\n/g, '\n').trim(),
    defaultModel: (profile.defaultModel ?? '').trim(),
    // 后端可能把 ref 归一化为 capability；工具配置以 canonical name/instructions 为准。
    tools: (profile.tools ?? []).map((tool) => ({
      name: tool.name.trim(),
      instructions: (tool.instructions ?? '').replace(/\r\n/g, '\n').trim(),
    })),
    toolDefaults: profile.toolDefaults ?? {},
    httpTools: profile.httpTools ?? [],
    pipelines: profile.pipelines ?? [],
  });
}

export function areProfilesEquivalent(
  currentProfile: BaseProfile,
  sessionProfile: ProfileSnapshot,
): boolean | null {
  const historical = parseProfile(sessionProfile);
  if (!historical) return null;
  return (
    JSON.stringify(comparableProfile(currentProfile)) ===
    JSON.stringify(comparableProfile(historical))
  );
}

/**
 * 优先比较完整 Profile 的语义内容；后端快照缺失时才回退版本 ID。
 */
export function shouldStartNewPreviewSession({
  sessionId,
  dirty,
  currentProfile,
  sessionProfile,
  sessionVersionId,
  currentVersionId,
}: PreviewSessionDecision): boolean {
  if (!sessionId || dirty) return true;
  const profilesEquivalent = areProfilesEquivalent(currentProfile, sessionProfile);
  if (profilesEquivalent != null) return !profilesEquivalent;
  return (
    sessionVersionId != null &&
    currentVersionId != null &&
    sessionVersionId !== currentVersionId
  );
}
