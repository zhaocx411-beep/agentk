/**
 * 工具详情页：运行表单
 */
import { useEffect, useMemo, useState } from 'react';
import { Button, Input } from 'antd';
import { PlayCircleOutlined, StopOutlined } from '@ant-design/icons';
import type { OfficialToolItem } from '@/types/officialTools';
import { ParamField } from '@/pages/AgentCenter/AgentDetail/components/ParamField';
import { useToolTtsVoices } from '../../hooks/useToolTtsVoices';
import {
  annotateReferFeatureParam,
  argsToParamFormValues,
  collectToolDefaults,
  getInvalidToolRunFields,
  hasReferOrFeaturePair,
  initParamFormValues,
  isReferOrFeatureField,
  validateToolRunArgs,
  type ParamFormValues,
} from '../../lib/toolRunForm';
import { isTtsVoiceTool } from '../../lib/ttsVoices';
import { TOOL_GALLERY_TEXT } from '../../mock';
import { TtsVoiceParamField } from './TtsVoiceParamField';
import styles from '../../ToolGallery.module.less';

interface ToolRunFormProps {
  tool: OfficialToolItem;
  running: boolean;
  cancelling: boolean;
  onRun: (args: Record<string, unknown>, title?: string) => void;
  onCancel: () => void;
  prefillArgs?: Record<string, unknown> | null;
  prefillToken?: number;
}

function ToolRunForm({
  tool,
  running,
  cancelling,
  onRun,
  onCancel,
  prefillArgs,
  prefillToken = 0,
}: ToolRunFormProps) {
  const wantsVoiceList = isTtsVoiceTool(tool.name);
  const { engines: ttsEngines, loading: ttsLoading } = useToolTtsVoices(tool.name);
  const params = useMemo(() => {
    const list = tool.parameters ?? [];
    if (!wantsVoiceList || list.some((item) => item.name === 'voiceId')) return list;
    return [
      ...list,
      {
        name: 'voiceId',
        type: 'string',
        required: false,
        description: '不选则使用默认音色',
      },
    ];
  }, [tool.parameters, wantsVoiceList]);
  const [title, setTitle] = useState('');
  const [values, setValues] = useState<ParamFormValues>(() => initParamFormValues(params));
  const [formError, setFormError] = useState<string | null>(null);
  const [showValidation, setShowValidation] = useState(false);
  const [invalidFields, setInvalidFields] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    if (!prefillToken || !prefillArgs) return;
    setValues(argsToParamFormValues(params, prefillArgs));
    setShowValidation(false);
    setInvalidFields(new Set());
    setFormError(null);
  }, [params, prefillArgs, prefillToken]);

  const requiredCount = useMemo(() => params.filter((p) => p.required).length, [params]);
  const referFeaturePair = useMemo(() => hasReferOrFeaturePair(params), [params]);

  const clearParamInvalid = (paramName: string) => {
    setInvalidFields((prev) => {
      if (prev.size === 0) return prev;
      const next = new Set(prev);
      const names = referFeaturePair && isReferOrFeatureField(paramName)
        ? ['referImages', 'featureVideo']
        : [paramName];
      for (const name of names) {
        next.delete(name);
        for (const key of prev) {
          if (key.startsWith(`${name}[`)) next.delete(key);
        }
      }
      return next;
    });
  };

  const handleRun = () => {
    const nextInvalid = getInvalidToolRunFields(params, values);
    setInvalidFields(nextInvalid);
    setShowValidation(true);
    const error = validateToolRunArgs(params, values);
    if (error) {
      setFormError(error);
      return;
    }
    setFormError(null);
    onRun(collectToolDefaults(params, values), title);
  };

  return (
    <div className={styles.runForm}>
      <div className={styles.runFormHead}>
        <div>
          <h2 className={styles.paramSectionTitle}>运行</h2>
          <p className={styles.paramSectionHint}>
            填写入参后执行工具；{requiredCount > 0 ? `${requiredCount} 个必填参数` : '无必填参数'}
            {referFeaturePair ? ` ${TOOL_GALLERY_TEXT.referOrFeatureHint}` : ''}
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
            <Button type="primary" icon={<PlayCircleOutlined />} onClick={handleRun}>
              运行
            </Button>
          )}
        </div>
      </div>

      <div className={styles.runTitleField}>
        <label className={styles.runTitleLabel} htmlFor="tool-run-title">
          运行标题（可选）
        </label>
        <Input
          id="tool-run-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="便于在历史记录中识别，例如「测试海报生成」"
          disabled={running}
        />
      </div>

      {formError ? <p className={styles.runFormError}>{formError}</p> : null}

      {params.length === 0 ? (
        <p className={styles.paramEmpty}>该工具无额外入参，可直接运行</p>
      ) : (
        <div className={styles.runParamForm}>
          {params.map((param) => {
            const paramInvalid =
              showValidation &&
              (invalidFields.has(param.name) ||
                [...invalidFields].some((key) => key.startsWith(`${param.name}[`)));
            if (param.name === 'voiceId' && wantsVoiceList) {
              return (
                <TtsVoiceParamField
                  key={param.name}
                  param={param}
                  toolName={tool.name}
                  engines={ttsEngines}
                  loading={ttsLoading}
                  value={values[param.name]}
                  invalid={paramInvalid}
                  onChange={(v) => {
                    setValues((prev) => ({ ...prev, [param.name]: v }));
                    if (formError) setFormError(null);
                    clearParamInvalid(param.name);
                  }}
                />
              );
            }
            const fieldParam = referFeaturePair ? annotateReferFeatureParam(param) : param;
            return (
            <ParamField
              key={param.name}
              param={fieldParam}
              value={values[param.name]}
              invalid={paramInvalid}
              invalidFieldKeys={showValidation ? invalidFields : undefined}
              onChange={(v) => {
                setValues((prev) => ({ ...prev, [param.name]: v }));
                if (formError) setFormError(null);
                clearParamInvalid(param.name);
              }}
            />
            );
          })}
        </div>
      )}
    </div>
  );
}

export { ToolRunForm };
