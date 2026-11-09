/**
 * TTS voiceId：引擎 + 音色两级下拉，展示 /agentk/tts/voices 全部引擎
 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { Select } from 'antd';
import type { OfficialToolParameter } from '@/types/officialTools';
import type { TtsEngineOption } from '@/pages/AgentPlayground/types';
import {
  findTtsEngineByVoice,
  findTtsEngineForTool,
  ttsEngineSelectOptions,
  ttsVoiceSelectOptions,
} from '../../lib/ttsVoices';
import fieldStyles from '@/pages/AgentCenter/AgentDetail/AgentDetail.module.less';
import styles from '../../ToolGallery.module.less';

interface TtsVoiceParamFieldProps {
  param: OfficialToolParameter;
  toolName: string;
  engines: TtsEngineOption[];
  loading?: boolean;
  value: unknown;
  invalid?: boolean;
  onChange: (value: unknown) => void;
}

function TtsVoiceParamField({
  param,
  toolName,
  engines,
  loading = false,
  value,
  invalid = false,
  onChange,
}: TtsVoiceParamFieldProps) {
  const voiceId = typeof value === 'string' ? value : '';
  const preferredEngine = useMemo(
    () => findTtsEngineByVoice(engines, voiceId) ?? findTtsEngineForTool(engines, toolName) ?? engines[0],
    [engines, toolName, voiceId],
  );
  const [engine, setEngine] = useState(preferredEngine?.engine ?? '');
  const filledDefault = useRef(false);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    if (!engines.length) return;
    if (engine && engines.some((row) => row.engine === engine)) {
      const selected = engines.find((row) => row.engine === engine);
      if (voiceId && selected && !selected.voices.some((voice) => voice.voiceId === voiceId)) {
        const owner = findTtsEngineByVoice(engines, voiceId);
        if (owner) setEngine(owner.engine);
      }
      return;
    }
    setEngine(preferredEngine?.engine ?? '');
  }, [engine, engines, preferredEngine, voiceId]);

  useEffect(() => {
    if (filledDefault.current || !param.required || voiceId || !preferredEngine?.defaultVoiceId) return;
    filledDefault.current = true;
    onChangeRef.current(preferredEngine.defaultVoiceId);
    setEngine(preferredEngine.engine);
  }, [param.required, preferredEngine, voiceId]);

  const selectedEngine = engines.find((row) => row.engine === engine);
  const voiceOptions = ttsVoiceSelectOptions(selectedEngine?.voices, voiceId);

  const handleEngineChange = (nextEngine: string) => {
    setEngine(nextEngine);
    const row = engines.find((item) => item.engine === nextEngine);
    if (!row) {
      onChange(undefined);
      return;
    }
    if (voiceId && row.voices.some((voice) => voice.voiceId === voiceId)) return;
    onChange(param.required ? row.defaultVoiceId || undefined : undefined);
  };

  return (
    <div className={`${fieldStyles.toolParamCard} ${invalid ? fieldStyles.toolParamCardInvalid : ''}`}>
      <div className={fieldStyles.toolParamCardHead}>
        <span className={fieldStyles.toolParamName}>
          {param.name}
          {param.required ? (
            <span className={fieldStyles.toolParamRequiredMark} aria-hidden>
              *
            </span>
          ) : null}
        </span>
        <span className={param.required ? fieldStyles.toolParamRequired : fieldStyles.toolParamOptional}>
          {param.required ? '必填' : '可选'}
        </span>
        <span className={fieldStyles.toolParamType}>{param.type}</span>
      </div>
      {param.description ? <p className={fieldStyles.toolParamDesc}>{param.description}</p> : null}
      <div className={fieldStyles.toolParamControlWrap}>
        <div className={styles.ttsVoiceStack}>
          <label className={styles.ttsVoiceField}>
            <span className={styles.ttsVoiceLabel}>引擎</span>
            <Select
              size="large"
              loading={loading}
              className={styles.ttsVoiceSelect}
              placeholder={loading ? '正在加载引擎' : '请选择引擎'}
              value={engine || undefined}
              options={ttsEngineSelectOptions(engines)}
              onChange={handleEngineChange}
            />
          </label>
          <label className={styles.ttsVoiceField}>
            <span className={styles.ttsVoiceLabel}>音色</span>
            <Select
              allowClear={!param.required}
              showSearch
              size="large"
              loading={loading}
              className={styles.ttsVoiceSelect}
              placeholder={param.required ? '请选择音色' : '请选择音色，可留空'}
              value={voiceId || undefined}
              options={voiceOptions}
              optionFilterProp="label"
              filterOption={(input, option) => {
                const keyword = input.trim().toLowerCase();
                if (!keyword) return true;
                return (
                  String(option?.label ?? '').toLowerCase().includes(keyword) ||
                  String(option?.value ?? '').toLowerCase().includes(keyword)
                );
              }}
              onChange={(next) => onChange(next)}
            />
          </label>
        </div>
      </div>
    </div>
  );
}

export { TtsVoiceParamField };
