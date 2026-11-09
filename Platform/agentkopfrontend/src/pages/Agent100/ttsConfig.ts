/**
 * 使用 config.tts / fieldOptions.ttsProvider，不把引擎、模型、音色摊平混用
 */
import type {
  Agent100TtsConfig,
  Agent100TtsProvider,
  FieldOptionItem,
  SchemaFieldContext,
} from './types';

export function ttsProviderOf(
  tts: Agent100TtsConfig | undefined,
  provider: string,
): Agent100TtsProvider | undefined {
  const key = provider.trim();
  if (!key) return undefined;
  return tts?.providers?.[key];
}

export function defaultTtsValue(tts: Agent100TtsConfig | undefined): Record<string, string> {
  const provider = tts?.defaultProvider?.trim() || Object.keys(tts?.providers ?? {})[0] || '';
  return applyTtsProviderChange(tts, provider);
}

export function applyTtsProviderChange(
  tts: Agent100TtsConfig | undefined,
  provider: string,
): Record<string, string> {
  const row = ttsProviderOf(tts, provider);
  return {
    provider,
    model: row?.defaultModel || row?.models?.[0] || '',
    voiceId: row?.defaultVoiceId || row?.voices?.[0]?.id || '',
  };
}

export function resolveTtsProviderOptions(context: SchemaFieldContext): FieldOptionItem[] {
  const fromField = context.fieldOptions.ttsProvider;
  if (fromField?.length) {
    return fromField.map((item) => ({
      value: String(item.id),
      label: item.label || String(item.id),
    }));
  }
  return Object.keys(context.tts?.providers ?? {}).map((id) => ({ value: id, label: id }));
}

export function resolveTtsModelOptions(provider: string, tts?: Agent100TtsConfig): FieldOptionItem[] {
  return (ttsProviderOf(tts, provider)?.models ?? []).map((id) => ({ value: id, label: id }));
}

export function resolveTtsVoiceOptions(provider: string, tts?: Agent100TtsConfig): FieldOptionItem[] {
  return (ttsProviderOf(tts, provider)?.voices ?? []).map((voice) => ({
    value: voice.id,
    label: voice.description ? `${voice.id} · ${voice.description}` : voice.id,
  }));
}
