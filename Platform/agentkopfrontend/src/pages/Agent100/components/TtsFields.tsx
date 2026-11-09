/**
 * tts.provider / model / voiceId：选项来自 config.tts.providers[provider]
 */
import type { ReactNode } from 'react';
import { Select } from 'antd';
import {
  applyTtsProviderChange,
  resolveTtsModelOptions,
  resolveTtsProviderOptions,
  resolveTtsVoiceOptions,
} from '../ttsConfig';
import { fieldLabel, isPlainObject } from '../utils';
import type { JsonSchemaNode, SchemaFieldContext } from '../types';
import styles from '../Agent100.module.less';

interface TtsFieldsProps {
  schema: JsonSchemaNode;
  value: unknown;
  onChange: (value: unknown) => void;
  context: SchemaFieldContext;
  disabled?: boolean;
  invalidFields?: ReadonlySet<string>;
  path?: string;
}

function TtsFieldCard({
  name,
  required,
  invalid,
  children,
}: {
  name: string;
  required: boolean;
  invalid: boolean;
  children: ReactNode;
}) {
  return (
    <div className={`${styles.fieldCard} ${invalid ? styles.fieldCardInvalid : ''}`}>
      <div className={styles.fieldHead}>
        <span className={styles.fieldName}>
          {fieldLabel(name)}
          {required ? (
            <span className={styles.fieldRequiredMark} aria-hidden>
              *
            </span>
          ) : null}
        </span>
        <span className={required ? styles.fieldRequired : styles.fieldOptional}>
          {required ? '必填' : '可选'}
        </span>
      </div>
      <div className={styles.fieldControlWrap}>{children}</div>
    </div>
  );
}

function TtsFields({
  schema,
  value,
  onChange,
  context,
  disabled = false,
  invalidFields,
  path = 'tts',
}: TtsFieldsProps) {
  const requiredSet = new Set(schema.required ?? []);
  const obj = isPlainObject(value) ? value : {};
  const provider = typeof obj.provider === 'string' ? obj.provider : '';
  const model = typeof obj.model === 'string' ? obj.model : '';
  const voiceId = typeof obj.voiceId === 'string' ? obj.voiceId : '';

  const providerOptions = resolveTtsProviderOptions(context);
  const modelOptions = resolveTtsModelOptions(provider, context.tts);
  const voiceOptions = resolveTtsVoiceOptions(provider, context.tts);

  const setField = (key: string, next: string | undefined) => {
    onChange({ ...obj, [key]: next ?? '' });
  };

  return (
    <fieldset className={styles.fieldGroup}>
      <legend className={styles.fieldGroupTitle}>{fieldLabel('tts')}</legend>
      <TtsFieldCard
        name="provider"
        required={requiredSet.has('provider')}
        invalid={invalidFields?.has(`${path}.provider`) ?? false}
      >
        <Select
          showSearch
          optionFilterProp="label"
          allowClear={!requiredSet.has('provider')}
          disabled={disabled}
          status={invalidFields?.has(`${path}.provider`) ? 'error' : undefined}
          className={styles.fieldControl}
          placeholder={requiredSet.has('provider') ? '请选择必填项' : '请选择（可留空）'}
          value={provider || undefined}
          options={providerOptions}
          onChange={(next) => onChange(applyTtsProviderChange(context.tts, next ?? ''))}
        />
      </TtsFieldCard>
      <TtsFieldCard
        name="model"
        required={requiredSet.has('model')}
        invalid={invalidFields?.has(`${path}.model`) ?? false}
      >
        <Select
          showSearch
          optionFilterProp="label"
          allowClear={!requiredSet.has('model')}
          disabled={disabled || !provider}
          status={invalidFields?.has(`${path}.model`) ? 'error' : undefined}
          className={styles.fieldControl}
          placeholder={provider ? '请选择必填项' : `请先选择${fieldLabel('provider')}`}
          value={model || undefined}
          options={modelOptions}
          onChange={(next) => setField('model', next)}
        />
      </TtsFieldCard>
      <TtsFieldCard
        name="voiceId"
        required={requiredSet.has('voiceId')}
        invalid={invalidFields?.has(`${path}.voiceId`) ?? false}
      >
        <Select
          showSearch
          optionFilterProp="label"
          allowClear={!requiredSet.has('voiceId')}
          disabled={disabled || !provider}
          status={invalidFields?.has(`${path}.voiceId`) ? 'error' : undefined}
          className={styles.fieldControl}
          placeholder={provider ? '请选择必填项' : `请先选择${fieldLabel('provider')}`}
          value={voiceId || undefined}
          options={voiceOptions}
          onChange={(next) => setField('voiceId', next)}
        />
      </TtsFieldCard>
    </fieldset>
  );
}

export { TtsFields };
