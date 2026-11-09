import type { ReactNode } from 'react';
import { Form, Button, Space } from 'antd';
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons';
import styles from './SearchForm.module.less';

interface SearchFormProps {
  /** 搜索表单字段 */
  children?: ReactNode;
  /** 搜索回调 */
  onSearch?: (values: Record<string, unknown>) => void;
  /** 重置回调 */
  onReset?: () => void;
  /** 加载状态 */
  loading?: boolean;
}

/**
 * SearchForm - 通用搜索表单组件
 * 提供统一的搜索/重置操作栏
 *
 * @example
 * <SearchForm onSearch={handleSearch} onReset={handleReset}>
 *   <Form.Item name="keyword"><Input placeholder="关键字" /></Form.Item>
 * </SearchForm>
 */
function SearchForm({ children, onSearch, onReset, loading }: SearchFormProps) {
  const [form] = Form.useForm();

  const handleSearch = () => {
    const values = form.getFieldsValue();
    onSearch?.(values);
  };

  const handleReset = () => {
    form.resetFields();
    onReset?.();
  };

  return (
    <div className={styles.searchForm}>
      <Form form={form} layout="inline" onFinish={handleSearch}>
        {children}
        <Form.Item>
          <Space>
            <Button type="primary" icon={<SearchOutlined />} htmlType="submit" loading={loading}>
              搜索
            </Button>
            <Button icon={<ReloadOutlined />} onClick={handleReset}>
              重置
            </Button>
          </Space>
        </Form.Item>
      </Form>
    </div>
  );
}

export default SearchForm;
