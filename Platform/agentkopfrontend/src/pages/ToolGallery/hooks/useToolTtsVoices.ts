/**
 * 工具广场 TTS 音色目录
 */
import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getTtsVoices, TTS_VOICES_QUERY_KEY } from '@/pages/AgentPlayground/api';
import { findTtsEngineForTool, isTtsVoiceTool } from '../lib/ttsVoices';

export function useToolTtsVoices(toolName: string) {
  const wantsVoiceList = isTtsVoiceTool(toolName);
  const { data: engines = [], isLoading } = useQuery({
    queryKey: TTS_VOICES_QUERY_KEY,
    queryFn: async () => {
      const res = await getTtsVoices();
      return res?.engines ?? [];
    },
    enabled: wantsVoiceList,
    staleTime: 10 * 60_000,
  });
  const preferredEngine = useMemo(
    () => findTtsEngineForTool(engines, toolName),
    [engines, toolName],
  );

  return {
    wantsVoiceList,
    engines,
    preferredEngine,
    loading: wantsVoiceList && isLoading,
  };
}
