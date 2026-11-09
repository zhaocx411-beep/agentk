/**
 * 语音合成 Agent：拉取 TTS 引擎/音色，维护发消息所需的 tts 参数
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getTtsVoices, TTS_VOICES_QUERY_KEY } from '@/pages/AgentPlayground/api';
import type { SendMessageTtsOptions, TtsEngineOption } from '@/pages/AgentPlayground/types';

export { TTS_VOICES_QUERY_KEY };

export function useTtsComposer(enabled: boolean) {
  const { data, isLoading } = useQuery({
    queryKey: TTS_VOICES_QUERY_KEY,
    queryFn: async () => {
      const res = await getTtsVoices();
      return res?.engines ?? [];
    },
    enabled,
    staleTime: 10 * 60_000,
  });

  const engines = useMemo(() => data ?? [], [data]);
  const [engine, setEngineState] = useState('');
  const [voiceId, setVoiceIdState] = useState('');

  useEffect(() => {
    if (!enabled || engines.length === 0) return;
    if (engine && engines.some((row) => row.engine === engine)) return;
    const first = engines[0];
    setEngineState(first.engine);
    setVoiceIdState(first.defaultVoiceId);
  }, [enabled, engine, engines]);

  const selectedEngine = useMemo(
    () => engines.find((row) => row.engine === engine),
    [engine, engines],
  );

  useEffect(() => {
    if (!selectedEngine) return;
    if (selectedEngine.voices.some((voice) => voice.voiceId === voiceId)) return;
    setVoiceIdState(selectedEngine.defaultVoiceId);
  }, [selectedEngine, voiceId]);

  const setEngine = useCallback(
    (nextEngine: string) => {
      setEngineState(nextEngine);
      const row = engines.find((item) => item.engine === nextEngine);
      if (row) setVoiceIdState(row.defaultVoiceId);
    },
    [engines],
  );

  const ttsOptions: SendMessageTtsOptions | undefined =
    enabled && engine && voiceId ? { engine, voiceId } : undefined;

  return {
    engines,
    engine,
    voiceId,
    setEngine,
    setVoiceId: setVoiceIdState,
    voices: selectedEngine?.voices ?? [],
    isLoading: enabled && isLoading,
    ttsOptions,
  };
}

export type TtsComposerModel = ReturnType<typeof useTtsComposer>;

export function findTtsEngine(engines: TtsEngineOption[], engine: string) {
  return engines.find((row) => row.engine === engine);
}
