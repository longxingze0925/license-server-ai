import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Table, Button, Space, Tag, Modal, Form, Input, Select, message, Card,
  Popconfirm, Tooltip, Drawer, Descriptions, Tabs, List
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, EditOutlined, StopOutlined, CheckOutlined,
  KeyOutlined, EyeOutlined
} from '@ant-design/icons';
import { customerApi, teamApi } from '../api';
import { useAuthStore } from '../store';

interface TeamMember {
  id: string;
  email: string;
  name: string;
  role: string;
}

interface Customer {
  id: string;
  email: string;
  name?: string;
  phone?: string;
  company?: string;
  status: string;
  owner_id?: string;
  owner_name?: string;
  owner_email?: string;
  has_password: boolean;
  last_login_at?: string;
  created_at: string;
  remark?: string;
  metadata?: string;
  stats?: {
    licenses: number;
    subscriptions: number;
    devices: number;
  };
}

interface License {
  id: string;
  license_key: string;
  type: string;
  status: string;
  app_name?: string;
  expire_at?: string;
}

interface Subscription {
  id: string;
  plan_type: string;
  status: string;
  app_name?: string;
  expire_at?: string;
}

interface Device {
  id: string;
  machine_id: string;
  device_name?: string;
  os_type?: string;
  status: string;
  last_heartbeat_at?: string;
}

