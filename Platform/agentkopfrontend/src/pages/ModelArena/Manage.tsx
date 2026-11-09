/**
 * 模型对战管理（Mock 列表）
 */
import { useMemo, useState } from 'react';
import { Alert, Button, Form, Input, Select, Space } from 'antd';
import { RiSwordLine } from 'react-icons/ri';
import { Link } from 'react-router-dom';
import { PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons';
import PageContainer from '@/components/PageContainer';
import DataTable from '@/components/DataTable';
import { getArenaManageColumns } from './columns';
import { ArenaBattleDetailModal } from './components/ArenaBattleDetailModal';
import { MOCK_BATTLE_RECORDS } from './mock';
import type { ArenaBattleRecord } from './types';
import styles from './Manage.module.less';

const STATUS_OPTIONS = [
  { label: '全部状态', value: '' },
  { label: '草稿', value: 'draft' },
  { label: '进行中', value: 'running' },
  { label: '投票中', value: 'voting' },
  { label: '已完成', value: 'completed' },
  { label: '已失效', value: 'invalid' },
];

function ArenaManage() {
  const [form] = Form.useForm();
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [detailRecord, setDetailRecord] = useState<ArenaBattleRecord | null>(null);

  const filtered = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    return MOCK_BATTLE_RECORDS.filter((record) => {
      if (status && record.status !== status) return false;
      if (!q) return true;
      return (
        record.id.toLowerCase().includes(q) ||
        record.title.toLowerCase().includes(q) ||
        record.modelA.toLowerCase().includes(q) ||
        record.modelB.toLowerCase().includes(q) ||
        record.creator.toLowerCase().includes(q)
      );
    });
  }, [keyword, status]);

  const pageData = useMemo(() => {
    const start = (page - 1) * pageSize;
    return filtered.slice(start, start + pageSize);
  }, [filtered, page, pageSize]);

  const columns = useMemo(
    () => getArenaManageColumns({ onViewDetail: setDetailRecord }),
    [],
  );

  const handleSearch = () => {
    const values = form.getFieldsValue();
    setKeyword(values.keyword ?? '');
    setStatus(values.status ?? '');
    setPage(1);
  };

  const handleReset = () => {
    form.resetFields();
    setKeyword('');
    setStatus('');
    setPage(1);
  };

  return (
    <PageContainer
      title="模型对战管理"
      extra={
        <Space>
          <Link to="/model-arena">
            <Button type="primary" icon={<PlusOutlined />}>
              新建对战
            </Button>
          </Link>
        </Space>
      }
    >
      <div className={styles.page}>
        <div className={styles.subtitle}>
          <RiSwordLine className={styles.subtitleIcon} />
          管理匿名对战轮次、投票结果与模型组合（当前为前端 Mock 数据）
        </div>

        <Alert
          type="info"
          showIcon
          className={styles.mockHint}
          message="Mock 模式"
          description="列表为历史轮次归档；「详情」查看完整问答与投票，「进入对战」按状态继续或回看。右上角「新建对战」会开启全新匿名轮次。"
        />

        <Form form={form} layout="inline" className={styles.toolbar} onFinish={handleSearch}>
          <Form.Item name="keyword">
            <Input allowClear placeholder="搜索 ID / 标题 / 模型 / 创建人" style={{ width: 280 }} />
          </Form.Item>
          <Form.Item name="status" initialValue="">
            <Select options={STATUS_OPTIONS} style={{ width: 140 }} />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" icon={<SearchOutlined />}>
                查询
              </Button>
              <Button icon={<ReloadOutlined />} onClick={handleReset}>
                重置
              </Button>
            </Space>
          </Form.Item>
        </Form>

        <div className={styles.tableWrap}>
          <DataTable<ArenaBattleRecord>
            rowKey="id"
            columns={columns}
            dataSource={pageData}
            pagination={{
              current: page,
              pageSize,
              total: filtered.length,
              onChange: (nextPage, nextSize) => {
                setPage(nextPage);
                if (nextSize) setPageSize(nextSize);
              },
            }}
            scroll={{ x: 1280 }}
          />
        </div>
      </div>

      <ArenaBattleDetailModal
        record={detailRecord}
        open={Boolean(detailRecord)}
        onClose={() => setDetailRecord(null)}
      />
    </PageContainer>
  );
}

export default ArenaManage;
