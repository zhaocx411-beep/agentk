import { Input, InputNumber, Select, Switch } from 'antd';
import { ModelNameSelect } from '@/components';
import { inferModelOptionsFrom } from '@/utils/modelField';
import { AGENT100_MEDIA_FIELDS, AGENT100_TEXTAREA_FIELDS } from '../constants';
import { AGENT100_TEXT } from '../mock';
import { isComplexSchemaObject } from '../schemaForm';
import { fieldLabel, isPlainObject, resolveSelectOptions } from '../utils';
import type { JsonSchemaNode, SchemaFieldContext } from '../types';
import { JsonField } from './JsonField';
import { MediaUriField } from './MediaUriField';
import { TtsFields } from './TtsFields';
import styles from '../Agent100.module.less';

interface SchemaFieldProps {
  name: string;
  schema: JsonSchemaNode;
  required: boolean;
  value: unknown;
  onChange: (value: unknown) => void;
  context: SchemaFieldContext;
  disabled?: boolean;
  invalid?: boolean;
  invalidFields?: ReadonlySet<string>;
  path?: string;
}

function SchemaField({
  name,
  schema,
  required,
  value,
  onChange,
  context,
  disabled = false,
  invalid = false,
  invalidFields,
  path,
}: SchemaFieldProps) {
  const fieldPath = path || name;
  const label = fieldLabel(name);
  const options = resolveSelectOptions(name, schema, context);
  const fieldInvalid = invalid || (invalidFields?.has(fieldPath) ?? false);
  const modelKind = inferModelOptionsFrom(name);

  if (name === 'tts' && schema.type === 'object' && schema.properties) {
    return (
      <TtsFields
        schema={schema}
        value={value}
        onChange={onChange}
        context={context}
        disabled={disabled}
        invalidFields={invalidFields}
        path={fieldPath}
      />
    );
  }

  if (schema.type === 'object' && schema.properties && !isComplexSchemaObject(schema)) {
    const requiredSet = new Set(schema.required ?? []);
    const obj = isPlainObject(value) ? value : {};
    return (
      <fieldset className={styles.fieldGroup}>
        <legend className={styles.fieldGroupTitle}>{label}</legend>
        {Object.entries(schema.properties).map(([childName, childSchema]) => (
          <SchemaField
            key={childName}
            name={childName}
            schema={childSchema}
            required={requiredSet.has(childName)}
            value={obj[childName]}
            context={context}
            disabled={disabled}
            invalidFields={invalidFields}
            path={`${fieldPath}.${childName}`}
            onChange={(next) => onChange({ ...obj, [childName]: next })}
          />
        ))}
      </fieldset>
    );
  }

  const control = (() => {
    if (schema.type === 'object' || schema.type === 'array' || isComplexSchemaObject(schema)) {
      return (
        <JsonField
          value={value}
          disabled={disabled}
          invalid={fieldInvalid}
          placeholder={required ? AGENT100_TEXT.jsonRequired : AGENT100_TEXT.jsonOptional}
          onChange={onChange}
        />
      );
    }
    if (schema.type === 'boolean') {
      return (
        <div className={styles.switchRow}>
          <Switch
            checked={value === undefined || value === null ? false : Boolean(value)}
            disabled={disabled}
            onChange={onChange}
          />
          <span className={styles.switchLabel}>
            {value === undefined || value === null ? (required ? '请选择' : '未设置') : value ? '开启' : '关闭'}
          </span>
        </div>
      );
    }
    if (modelKind) {
      return (
        <ModelNameSelect
          allowClear
          disabled={disabled}
          status={fieldInvalid ? 'error' : undefined}
          className={styles.fieldControl}
          placeholder={required ? '请选择或输入模型' : '请选择或输入模型，可留空'}
          value={value === undefined || value === null || value === '' ? undefined : String(value)}
          options={options}
          onChange={(next) => onChange(next)}
        />
      );
    }
    if (options.length > 0) {
      const hasEmptyOption = options.some((item) => item.value === '');
      return (
        <Select
          showSearch
          optionFilterProp="label"
          allowClear={!required && !hasEmptyOption}
          disabled={disabled}
          status={fieldInvalid ? 'error' : undefined}
          className={styles.fieldControl}
          placeholder={required ? '请选择必填项' : '请选择（可留空）'}
          value={
            value === undefined || value === null
              ? hasEmptyOption
                ? ''
                : undefined
              : String(value)
          }
          options={options}
          onChange={(next) => onChange(next ?? (hasEmptyOption ? '' : undefined))}
        />
      );
    }
    if (schema.type === 'number' || schema.type === 'integer') {
      return (
        <InputNumber
          disabled={disabled}
          status={fieldInvalid ? 'error' : undefined}
          className={styles.fieldControl}
          value={typeof value === 'number' ? value : undefined}
          min={schema.minimum}
          max={schema.maximum}
          step={schema.type === 'integer' ? 1 : 0.1}
          placeholder={required ? '请输入必填数字' : '输入数字，可留空'}
          onChange={(next) => onChange(next)}
        />
      );
    }
    if (schema.type === 'string' && AGENT100_MEDIA_FIELDS.has(name)) {
      return (
        <MediaUriField
          value={typeof value === 'string' ? value : ''}
          disabled={disabled}
          invalid={fieldInvalid}
          onChange={onChange}
        />
      );
    }
    if (schema.type === 'string' && AGENT100_TEXTAREA_FIELDS.has(name)) {
      return (
        <Input.TextArea
          disabled={disabled}
          status={fieldInvalid ? 'error' : undefined}
          value={typeof value === 'string' ? value : ''}
          autoSize={{ minRows: 2, maxRows: 6 }}
          placeholder={required ? '请填写必填项' : '可留空'}
          onChange={(event) => onChange(event.target.value)}
        />
      );
    }
    return (
      <Input
        allowClear
        disabled={disabled}
        status={fieldInvalid ? 'error' : undefined}
        className={styles.fieldControl}
        value={value === undefined || value === null ? '' : String(value)}
        placeholder={required ? '请输入必填项' : '可留空'}
        onChange={(event) => onChange(event.target.value)}
      />
    );
  })();

  return (
    <div className={`${styles.fieldCard} ${fieldInvalid ? styles.fieldCardInvalid : ''}`}>
      <div className={styles.fieldHead}>
        <span className={styles.fieldName}>
          {label}
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
      <div className={styles.fieldControlWrap}>{control}</div>
    </div>
  );
}

export { SchemaField };
