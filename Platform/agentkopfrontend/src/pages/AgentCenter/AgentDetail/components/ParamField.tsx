/**
 * 工具默认参数单个控件：标量 / 枚举 / URL 列表（支持本地上传）
 * 选项只认工具 parameters.enumValues（或调用方传入的 options），不请求 /agentk/models
 */
import { Input, InputNumber, Select, Switch } from 'antd';
import type { OfficialToolParameter } from '@/types/officialTools';
import {
  formatParamForInput,
  isArrayStringParam,
  isComplexParam,
  isSchemaObjectArrayParam,
  isSchemaObjectParam,
  supportsLocalMediaUpload,
} from '../lib/toolDefaultsUtils';
import { ObjectArrayParamEditor, SchemaObjectParamEditor } from './ObjectArrayParamEditor';
import { ToolParamMediaPicker } from './ToolParamMediaPicker';
import styles from '../AgentDetail.module.less';

interface ParamFieldProps {
  param: OfficialToolParameter;
  value: unknown;
  onChange: (v: unknown) => void;
  invalid?: boolean;
  invalidFieldKeys?: ReadonlySet<string>;
  /** 覆盖 enumValues，用于目录接口下发的选项（如 TTS 音色） */
  options?: Array<{ value: string; label: string }>;
  optionsLoading?: boolean;
}

function formatDefaultValue(value: OfficialToolParameter['defaultValue']): string {
  if (value === undefined || value === null) return '—';
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  return String(value);
}

function ParamFieldChildren({ parameters }: { parameters: OfficialToolParameter[] }) {
  if (parameters.length === 0) return null;
  return (
    <ul className={styles.toolParamChildrenList}>
      {parameters.map((child) => (
        <li key={child.name} className={styles.toolParamChildItem}>
          <div className={styles.toolParamChildHead}>
            <code className={styles.toolParamChildName}>{child.name}</code>
            <span className={styles.toolParamType}>{child.type}</span>
            <span className={child.required ? styles.toolParamRequired : styles.toolParamOptional}>
              {child.required ? '必填' : '可选'}
            </span>
          </div>
          {child.description ? <p className={styles.toolParamDesc}>{child.description}</p> : null}
          {child.children?.length ? <ParamFieldChildren parameters={child.children} /> : null}
        </li>
      ))}
    </ul>
  );
}