const Customers: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useAuthStore();
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [teamMembers, setTeamMembers] = useState<TeamMember[]>([]);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editingCustomer, setEditingCustomer] = useState<Customer | null>(null);
  const [detailDrawerVisible, setDetailDrawerVisible] = useState(false);
  const [selectedCustomer, setSelectedCustomer] = useState<Customer | null>(null);
  const [customerLicenses, setCustomerLicenses] = useState<License[]>([]);
  const [customerSubscriptions, setCustomerSubscriptions] = useState<Subscription[]>([]);
  const [customerDevices, setCustomerDevices] = useState<Device[]>([]);
  const [resetPasswordModalVisible, setResetPasswordModalVisible] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 });
  const [searchKeyword, setSearchKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [ownerFilter, setOwnerFilter] = useState<string>('');
  const [form] = Form.useForm();
  const [passwordForm] = Form.useForm();

  const isAdmin = user?.role === 'owner' || user?.role === 'admin';
  const canUpdateCustomer = user?.role === 'owner' || user?.role === 'admin' || user?.role === 'developer';
  const canDeleteCustomer = user?.role === 'owner' || user?.role === 'admin';

  // 获取团队成员列表（用于选择所属成员）
  const fetchTeamMembers = useCallback(async () => {
    try {
      const res: any = await teamApi.list({ page: 1, page_size: 100 });
      setTeamMembers(res.list || []);
    } catch {
      // handled by interceptor
    }
  }, []);

  const fetchCustomers = useCallback(async (page = 1, pageSize = 20) => {
    setLoading(true);
    try {
      const params: any = { page, page_size: pageSize };
      if (searchKeyword) params.keyword = searchKeyword;
      if (statusFilter) params.status = statusFilter;
      if (ownerFilter) params.owner_id = ownerFilter;
      const res: any = await customerApi.list(params);
      setCustomers(res.list || []);
      setPagination({ current: page, pageSize, total: res.total || 0 });
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  }, [ownerFilter, searchKeyword, statusFilter]);

  useEffect(() => {
    fetchTeamMembers();
  }, [fetchTeamMembers]);

  useEffect(() => {
    fetchCustomers();
  }, [fetchCustomers]);

  useEffect(() => {
    if (searchParams.get('create') !== '1' || !canUpdateCustomer) {
      return;
    }
    setEditingCustomer(null);
    form.resetFields();
    setModalVisible(true);
    setSearchParams({}, { replace: true });
  }, [canUpdateCustomer, form, searchParams, setSearchParams]);

  const openCreateModal = () => {
    setEditingCustomer(null);
    form.resetFields();
    setModalVisible(true);
  };

  const fetchCustomerDetails = async (id: string) => {
    try {
      const [detail, licenses, subscriptions, devices]: any = await Promise.all([
        customerApi.get(id),
        customerApi.getLicenses(id),
        customerApi.getSubscriptions(id),
        customerApi.getDevices(id),
      ]);
      setSelectedCustomer(detail as Customer);
      setCustomerLicenses(licenses || []);
      setCustomerSubscriptions(subscriptions || []);
      setCustomerDevices(devices || []);
    } catch {
      // handled by interceptor
    }
  };

  const handleCreate = async (values: any) => {
    if (!canUpdateCustomer) return;

    setSubmitting(true);
    try {
      await customerApi.create(values);
      message.success('客户创建成功');
      setModalVisible(false);
      form.resetFields();
      fetchCustomers(pagination.current, pagination.pageSize);
    } catch (e: any) {
      message.error(e?.message || '客户创建失败，请检查邮箱、密码和权限');
    } finally {
      setSubmitting(false);
    }
  };

  const handleUpdate = async (values: any) => {
    if (!canUpdateCustomer) return;

    if (!editingCustomer) return;
    setSubmitting(true);
    try {
      await customerApi.update(editingCustomer.id, values);
      message.success('客户更新成功');
      setModalVisible(false);
      setEditingCustomer(null);
      form.resetFields();
      fetchCustomers(pagination.current, pagination.pageSize);
    } catch (e: any) {
      message.error(e?.message || '客户更新失败，请稍后重试');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!canDeleteCustomer) return;

    try {
      await customerApi.delete(id);
      message.success('客户已删除');
      fetchCustomers(pagination.current, pagination.pageSize);
    } catch {
      // handled by interceptor
    }
  };

  const handleToggleStatus = async (customer: Customer) => {
    if (!canUpdateCustomer) return;

    try {
      if (customer.status === 'active') {
        await customerApi.disable(customer.id);
        message.success('客户已禁用');
      } else {
        await customerApi.enable(customer.id);
        message.success('客户已启用');
      }
      fetchCustomers(pagination.current, pagination.pageSize);
    } catch {
      // handled by interceptor
    }
  };

  const handleResetPassword = async (values: { password: string }) => {
    if (!canUpdateCustomer) return;

    if (!selectedCustomer) return;
    try {
      await customerApi.resetPassword(selectedCustomer.id, values);
      message.success('密码已重置');
      setResetPasswordModalVisible(false);
      passwordForm.resetFields();
    } catch {
      // handled by interceptor
    }
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      active: { color: 'green', text: '正常' },
      disabled: { color: 'red', text: '已禁用' },
      banned: { color: 'volcano', text: '已封禁' },
      pending: { color: 'orange', text: '待激活' },
    };
    const info = statusMap[status] || { color: 'default', text: status };
    return <Tag color={info.color}>{info.text}</Tag>;
  };

  const columns = [
    { title: '邮箱', dataIndex: 'email', key: 'email' },
    { title: '姓名', dataIndex: 'name', key: 'name', render: (v: string) => v || '-' },
    { title: '公司', dataIndex: 'company', key: 'company', render: (v: string) => v || '-' },
    {
      title: '所属成员',
      dataIndex: 'owner_name',
      key: 'owner_name',
      render: (v: string, record: Customer) => v || record.owner_email || '-',
    },
    { title: '状态', dataIndex: 'status', key: 'status', render: getStatusTag },
    {
      title: '密码',
      dataIndex: 'has_password',
      key: 'has_password',
      render: (v: boolean) => v ? <Tag color="blue">已设置</Tag> : <Tag>未设置</Tag>,
    },
    {
      title: '最后登录',
      dataIndex: 'last_login_at',
      key: 'last_login_at',
      render: (v: string) => v ? new Date(v).toLocaleString() : '-',
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: any, record: Customer) => (
        <Space>
          <Tooltip title="查看详情">
            <Button
              type="link"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => {
                fetchCustomerDetails(record.id);
                setDetailDrawerVisible(true);
              }}
            />
          </Tooltip>
          {canUpdateCustomer && (
            <Tooltip title="编辑">
              <Button
                type="link"
                size="small"
                icon={<EditOutlined />}
                onClick={() => {
                  setEditingCustomer(record);
                  form.setFieldsValue(record);
                  setModalVisible(true);
                }}
              />
            </Tooltip>
          )}
          {canUpdateCustomer && (
            <Tooltip title={record.status === 'active' ? '禁用' : '启用'}>
              <Button
                type="link"
                size="small"
                icon={record.status === 'active' ? <StopOutlined /> : <CheckOutlined />}
                onClick={() => handleToggleStatus(record)}
              />
            </Tooltip>
          )}
          {canDeleteCustomer && (
            <Popconfirm
              title="确定要删除该客户吗？相关数据也会被删除。"
              onConfirm={() => handleDelete(record.id)}
            >
              <Button type="link" size="small" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  const detailTabItems = [
    {
      key: 'info',
      label: '基本信息',
      children: selectedCustomer && (
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label="邮箱">{selectedCustomer.email}</Descriptions.Item>
          <Descriptions.Item label="姓名">{selectedCustomer.name || '-'}</Descriptions.Item>
          <Descriptions.Item label="电话">{selectedCustomer.phone || '-'}</Descriptions.Item>
          <Descriptions.Item label="公司">{selectedCustomer.company || '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{getStatusTag(selectedCustomer.status)}</Descriptions.Item>
          <Descriptions.Item label="密码">
            {selectedCustomer.has_password ? <Tag color="blue">已设置</Tag> : <Tag>未设置</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="授权码数">{selectedCustomer.stats?.licenses || 0}</Descriptions.Item>
          <Descriptions.Item label="订阅数">{selectedCustomer.stats?.subscriptions || 0}</Descriptions.Item>
          <Descriptions.Item label="设备数">{selectedCustomer.stats?.devices || 0}</Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {new Date(selectedCustomer.created_at).toLocaleString()}
          </Descriptions.Item>
          <Descriptions.Item label="备注" span={2}>{selectedCustomer.remark || '-'}</Descriptions.Item>
        </Descriptions>
      ),
    },
    {
      key: 'licenses',
      label: `授权码 (${customerLicenses.length})`,
      children: (
        <List
          size="small"
          dataSource={customerLicenses}
          renderItem={(item) => (
            <List.Item>
              <List.Item.Meta
                title={item.license_key}
                description={`${item.app_name || '未知应用'} | ${item.type} | ${item.status}`}
              />
              {item.expire_at && <span>过期: {new Date(item.expire_at).toLocaleDateString()}</span>}
            </List.Item>
          )}
        />
      ),
    },
    {
      key: 'subscriptions',
      label: `订阅 (${customerSubscriptions.length})`,
      children: (
        <List
          size="small"
          dataSource={customerSubscriptions}
          renderItem={(item) => (
            <List.Item>
              <List.Item.Meta
                title={item.app_name || '未知应用'}
                description={`${item.plan_type} | ${item.status}`}
              />
              {item.expire_at && <span>过期: {new Date(item.expire_at).toLocaleDateString()}</span>}
            </List.Item>
          )}
        />
      ),
    },
    {
      key: 'devices',
      label: `设备 (${customerDevices.length})`,
      children: (
        <List
          size="small"
          dataSource={customerDevices}
          renderItem={(item) => (
            <List.Item>
              <List.Item.Meta
                title={item.device_name || item.machine_id}
                description={`${item.os_type || '未知系统'} | ${item.status}`}
              />
              {item.last_heartbeat_at && (
                <span>最后心跳: {new Date(item.last_heartbeat_at).toLocaleString()}</span>
              )}
            </List.Item>
          )}
        />
      ),
    },
  ];

  return (
    <Card
      title="客户管理"
      extra={
        <Space>
          <Input.Search
            placeholder="搜索邮箱/姓名/公司"
            allowClear
            onSearch={setSearchKeyword}
            style={{ width: 200 }}
          />
          <Select
            placeholder="状态筛选"
            allowClear
            style={{ width: 120 }}
            onChange={setStatusFilter}
          >
            <Select.Option value="active">正常</Select.Option>
            <Select.Option value="disabled">已禁用</Select.Option>
            <Select.Option value="banned">已封禁</Select.Option>
          </Select>
          {isAdmin && (
            <Select
              placeholder="所属成员"
              allowClear
              style={{ width: 150 }}
              onChange={setOwnerFilter}
            >
              {teamMembers.map(m => (
                <Select.Option key={m.id} value={m.id}>{m.name || m.email}</Select.Option>
              ))}
            </Select>
          )}
          {canUpdateCustomer && (
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
              添加客户
            </Button>
          )}
        </Space>
      }
    >
      <Table
        columns={columns}
        dataSource={customers}
        rowKey="id"
        loading={loading}
        pagination={{
          ...pagination,
          showSizeChanger: true,
          showTotal: (total) => `共 ${total} 条`,
          onChange: (page, pageSize) => fetchCustomers(page, pageSize),
        }}
      />

      <Modal
        title={editingCustomer ? '编辑客户' : '添加客户'}
        open={modalVisible}
        onCancel={() => {
          setModalVisible(false);
          setEditingCustomer(null);
          form.resetFields();
        }}
        footer={null}
        width={600}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={editingCustomer ? handleUpdate : handleCreate}
          onFinishFailed={({ errorFields }) => {
            const first = errorFields[0]?.errors?.[0];
            message.warning(first || '请先补全客户信息');
          }}
        >
          <Form.Item
            name="email"
            label="邮箱"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '请输入有效的邮箱' },
            ]}
          >
            <Input placeholder="请输入邮箱" disabled={!!editingCustomer} />
          </Form.Item>
          {!editingCustomer && (
            <Form.Item
              name="password"
              label="密码"
              extra="可选，用于订阅模式登录"
              rules={[
                { min: 8, message: '密码至少8位' },
                { pattern: /\d/, message: '密码必须包含数字' },
                { pattern: /[^A-Za-z0-9]/, message: '密码必须包含特殊字符' },
              ]}
            >
              <Input.Password placeholder="请输入密码（可选）" />
            </Form.Item>
          )}
          {!editingCustomer && isAdmin && (
            <Form.Item
              name="owner_id"
              label="所属成员"
              extra="不选择则默认为当前登录用户"
            >
              <Select placeholder="请选择所属成员" allowClear>
                {teamMembers.map(m => (
                  <Select.Option key={m.id} value={m.id}>{m.name || m.email}</Select.Option>
                ))}
              </Select>
            </Form.Item>
          )}
          <Form.Item name="name" label="姓名">
            <Input placeholder="请输入姓名" />
          </Form.Item>
          <Form.Item name="phone" label="电话">
            <Input placeholder="请输入电话" />
          </Form.Item>
          <Form.Item name="company" label="公司">
            <Input placeholder="请输入公司名称" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={3} placeholder="请输入备注" />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" loading={submitting}>
                {editingCustomer ? '保存' : '创建'}
              </Button>
              <Button disabled={submitting} onClick={() => setModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title="客户详情"
        open={detailDrawerVisible}
        onClose={() => {
          setDetailDrawerVisible(false);
          setSelectedCustomer(null);
        }}
        width={600}
        extra={
          selectedCustomer && canUpdateCustomer && (
            <Button
              icon={<KeyOutlined />}
              onClick={() => setResetPasswordModalVisible(true)}
            >
              重置密码
            </Button>
          )
        }
      >
        <Tabs items={detailTabItems} />
      </Drawer>

      <Modal
        title="重置密码"
        open={resetPasswordModalVisible}
        onCancel={() => {
          setResetPasswordModalVisible(false);
          passwordForm.resetFields();
        }}
        footer={null}
      >
        <Form form={passwordForm} layout="vertical" onFinish={handleResetPassword}>
          <Form.Item
            name="password"
            label="新密码"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 8, message: '密码至少8位' },
              { pattern: /\d/, message: '密码必须包含数字' },
              { pattern: /[^A-Za-z0-9]/, message: '密码必须包含特殊字符' },
            ]}
          >
            <Input.Password placeholder="请输入新密码" />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                确定
              </Button>
              <Button onClick={() => setResetPasswordModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
};

export default Customers;
