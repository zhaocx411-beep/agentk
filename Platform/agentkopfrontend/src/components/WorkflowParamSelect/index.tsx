/**
 * Pipeline / 工具参数：模型字段来自 /agentk/models，可下拉也可手填
 */
import { AutoComplete, Input, Select } from 'antd';
import type { WorkflowParamDef } from '@/pages/AgentPlayground/types';
import { useModelCatalog } from '@/hooks/useModelCatalog';
import { selectOptionsForModelField } from '@/utils/models';

export interface ModelNameSelectProps {
  value?: string | number;
  onChange: (value: string) => void;
  options?: Array<{ value: string; label: string }>;
  loading?: boolean;
  placeholder?: string;
  className?: string;
  size?: 'small' | 'middle' | 'large';
  disabled?: boolean;
  allowClear?: boolean;
  status?: 'error' | 'warning';
}

function matchesKeyword(input: string, option?: { label?: unknown; value?: unknown }): boolean {
  const keyword = input.trim().toLowerCase();
  if (!keyword) return true;
  return (
    String(option?.label ?? '')
      .toLowerCase()
      .includes(keyword) ||
    String(option?.value ?? '')
      .toLowerCase()
      .includes(keyword)
  );
}

function withCurrentOption(
  options: Array<{ value: string; label: string }>,
  value: string,
): Array<{ value: string; label: string }> {
  if (!value || options.some((item) => item.value === value)) return options;
  return [{ value, label: value }, ...options];
}

/** 模型名：目录有选项时下拉选择，没有或未列出时也可手填 */
function ModelNameSelect({
  value,
  onChange,
  options = [],
  loading = false,
  placeholder = '请选择或输入模型',
  className,
  size,
  disabled,
  allowClear = true,
  status,
}: ModelNameSelectProps) {
  const text = value === undefined || value === null || value === '' ? '' : String(value);
  const listed = withCurrentOption(options, text);

  return (
    <AutoComplete
      value={text}
      onChange={(next) => onChange(next ?? '')}
      options={listed}
      className={className}
      disabled={disabled}
      style={{ width: '100%' }}
      filterOption={matchesKeyword}
      notFoundContent={loading ? '加载模型中…' : '暂无匹配项，可直接输入模型名'}
    >
      <Input
        allowClear={allowClear}
        size={size}
        status={status}
        placeholder={placeholder}
        disabled={disabled}
      />
    </AutoComplete>
  );
}

interface WorkflowParamSelectProps {
  param: WorkflowParamDef;
  value?: string | number;
  onChange: (value: string) => void;
  className?: string;
  size?: 'small' | 'middle' | 'large';
  disabled?: boolean;
}

function WorkflowParamSelect({
  param,
  value,
  onChange,
  className,
  size,
  disabled,
}: WorkflowParamSelectProps) {
  const { catalog, isLoading } = useModelCatalog({ enabled: !!param.optionsFrom });

  const options: Array<{ value: string; label: string }> = param.optionsFrom
    ? selectOptionsForModelField(catalog, param.optionsFrom, param.options)
    : (param.options ?? []).map((item) => ({ label: item.label, value: String(item.value) }));

  if (param.optionsFrom) {
    return (
      <ModelNameSelect
        value={value}
        onChange={onChange}
        options={options}
        loading={isLoading}
        placeholder={param.placeholder || '请选择或输入模型'}
        className={className}
        size={size}
        disabled={disabled}
        allowClear
      />
    );
  }

  return (
    <Select
      value={value === undefined || value === '' ? undefined : value}
      onChange={(v) => onChange(String(v))}
      options={options}
      placeholder={param.placeholder}
      className={className}
      size={size}
      disabled={disabled}
      allowClear
      showSearch
      optionFilterProp="label"
      filterOption={matchesKeyword}
    />
  );
}

export { ModelNameSelect, WorkflowParamSelect };
