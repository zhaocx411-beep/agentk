/**
 * 按 executionSpecs / schemaFields 的字段声明渲染一个入参控件
 * 换执行方式时表单结构随声明变化，这里不认识任何具体字段名
 * 未知 control / 异常 value 一律降级为文本，避免白屏
 */
import { Button, Input, InputNumber, Select, Space } from 'antd';
import { CloseCircleFilled, MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';
import type { FieldSpec, FieldValue } from '../lib/executionSpecs';
import styles from '../CreationStudio.module.less';

const { TextArea } = Input;

interface ArgFieldProps {
  field: FieldSpec;
  /** 是否把字段的真实键名标在右上角；agent-loop 的 text 拼装无需暴露 */
  showKey: boolean;
  value: FieldValue;
  onChange: (value: FieldValue) => void;
  /** select 控件的选项；来自 schema enum，没有则手填 */
  selectOptions?: { value: string; label: string }[];
  selectLoading?: boolean;
}

function asStringList(value: FieldValue): string[] {
  if (!Array.isArray(value)) return [];
  try {
    return value.map((item) => (item == null ? '' : String(item)));
  } catch {
    return [];
  }
}

function asDisplayText(value: FieldValue): string {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}

function asSafeNumber(value: FieldValue): number | undefined {
  if (value === '' || value == null) return undefined;
  const num = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(num) ? num : undefined;
}

function ArgField({
  field,
  showKey,
  value,
  onChange,
  selectOptions = [],
  selectLoading = false,
}: ArgFieldProps) {
  if (!field?.key) return null;

  const control = field.control || 'text';
  const list = asStringList(value);
  const safeOptions = Array.isArray(selectOptions)
    ? selectOptions.filter((item) => item && item.value != null)
    : [];

  const textValue = asDisplayText(value);

  return (
    <div className={styles.field}>
      <div className={styles.fieldHead}>
        <span className={styles.fieldLabel}>
          {field.label || field.key}
          {field.required ? <span className={styles.requiredMark}>*</span> : null}
        </span>
        {showKey && <span className={styles.argKey}>{field.key}</span>}
      </div>
      {field.hint ? <p className={styles.fieldTip}>{field.hint}</p> : null}

      {control === 'textarea' && (
        <div className={`${styles.textareaWrap} ${styles.textareaWrapCounted}`}>
          <TextArea
            value={textValue}
            onChange={(e) => onChange(e.target.value)}
            autoSize={{ minRows: 3, maxRows: 10 }}
            placeholder={field.placeholder}
            className={styles.textareaInput}
            allowClear
          />
          <span className={styles.charCount}>{textValue.length} 字</span>
        </div>
      )}

      {control === 'text' && (
        <Input
          size="large"
          value={textValue}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.placeholder}
          className={styles.textInput}
          allowClear
        />
      )}

      {control === 'number' && (
        <div className={styles.clearableNumber}>
          <InputNumber
            size="large"
            className={`${styles.fullWidth} ${styles.numberInput}`}
            value={asSafeNumber(value)}
            min={field.min}
            max={field.max}
            precision={0}
            controls
            onChange={(next) => {
              if (next == null || !Number.isFinite(next)) {
                onChange('');
                return;
              }
              onChange(next);
            }}
          />
          {value !== '' && value != null ? (
            <button
              type="button"
              className={styles.inputClear}
              aria-label="清空"
              onClick={() => onChange('')}
            >
              <CloseCircleFilled />
            </button>
          ) : null}
        </div>
      )}

      {control === 'select' &&
        (safeOptions.length > 0 ? (
          <Select
            className={`${styles.fullWidth} ${styles.selectInput}`}
            showSearch
            allowClear
            loading={selectLoading}
            value={value === '' || value == null ? undefined : String(value)}
            onChange={(next) => onChange(next ?? '')}
            options={safeOptions}
            placeholder={field.placeholder ?? '请选择'}
            notFoundContent={selectLoading ? '加载中…' : '暂无可选项'}
            optionFilterProp="label"
            size="large"
          />
        ) : (
          <Input
            size="large"
            allowClear
            value={textValue}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.placeholder ?? '请输入'}
            className={styles.textInput}
          />
        ))}

      {control === 'array' && (
        <div className={styles.arrayField}>
          <Space direction="vertical" className={styles.fullWidth} size={8}>
            {list.map((item, index) => (
              <div key={`${field.key}-${index}`} className={styles.arrayRow}>
                <Input
                  value={item}
                  placeholder={field.placeholder ?? `第 ${index + 1} 项`}
                  allowClear
                  onChange={(e) => {
                    const next = [...list];
                    next[index] = e.target.value;
                    onChange(next);
                  }}
                />
                <Button
                  type="text"
                  danger
                  icon={<MinusCircleOutlined />}
                  aria-label="删除该项"
                  onClick={() => onChange(list.filter((_, i) => i !== index))}
                />
              </div>
            ))}
            <Button
              type="dashed"
              block
              icon={<PlusOutlined />}
              onClick={() => onChange([...list, ''])}
            >
              添加一项
            </Button>
          </Space>
        </div>
      )}

      {control === 'json' && (
        <TextArea
          value={asDisplayText(value)}
          onChange={(e) => onChange(e.target.value)}
          autoSize={{ minRows: 4, maxRows: 16 }}
          placeholder={field.placeholder ?? '{"key": "value"}'}
          className={styles.monoInput}
          allowClear
        />
      )}

      {/* 未知 control：降级文本，保证能填能传 */}
      {!['textarea', 'text', 'number', 'select', 'array', 'json'].includes(control) && (
        <Input
          value={asDisplayText(value)}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.placeholder || field.key}
          allowClear
        />
      )}
    </div>
  );
}

export default ArgField;