function ParamField({
  param,
  value,
  onChange,
  invalid,
  invalidFieldKeys,
  options,
  optionsLoading = false,
}: ParamFieldProps) {
  const optionalHint = param.required ? '必填' : '可留空';
  const schemaObjectArray = isSchemaObjectArrayParam(param);
  const schemaObject = isSchemaObjectParam(param);
  const enumOptions = param.enumValues?.length
    ? param.enumValues.map((item) => ({
        value: String(item),
        label: String(item),
      }))
    : undefined;
  const selectOptions = options ?? enumOptions;
  const useSelect = options !== undefined || Boolean(enumOptions?.length);
  const uploadControl = (
    <ToolParamMediaPicker param={param} value={value} onChange={onChange} invalid={invalid} />
  );

  const control = (() => {
    if (schemaObjectArray) {
      return (
        <ObjectArrayParamEditor
          param={param}
          value={value}
          onChange={onChange}
          invalidFieldKeys={invalidFieldKeys}
        />
      );
    }
    if (schemaObject) {
      return (
        <SchemaObjectParamEditor
          param={param}
          value={value}
          onChange={onChange}
          invalidFieldKeys={invalidFieldKeys}
        />
      );
    }
    if (param.type === 'boolean') {
      return (
        <div className={styles.toolParamSwitchRow}>
          <Switch
            checked={value === undefined || value === null ? false : Boolean(value)}
            onChange={onChange}
          />
          <span
            className={
              invalid && param.required ? styles.toolParamSwitchLabelInvalid : styles.toolParamSwitchLabel
            }
          >
            {value === undefined || value === null
              ? param.required
                ? '请选择'
                : '未设置'
              : value
                ? '开启'
                : '关闭'}
          </span>
        </div>
      );
    }
    if (useSelect) {
      return (
        <Select
          allowClear={!param.required}
          showSearch
          optionFilterProp="label"
          size="large"
          loading={optionsLoading}
          status={invalid ? 'error' : undefined}
          className={styles.toolParamControl}
          placeholder={param.required ? '请选择必填项' : '请选择，可留空'}
          filterOption={(input, option) => {
            const keyword = input.trim().toLowerCase();
            if (!keyword) return true;
            return (
              String(option?.label ?? '').toLowerCase().includes(keyword) ||
              String(option?.value ?? '').toLowerCase().includes(keyword)
            );
          }}
          value={value === undefined || value === null || value === '' ? undefined : String(value)}
          onChange={(v) => onChange(v)}
          options={selectOptions}
        />
      );
    }
    if (param.type === 'number' || param.type === 'integer') {
      return (
        <InputNumber
          size="large"
          status={invalid ? 'error' : undefined}
          className={styles.toolParamFull}
          value={typeof value === 'number' ? value : undefined}
          onChange={(v) => onChange(v)}
          placeholder={param.required ? '请输入必填数字' : `输入数字，${optionalHint}`}
        />
      );
    }
    if (param.type === 'string' && supportsLocalMediaUpload(param)) {
      return uploadControl;
    }
    if (isArrayStringParam(param)) {
      if (supportsLocalMediaUpload(param)) {
        return uploadControl;
      }
      return (
        <Input.TextArea
          rows={3}
          status={invalid ? 'error' : undefined}
          value={formatParamForInput(param, value)}
          onChange={(e) => onChange(e.target.value)}
          placeholder={
            param.required
              ? '请填写必填项：JSON 对象/数组，或每行一个'
              : 'JSON 对象/数组，或每行一个、逗号分隔；可留空'
          }
          className={styles.toolParamTextarea}
        />
      );
    }
    if (isComplexParam(param)) {
      if (supportsLocalMediaUpload(param)) {
        return uploadControl;
      }
      return (
        <Input.TextArea
          rows={4}
          status={invalid ? 'error' : undefined}
          value={formatParamForInput(param, value)}
          onChange={(e) => onChange(e.target.value)}
          placeholder={
            param.required
              ? '请填写必填 JSON，例如 [{"url":"..."}]'
              : '填写 JSON，例如 [{"url":"..."}]；可留空'
          }
          className={`${styles.toolParamTextarea} ${styles.toolParamMono}`}
        />
      );
    }
    return (
      <Input
        allowClear
        size="large"
        status={invalid ? 'error' : undefined}
        value={value === undefined || value === null ? '' : String(value)}
        onChange={(e) => onChange(e.target.value)}
        placeholder={param.required ? '请输入必填默认值' : '输入默认值，可留空'}
        className={styles.toolParamControl}
      />
    );
  })();

  return (
    <div className={`${styles.toolParamCard} ${invalid ? styles.toolParamCardInvalid : ''}`}>
      <div className={styles.toolParamCardHead}>
        <span className={styles.toolParamName}>
          {param.name}
          {param.required ? (
            <span className={styles.toolParamRequiredMark} aria-hidden>
              *
            </span>
          ) : null}
        </span>
        <span className={param.required ? styles.toolParamRequired : styles.toolParamOptional}>
          {param.required ? '必填' : '可选'}
        </span>
        <span className={styles.toolParamType}>{param.type}</span>
      </div>
      {param.description ? <p className={styles.toolParamDesc}>{param.description}</p> : null}
      {(param.defaultValue !== undefined && param.defaultValue !== null) ||
      (param.enumValues && param.enumValues.length > 0) ? (
        <div className={styles.toolParamMeta}>
          {param.defaultValue !== undefined && param.defaultValue !== null ? (
            <span>
              默认值：<em>{formatDefaultValue(param.defaultValue)}</em>
            </span>
          ) : null}
          {param.enumValues && param.enumValues.length > 0 && options === undefined ? (
            <span className={styles.toolParamEnums}>
              可选值：
              {param.enumValues.map((item) => (
                <code key={String(item)} className={styles.toolParamEnum}>
                  {String(item)}
                </code>
              ))}
            </span>
          ) : null}
        </div>
      ) : null}
      {param.children && param.children.length > 0 && !schemaObjectArray && !schemaObject ? (
        <div className={styles.toolParamChildren}>
          <p className={styles.toolParamChildrenLabel}>子字段</p>
          <ParamFieldChildren parameters={param.children} />
        </div>
      ) : null}
      <div className={styles.toolParamControlWrap}>{control}</div>
    </div>
  );
}

export { ParamField };
