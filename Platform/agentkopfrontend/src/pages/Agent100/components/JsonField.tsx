import { useEffect, useState } from 'react';
import { Input } from 'antd';
import { prettyJson } from '../utils';
import styles from '../Agent100.module.less';

interface JsonFieldProps {
  value: unknown;
  onChange: (value: string) => void;
  disabled?: boolean;
  invalid?: boolean;
  placeholder?: string;
}

function toText(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value === undefined || value === null) return '';
  return prettyJson(value);
}

function JsonField({
  value,
  onChange,
  disabled = false,
  invalid = false,
  placeholder,
}: JsonFieldProps) {
  const [text, setText] = useState(() => toText(value));

  useEffect(() => {
    setText(toText(value));
  }, [value]);

  return (
    <Input.TextArea
      className={styles.jsonField}
      status={invalid ? 'error' : undefined}
      disabled={disabled}
      value={text}
      autoSize={{ minRows: 8, maxRows: 18 }}
      placeholder={placeholder}
      onChange={(event) => {
        setText(event.target.value);
        onChange(event.target.value);
      }}
    />
  );
}

export { JsonField };
