import { Select, Spin } from 'antd';
import type { TtsComposerModel } from './useTtsComposer';
import styles from './TtsComposerControls.module.less';

interface TtsComposerControlsProps {
  model: TtsComposerModel;
  disabled?: boolean;
}

function TtsComposerControls({ model, disabled }: TtsComposerControlsProps) {
  const { engines, engine, voiceId, setEngine, setVoiceId, voices, isLoading } = model;

  if (isLoading && engines.length === 0) {
    return (
      <div className={styles.row} aria-label="语音合成参数">
        <Spin size="small" />
        <span className={styles.hint}>加载语音引擎…</span>
      </div>
    );
  }

  if (engines.length === 0) {
    return (
      <div className={styles.row} aria-label="语音合成参数">
        <p className={styles.hint}>暂无可用语音引擎，请稍后重试</p>
      </div>
    );
  }

  return (
    <div className={styles.row} aria-label="语音合成参数">
      <div className={styles.field}>
        <span className={styles.label}>语音引擎</span>
        <Select
          className={styles.select}
          size="middle"
          disabled={disabled}
          value={engine || undefined}
          options={engines.map((row) => ({
            value: row.engine,
            label: row.displayName,
          }))}
          onChange={setEngine}
        />
      </div>
      <div className={styles.field}>
        <span className={styles.label}>音色</span>
        <Select
          className={styles.select}
          size="middle"
          disabled={disabled || voices.length === 0}
          value={voiceId || undefined}
          showSearch
          optionFilterProp="label"
          options={voices.map((voice) => ({
            value: voice.voiceId,
            label: voice.displayName,
          }))}
          onChange={setVoiceId}
        />
      </div>
    </div>
  );
}

export { TtsComposerControls };
