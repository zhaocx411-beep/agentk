/**
 * array<object> / object 结构化编辑：按 children schema 渲染子字段控件
 */
import { useRef, useState, type ReactNode } from 'react';
import { Button, Input, InputNumber, Select, Switch } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import type { OfficialToolParameter } from '@/types/officialTools';
import { uploadMedia, validateMediaUploadFile } from '@/services/media';
import { message } from '@/utils/feedback';
import {
  acceptForMediaParam,
  createEmptyObjectItem,
  isChildValueEmpty,
  isMediaUrlChildField,
  mediaKindsHint,
  mediaKindsForParam,
  parseObjectArrayValue,
  parseObjectValue,
} from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

interface ObjectArrayParamEditorProps {
  param: OfficialToolParameter;
  value: unknown;
  onChange: (value: unknown) => void;
  invalidFieldKeys?: ReadonlySet<string>;
}

interface ChildFieldControlProps {
  child: OfficialToolParameter;
  parentParam: OfficialToolParameter;
  value: unknown;
  onChange: (value: unknown) => void;
  invalid?: boolean;
  fieldKey?: string;
}

function ChildFieldControl({ child, parentParam, value, onChange, invalid }: ChildFieldControlProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const urlField = isMediaUrlChildField(child, parentParam);
  const syntheticParam: OfficialToolParameter = {
    ...child,
    description: child.description ?? parentParam.description,
  };

  const handleUpload = async (files: File[]) => {
    const validFiles = files.filter((file) => {
      const validationError = validateMediaUploadFile(file);
      if (validationError) message.error(validationError);
      return !validationError;
    });
    if (validFiles.length === 0) return;
    setUploading(true);
    try {
      const result = await uploadMedia(validFiles[0]);
      const url = result.url;
      if (!url) {
        message.error('上传成功但未拿到文件地址');
        return;
      }
      onChange(url);
      message.success('上传成功');
    } catch (error: unknown) {
      message.error(error instanceof Error ? error.message : '上传失败');
    } finally {
      setUploading(false);
    }
  };

  let control: ReactNode;
  if (child.type === 'boolean') {
    const checked = value === undefined || value === null ? false : Boolean(value);
    control = (
      <div className={styles.toolParamSwitchRow}>
        <Switch checked={checked} onChange={onChange} />
        <span
          className={
            invalid && child.required ? styles.toolParamSwitchLabelInvalid : styles.toolParamSwitchLabel
          }
        >
          {checked ? '开启' : '关闭'}
        </span>
      </div>
    );
  } else if (child.enumValues?.length) {
    control = (
      <Select
        allowClear={!child.required}
        size="middle"
        status={invalid ? 'error' : undefined}
        className={styles.toolParamControl}
        placeholder={child.required ? '请选择' : '可选'}
        value={value === undefined || value === null || value === '' ? undefined : String(value)}
        onChange={onChange}
        options={child.enumValues.map((item) => ({
          value: String(item),
          label: String(item),
        }))}
      />
    );
  } else if (child.type === 'number' || child.type === 'integer') {
    control = (
      <InputNumber
        size="middle"
        status={invalid ? 'error' : undefined}
        className={styles.toolParamFull}
        value={typeof value === 'number' ? value : undefined}
        onChange={onChange}
        placeholder={child.required ? '必填' : '可选'}
      />
    );
  } else if (urlField) {
    const kinds = mediaKindsForParam(syntheticParam);
    const kindsHint = mediaKindsHint(kinds);
    control = (
      <div className={styles.objectArrayUrlField}>
        <Input
          allowClear
          size="middle"
          status={invalid ? 'error' : undefined}
          value={value === undefined || value === null ? '' : String(value)}
          onChange={(e) => onChange(e.target.value)}
          placeholder={child.required ? '粘贴或上传音频 URL' : '可选 URL'}
          className={styles.toolParamControl}
        />
        <input
          ref={inputRef}
          type="file"
          accept={acceptForMediaParam(syntheticParam)}
          className={styles.toolParamFileInput}
          disabled={uploading}
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            event.target.value = '';
            void handleUpload(files);
          }}
        />
        <button
          type="button"
          className={styles.toolParamLocalUpload}
          disabled={uploading}
          onClick={() => inputRef.current?.click()}
        >
          {uploading ? '上传中…' : '本地上传'}
        </button>
        <span className={styles.toolParamUploadHint}>支持本地或历史{kindsHint}</span>
      </div>
    );
  } else {
    control = (
      <Input
        allowClear
        size="middle"
        status={invalid ? 'error' : undefined}
        value={value === undefined || value === null ? '' : String(value)}
        onChange={(e) => onChange(e.target.value)}
        placeholder={child.required ? '必填' : '可选'}
        className={styles.toolParamControl}
      />
    );
  }

  return (
    <div className={styles.objectArrayChildField}>
      <div className={styles.objectArrayChildHead}>
        <span className={styles.objectArrayChildName}>
          {child.name}
          {child.required ? (
            <span className={styles.toolParamRequiredMark} aria-hidden>
              *
            </span>
          ) : null}
        </span>
        <span className={child.required ? styles.toolParamRequired : styles.toolParamOptional}>
          {child.required ? '必填' : '可选'}
        </span>
        <span className={styles.toolParamType}>{child.type}</span>
      </div>
      {child.description ? <p className={styles.toolParamDesc}>{child.description}</p> : null}
      {control}
    </div>
  );
}

