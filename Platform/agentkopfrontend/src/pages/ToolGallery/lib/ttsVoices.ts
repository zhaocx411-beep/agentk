/**
 * 工具广场：按 TTS 工具名匹配 /agentk/tts/voices 里的引擎音色
 */
import type { TtsEngineOption, TtsVoiceOption } from '@/pages/AgentPlayground/types';

const TTS_VOICE_TOOLS = new Set([
  'generate_speech_qwen',
  'generate_speech_minimax',
  'generate_speech_cosyvoice',
]);

export function ttsToolKey(toolName: string): string {
  return toolName.trim().replace(/^tool:/, '').replace(/^agentk\./, '');
}

export function isTtsVoiceTool(toolName: string): boolean {
  return TTS_VOICE_TOOLS.has(ttsToolKey(toolName));
}

export function findTtsEngineForTool(
  engines: TtsEngineOption[],
  toolName: string,
): TtsEngineOption | undefined {
  const key = ttsToolKey(toolName);
  const engineName = key.replace(/^generate_speech_/, '').toLowerCase();
  return (
    engines.find((row) => ttsToolKey(row.tool) === key) ??
    engines.find((row) => row.engine.toLowerCase() === engineName) ??
    engines.find((row) => {
      const engine = row.engine.toLowerCase();
      return engine.includes(engineName) || engineName.includes(engine);
    })
  );
}

export function findTtsEngineByVoice(
  engines: TtsEngineOption[],
  voiceId: string,
): TtsEngineOption | undefined {
  if (!voiceId) return undefined;
  return engines.find((row) => row.voices.some((voice) => voice.voiceId === voiceId));
}

export function ttsEngineSelectOptions(engines: TtsEngineOption[]): Array<{ value: string; label: string }> {
  return engines.map((row) => ({
    value: row.engine,
    label: row.displayName?.trim() || row.engine,
  }));
}

export function ttsVoiceSelectOptions(
  voices: TtsVoiceOption[] | undefined,
  current?: string,
): Array<{ value: string; label: string }> {
  const options = (voices ?? []).map((voice) => ({
    value: voice.voiceId,
    label: voice.displayName?.trim() ? `${voice.displayName}（${voice.voiceId}）` : voice.voiceId,
  }));
  const value = current?.trim() ?? '';
  if (value && !options.some((item) => item.value === value)) {
    return [{ value, label: value }, ...options];
  }
  return options;
}
