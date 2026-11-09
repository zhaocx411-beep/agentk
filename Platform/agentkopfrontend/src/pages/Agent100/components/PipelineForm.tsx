import { useEffect, useMemo, useState } from 'react';
import { Button, Input } from 'antd';
import { PlayCircleOutlined, PlusOutlined, StopOutlined } from '@ant-design/icons';
import { AGENT100_TEXT } from '../mock';
import { toDocumentaryV2Args } from '../documentaryV2';
import {
  buildPipelineDefaults,
  collectPipelineArgs,
  getInvalidPipelineFields,
  normalizePrefillArgs,
  validatePipelineArgs,
  type PipelineFormValues,
} from '../schemaForm';
import { collectVoices } from '../utils';
import type { Agent100Config, Agent100Pipeline, SchemaFieldContext } from '../types';
import { SchemaField } from './SchemaField';
import styles from '../Agent100.module.less';

interface PipelineFormProps {
  pipeline: Agent100Pipeline;
  config: Agent100Config;
  running: boolean;
  cancelling: boolean;
  canRun: boolean;
  prefillArgs?: Record<string, unknown> | null;
  prefillToken?: number;
  hasSession?: boolean;
  onRun: (args: Record<string, unknown>, title?: string) => void;
  onCancel: () => void;
  onNewSession?: () => void;
}

function PipelineForm({
  pipeline,
  config,
  running,
  cancelling,
  canRun,
  hasSession = false,
  prefillArgs,
  prefillToken = 0,
  onRun,
  onCancel,
  onNewSession,
}: PipelineFormProps) {
  const [title, setTitle] = useState('');
  const [values, setValues] = useState<PipelineFormValues>(() =>
    buildPipelineDefaults(pipeline, config),
  );
  const [formError, setFormError] = useState<string | null>(null);
  const [invalidFields, setInvalidFields] = useState<Set<string>>(() => new Set());

  const context = useMemo<SchemaFieldContext>(
    () => ({
      fieldOptions: config.fieldOptions ?? {},
      voices: collectVoices(config.voices),
      modelOptions: config.models.options ?? {},
      tts: config.tts,
    }),
    [config],
  );

  const requiredCount = pipeline.inputSchema.required?.length ?? 0;
  const properties = pipeline.inputSchema.properties ?? {};
  const requiredSet = new Set(pipeline.inputSchema.required ?? []);

  useEffect(() => {
    if (!prefillToken || !prefillArgs) return;
    setValues((prev) => ({
      ...prev,
      ...normalizePrefillArgs(pipeline.inputSchema, prefillArgs),
    }));
    setFormError(null);
    setInvalidFields(new Set());
  }, [prefillArgs, prefillToken, pipeline.inputSchema]);

  const handleRun = () => {
    const nextInvalid = getInvalidPipelineFields(pipeline.inputSchema, values);
    setInvalidFields(nextInvalid);
    const error = validatePipelineArgs(pipeline.inputSchema, values);
    if (error) {
      setFormError(error);
      return;
    }
    setFormError(null);
    onRun(
      toDocumentaryV2Args(pipeline.name, collectPipelineArgs(pipeline.inputSchema, values), config),
      title,
    );
  };

  return (
    <div className={styles.runForm}>
      <div className={styles.runFormHead}>
        <div>
          <h2 className={styles.sectionTitle}>
            {AGENT100_TEXT.runTitle} · {pipeline.displayName}
          </h2>
          <p className={styles.sectionHint}>
            {AGENT100_TEXT.runHint} {AGENT100_TEXT.requiredHint(requiredCount)}
            {hasSession ? ` ${AGENT100_TEXT.newSessionHint}` : ''}
          </p>
        </div>
        <div className={styles.runFormActions}>
          {running ? (
            <Button
              danger
              icon={<StopOutlined />}
              loading={cancelling}
              onClick={() => void onCancel()}
            >
              取消执行
            </Button>
          ) : (
            <>
              {onNewSession ? (
                <Button icon={<PlusOutlined />} onClick={onNewSession}>
                  {AGENT100_TEXT.newSession}
                </Button>
              ) : null}
              <Button
                type="primary"
                icon={<PlayCircleOutlined />}
                disabled={!canRun}
                onClick={handleRun}
              >
                运行
              </Button>
            </>
          )}
        </div>
      </div>

      <div className={styles.runTitleField}>
        <label className={styles.runTitleLabel} htmlFor="agent100-run-title">
          {AGENT100_TEXT.runNameLabel}
        </label>
        <Input
          id="agent100-run-title"
          value={title}
          disabled={running}
          placeholder={AGENT100_TEXT.runNamePlaceholder}
          onChange={(event) => setTitle(event.target.value)}
        />
      </div>

      {formError ? <p className={styles.runFormError}>{formError}</p> : null}

      {Object.keys(properties).length === 0 ? (
        <p className={styles.paramEmpty}>{AGENT100_TEXT.runEmpty}</p>
      ) : (
        <div className={styles.runParamForm}>
          {Object.entries(properties).map(([name, schema]) => (
            <SchemaField
              key={name}
              name={name}
              schema={schema}
              required={requiredSet.has(name)}
              value={values[name]}
              context={context}
              disabled={running}
              invalidFields={invalidFields}
              onChange={(next) => {
                setValues((prev) => ({ ...prev, [name]: next }));
                if (formError) setFormError(null);
                setInvalidFields((prev) => {
                  if (!prev.has(name)) return prev;
                  const copy = new Set(prev);
                  copy.delete(name);
                  return copy;
                });
              }}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export { PipelineForm };