function ObjectArrayParamEditor({ param, value, onChange, invalidFieldKeys }: ObjectArrayParamEditorProps) {
  const children = param.children ?? [];
  const items = parseObjectArrayValue(value);

  const fieldKeyFor = (index: number, childName: string) => `${param.name}[${index + 1}].${childName}`;

  const isFieldInvalid = (index: number, childName: string) =>
    invalidFieldKeys?.has(fieldKeyFor(index, childName)) ?? false;

  const isItemInvalid = (index: number) =>
    children.some((child) => child.required && isFieldInvalid(index, child.name));

  const updateItems = (next: Record<string, unknown>[]) => {
    onChange(next);
  };

  const addItem = () => {
    updateItems([...items, createEmptyObjectItem(children)]);
  };

  const removeItem = (index: number) => {
    updateItems(items.filter((_, i) => i !== index));
  };

  const updateItemField = (index: number, field: string, fieldValue: unknown) => {
    const next = items.map((item, i) => {
      if (i !== index) return item;
      const row = { ...item };
      if (
        fieldValue === undefined ||
        fieldValue === null ||
        (typeof fieldValue === 'string' && !fieldValue.trim())
      ) {
        delete row[field];
      } else {
        row[field] = fieldValue;
      }
      return row;
    });
    updateItems(next);
  };

  const itemLabel = param.name.includes('track')
    ? '音轨'
    : param.name.includes('clip')
      ? '片段'
      : '项';

  return (
    <div className={styles.objectArrayEditor}>
      {items.length === 0 ? (
        <p className={styles.objectArrayEmpty}>暂无{itemLabel}，点击下方按钮添加</p>
      ) : (
        <div className={styles.objectArrayList}>
          {items.map((item, index) => (
            <div
              key={`${param.name}-${index}`}
              className={`${styles.objectArrayItem}${isItemInvalid(index) ? ` ${styles.objectArrayItemInvalid}` : ''}`}
            >
              <div className={styles.objectArrayItemHead}>
                <span className={styles.objectArrayItemTitle}>
                  {itemLabel} {index + 1}
                </span>
                <Button
                  type="text"
                  danger
                  size="small"
                  icon={<DeleteOutlined />}
                  onClick={() => removeItem(index)}
                >
                  删除
                </Button>
              </div>
              <div className={styles.objectArrayItemFields}>
                {children.map((child) => (
                  <ChildFieldControl
                    key={child.name}
                    child={child}
                    parentParam={param}
                    value={item[child.name]}
                    invalid={isFieldInvalid(index, child.name)}
                    onChange={(fieldValue) => updateItemField(index, child.name, fieldValue)}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      <Button type="dashed" block icon={<PlusOutlined />} onClick={addItem}>
        添加{itemLabel}
      </Button>
    </div>
  );
}

interface SchemaObjectParamEditorProps {
  param: OfficialToolParameter;
  value: unknown;
  onChange: (value: unknown) => void;
  invalidFieldKeys?: ReadonlySet<string>;
}

function SchemaObjectParamEditor({ param, value, onChange, invalidFieldKeys }: SchemaObjectParamEditorProps) {
  const children = param.children ?? [];
  const item = parseObjectValue(value);

  const isFieldInvalid = (_childName: string) => invalidFieldKeys?.has(param.name) ?? false;

  const updateField = (field: string, fieldValue: unknown) => {
    const next = { ...item };
    if (
      fieldValue === undefined ||
      fieldValue === null ||
      (typeof fieldValue === 'string' && !fieldValue.trim())
    ) {
      delete next[field];
    } else {
      next[field] = fieldValue;
    }
    onChange(next);
  };

  return (
    <div className={styles.schemaObjectNest} role="group" aria-label={`${param.name} 子字段`}>
      {children.map((child) => (
        <ChildFieldControl
          key={child.name}
          child={child}
          parentParam={param}
          value={item[child.name]}
          invalid={isFieldInvalid(child.name) && child.required && isChildValueEmpty(child, item[child.name])}
          onChange={(fieldValue) => updateField(child.name, fieldValue)}
        />
      ))}
    </div>
  );
}

export { ObjectArrayParamEditor, SchemaObjectParamEditor };
