/**
 * 添加工具时填写默认入参 → 写入 profile.toolDefaults.<tool>
 */
import { useEffect, useState, type MutableRefObject } from 'react';
import type { OfficialToolItem } from '@/types/officialTools';
import { ACCENT_CLASS } from '../lib/accentStyles';
import { getToolMeta } from '../lib/toolMeta';
import { ParamField } from './ParamField';
import {
  collectToolDefaults,
  initParamFormValues,
  type ParamFormValues,
} from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

interface ToolDefaultsFormProps {
  tool: OfficialToolItem;
  existingDefaults?: Record<string, unknown>;
  /** 暴露当前收集到的 defaults，供父级确认时读取 */
  valuesRef: MutableRefObject<Record<string, unknown>>;
}

function ToolDefaultsForm({
  tool,
  existingDefaults,
  valuesRef,
}: ToolDefaultsFormProps) {
  const meta = getToolMeta(tool.name);
  const Icon = meta.Icon;
  const params = tool.parameters ?? [];
  const [values, setValues] = useState<ParamFormValues>(() =>
    initParamFormValues(params, existingDefaults),
  );

  useEffect(() => {
    setValues(initParamFormValues(tool.parameters, existingDefaults));
  }, [tool.name, tool.parameters, existingDefaults]);

  useEffect(() => {
    valuesRef.current = collectToolDefaults(tool.parameters, values);
  }, [tool.parameters, values, valuesRef]);

  const requiredCount = params.filter((p) => p.required).length;
  const optionalCount = params.length - requiredCount;

  return (
    <div className={styles.toolConfigPreview}>
      <div className={styles.toolConfigHero}>
        <div className={`${styles.toolConfigHeroIcon} ${ACCENT_CLASS[meta.accent]}`}>
          <Icon />
        </div>
        <div className={styles.toolConfigHeroBody}>
          <div className={styles.toolConfigHeroTitle}>
            <span className={styles.toolConfigHeroName}>{tool.name}</span>
            <span className={styles.toolConfigHeroCat}>{tool.kind?.trim() || ''}</span>
          </div>
          <p className={styles.toolConfigHeroDesc}>{tool.description?.trim() || ''}</p>
        </div>
      </div>

      <div className={styles.toolConfigBlock}>
        <div className={styles.toolConfigBlockHead}>
          <h4 className={styles.toolConfigTitle}>默认参数</h4>
          {params.length > 0 ? (
            <span className={styles.toolConfigCount}>
              {requiredCount > 0 ? `${requiredCount} 必填` : null}
              {requiredCount > 0 && optionalCount > 0 ? ' · ' : null}
              {optionalCount > 0 ? `${optionalCount} 可选` : null}
            </span>
          ) : null}
        </div>
        {params.length === 0 ? (
          <p className={styles.toolConfigEmpty}>该工具无额外入参，确认后即可启用</p>
        ) : (
          <div className={styles.toolParamForm}>
            {params.map((param) => (
              <ParamField
                key={param.name}
                param={param}
                value={values[param.name]}
                onChange={(v) => {
                  setValues((prev) => ({ ...prev, [param.name]: v }));
                }}
              />
            ))}
          </div>
        )}
      </div>
      <p className={styles.toolConfigHint}>
        已填写的值会作为运行时默认入参；未填（含必填）则由 Agent 在对话中补齐。
      </p>
    </div>
  );
}

export { ToolDefaultsForm };
