/**
 * 权限控制页面
 *
 * 管理平台管理员名单：查看、新增、删除权限
 *
 * src/pages/PermissionControl/
 * ├── index.tsx                        # 页面主组件
 * ├── api.ts                          # 接口请求
 * ├── types.ts                        # 类型定义
 * └── PermissionControl.module.less   # 页面样式
 */

import { useState, useCallback, useEffect, useRef } from 'react';
import { Button, Table, Tag, Space, Modal, Form, Input, Select, Popconfirm } from 'antd';
import { message } from '@/utils/feedback';
import { PlusOutlined, ReloadOutlined, DeleteOutlined } from '@ant-design/icons';
import { RiShieldLine } from 'react-icons/ri';
import PageContainer from '@/components/PageContainer';
import { getAdminUsers, addAdmin, removeAdmin } from './api';
import type { AdminUser, AddAdminParams } from './types';
import { ROLE_OPTIONS } from './constants';
import styles from './PermissionControl.module.less';

/**
 * 权限控制页面
 * 管理平台管理员名单：查看、新增、删除
 */
function PermissionControl() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<AdminUser[]>([]);
  const [activeCount, setActiveCount] = useState(0);
  const [addModalOpen, setAddModalOpen] = useState(false);
  const [addLoading, setAddLoading] = useState(false);
  const [hasScroll, setHasScroll] = useState(false);
  const [form] = Form.useForm();
  const tableBodyRef = useRef<HTMLDivElement | null>(null);

  /** 拉取管理员名单 */
  const fetchList = useCallback(async () => {
    try {
      setLoading(true);
      const res = await getAdminUsers();
      setData(res?.dbAdmins || []);
      setActiveCount(res?.activeCount || 0);
    } catch {
      message.error('获取权限名单失败');
    } finally {
      setLoading(false);
    }
  }, []);

  // 首次挂载自动加载（ref 防 StrictMode double-mount）
  const initRef = useRef(false);
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;
    fetchList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 数据变化后检测表格体是否溢出可滚动
  useEffect(() => {
    const el = tableBodyRef.current?.querySelector('.ant-table-body');
    if (el) {
      setHasScroll(el.scrollHeight > el.clientHeight);
    }
  }, [data, loading]);

  /** 新增权限 */
  const handleAdd = useCallback(async () => {
    try {
      const values = await form.validateFields();
      setAddLoading(true);
      await addAdmin(values as AddAdminParams);
      message.success('权限添加成功');
      setAddModalOpen(false);
      form.resetFields();
      fetchList();
    } catch (err) {
      // validateFields 失败时不提示，API 失败时 request 层已处理
      if (err instanceof Error && !('errorFields' in (err as unknown as object))) {
        // API error, already handled by interceptor
      }
    } finally {
      setAddLoading(false);
    }
  }, [form, fetchList]);

  /** 删除权限 */
  const handleRemove = useCallback(
    async (record: AdminUser) => {
      try {
        await removeAdmin({ userId: record.userId, role: record.role });
        message.success('权限删除成功');
        fetchList();
      } catch {
        // API error already handled by interceptor
      }
    },
    [fetchList],
  );

  /** 表格列定义 */
  const columns = [
    {
      title: '用户 ID',
      dataIndex: 'userId',
      key: 'userId',
      width: 200,
      render: (text: string) => <span className={styles.roleTag}>{text}</span>,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 180,
      render: (text: string) => (
        <Tag color="purple" className={styles.roleTag}>
          {text}
        </Tag>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 120,
      render: (text: string) => (
        <Tag color={text === 'active' ? 'green' : 'default'}>
          {text === 'active' ? '生效' : '停用'}
        </Tag>
      ),
    },
    {
      title: '创建人',
      dataIndex: 'createdBy',
      key: 'createdBy',
      width: 150,
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 200,
    },
    {
      title: '操作',
      key: 'action',
      width: 100,
      render: (_: unknown, record: AdminUser) => (
        <Popconfirm
          title="确认删除该权限？"
          onConfirm={() => handleRemove(record)}
          okText="删除"
          cancelText="取消"
        >
          <Button type="link" danger size="small" icon={<DeleteOutlined />}>
            删除
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <PageContainer
      title="权限控制"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchList} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddModalOpen(true)}>
            新增权限
          </Button>
        </Space>
      }
    >
      {/* 页面描述 */}
      <div className={styles.subtitle}>
        <RiShieldLine className={styles.subtitleIcon} />
        管理平台管理员权限名单，支持查看、新增与删除
      </div>

      {/* 统计信息 */}
      <div className={styles.statsBar}>
        <div className={styles.statsItem}>
          活跃管理员：
          <span className={styles.statsValue}>{activeCount}</span>
        </div>
        <div className={styles.statsItem}>
          总记录数：
          <span className={styles.statsValue}>{data.length}</span>
        </div>
      </div>

      {/* 管理员列表 */}
      <div className={styles.actionBar}>
        <span className={styles.actionTitle}>权限名单</span>
      </div>

      <div className={styles.tableCard} ref={tableBodyRef}>
        <Table<AdminUser>
          rowKey="id"
          columns={columns}
          dataSource={data}
          loading={loading}
          pagination={false}
          size="middle"
          scroll={{ y: 'calc(100vh - 340px)' }}
        />
        {hasScroll && <div className={styles.scrollFade} />}
      </div>

      {/* 新增权限弹窗 */}
      <Modal
        title="新增权限"
        open={addModalOpen}
        onOk={handleAdd}
        onCancel={() => {
          setAddModalOpen(false);
          form.resetFields();
        }}
        confirmLoading={addLoading}
        okText="确认添加"
        cancelText="取消"
        className={styles.addModal}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" autoComplete="off">
          <Form.Item
            name="userId"
            label="用户 ID"
            rules={[{ required: true, message: '请输入用户 ID（邮箱前缀）' }]}
          >
            <Input placeholder="例如：wb_chenlong09" />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
            <Select placeholder="请选择角色" options={ROLE_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}

export default PermissionControl;
