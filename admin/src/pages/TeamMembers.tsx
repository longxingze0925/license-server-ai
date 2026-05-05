import React, { useCallback, useEffect, useState } from 'react';
import { Table, Button, Space, Tag, Modal, Form, Input, Select, message, Card, Popconfirm, Tooltip } from 'antd';
import { PlusOutlined, DeleteOutlined, EditOutlined, KeyOutlined } from '@ant-design/icons';
import { teamApi } from '../api';
import { useAuthStore } from '../store';

interface TeamMember {
  id: string;
  email: string;
  name: string;
  phone?: string;
  avatar?: string;
  role: string;
  status: string;
  last_login_at?: string;
  created_at: string;
}

const roleOptions = [
  { value: 'admin', label: '管理员', color: 'red' },
  { value: 'developer', label: '开发者', color: 'blue' },
  { value: 'viewer', label: '只读', color: 'default' },
];

const TeamMembers: React.FC = () => {
  const { user } = useAuthStore();
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [loading, setLoading] = useState(false);
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [editModalVisible, setEditModalVisible] = useState(false);
  const [passwordModalVisible, setPasswordModalVisible] = useState(false);
  const [roleModalVisible, setRoleModalVisible] = useState(false);
  const [selectedMember, setSelectedMember] = useState<TeamMember | null>(null);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 });
  const [createForm] = Form.useForm();
  const [editForm] = Form.useForm();
  const [passwordForm] = Form.useForm();
  const [roleForm] = Form.useForm();

  const isOwner = user?.role === 'owner';
  const isAdmin = user?.role === 'admin' || isOwner;

  const fetchMembers = useCallback(async (page = 1, pageSize = 20) => {
    setLoading(true);
    try {
      const res: any = await teamApi.list({ page, page_size: pageSize });
      setMembers(res.list || []);
      setPagination({ current: page, pageSize, total: res.total || 0 });
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchMembers();
  }, [fetchMembers]);

  const handleCreate = async (values: { email: string; password: string; name: string; role: string; phone?: string }) => {
    try {
      await teamApi.create(values);
      message.success('成员创建成功');
      setCreateModalVisible(false);
      createForm.resetFields();
      fetchMembers(pagination.current, pagination.pageSize);
    } catch {
      // handled by interceptor
    }
  };

  const handleUpdate = async (values: { email?: string; name?: string; phone?: string }) => {
    if (!selectedMember) return;
    try {
      await teamApi.update(selectedMember.id, values);
      message.success('成员信息已更新');
      setEditModalVisible(false);
      editForm.resetFields();
      setSelectedMember(null);
      fetchMembers(pagination.current, pagination.pageSize);
    } catch {
      // handled by interceptor
    }
  };

  const handleResetPassword = async (values: { password: string }) => {
    if (!selectedMember) return;
    try {
      await teamApi.resetPassword(selectedMember.id, values);
      message.success('密码已重置');
      setPasswordModalVisible(false);
      passwordForm.resetFields();
      setSelectedMember(null);
    } catch {
      // handled by interceptor
    }
  };

  const handleUpdateRole = async (values: { role: string }) => {
    if (!selectedMember) return;
    try {
      await teamApi.updateRole(selectedMember.id, values);
      message.success('角色已更新');
      setRoleModalVisible(false);
      roleForm.resetFields();
      setSelectedMember(null);
      fetchMembers(pagination.current, pagination.pageSize);
    } catch {
      // handled by interceptor
    }
  };

  const handleRemove = async (id: string) => {
    try {
      await teamApi.remove(id);
      message.success('成员已移除');
      fetchMembers(pagination.current, pagination.pageSize);
    } catch {
      // handled by interceptor
    }
  };

  const getRoleTag = (role: string) => {
    if (role === 'owner') return <Tag color="gold">所有者</Tag>;
    const roleInfo = roleOptions.find(r => r.value === role);
    return <Tag color={roleInfo?.color || 'default'}>{roleInfo?.label || role}</Tag>;
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      active: { color: 'green', text: '正常' },
      disabled: { color: 'red', text: '已禁用' },
      pending: { color: 'orange', text: '待激活' },
    };
    const info = statusMap[status] || { color: 'default', text: status };
    return <Tag color={info.color}>{info.text}</Tag>;
  };

  const columns = [
    { title: '邮箱', dataIndex: 'email', key: 'email' },
    { title: '姓名', dataIndex: 'name', key: 'name' },
    { title: '电话', dataIndex: 'phone', key: 'phone', render: (v: string) => v || '-' },
    { title: '角色', dataIndex: 'role', key: 'role', render: getRoleTag },
    { title: '状态', dataIndex: 'status', key: 'status', render: getStatusTag },
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
      render: (_: any, record: TeamMember) => {
        const isSelf = record.id === user?.id;
        const canEdit = isSelf || isAdmin;
        const canEditRole = record.role !== 'owner' && !isSelf && (isOwner || (isAdmin && record.role !== 'admin'));
        const canDelete = record.role !== 'owner' && !isSelf && (isOwner || (isAdmin && record.role !== 'admin'));
        const canResetPassword = isSelf || (isAdmin && record.role !== 'owner') || (isOwner);

        return (
          <Space>
            {canEdit && (
              <Tooltip title="编辑信息">
                <Button
                  type="link"
                  size="small"
                  icon={<EditOutlined />}
                  onClick={() => {
                    setSelectedMember(record);
                    editForm.setFieldsValue({
                      email: record.email,
                      name: record.name,
                      phone: record.phone,
                    });
                    setEditModalVisible(true);
                  }}
                />
              </Tooltip>
            )}
            {canResetPassword && (
              <Tooltip title="重置密码">
                <Button
                  type="link"
                  size="small"
                  icon={<KeyOutlined />}
                  onClick={() => {
                    setSelectedMember(record);
                    setPasswordModalVisible(true);
                  }}
                />
              </Tooltip>
            )}
            {canEditRole && (
              <Tooltip title="修改角色">
                <Button
                  type="link"
                  size="small"
                  onClick={() => {
                    setSelectedMember(record);
                    roleForm.setFieldsValue({ role: record.role });
                    setRoleModalVisible(true);
                  }}
                >
                  角色
                </Button>
              </Tooltip>
            )}
            {canDelete && (
              <Popconfirm
                title="确定要移除该成员吗？"
                onConfirm={() => handleRemove(record.id)}
              >
                <Button type="link" size="small" danger icon={<DeleteOutlined />} />
              </Popconfirm>
            )}
          </Space>
        );
      },
    },
  ];

  return (
    <Card
      title="团队管理"
      extra={
        isAdmin && (
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => setCreateModalVisible(true)}
          >
            添加成员
          </Button>
        )
      }
    >
      <Table
        columns={columns}
        dataSource={members}
        rowKey="id"
        loading={loading}
        pagination={{
          ...pagination,
          showSizeChanger: true,
          showTotal: (total) => `共 ${total} 条`,
          onChange: (page, pageSize) => fetchMembers(page, pageSize),
        }}
      />

      {/* 创建成员弹窗 */}
      <Modal
        title="添加成员"
        open={createModalVisible}
        onCancel={() => {
          setCreateModalVisible(false);
          createForm.resetFields();
        }}
        footer={null}
      >
        <Form form={createForm} layout="vertical" onFinish={handleCreate}>
          <Form.Item
            name="email"
            label="邮箱"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '请输入有效的邮箱' },
            ]}
          >
            <Input placeholder="请输入邮箱" />
          </Form.Item>
          <Form.Item
            name="name"
            label="姓名"
            rules={[{ required: true, message: '请输入姓名' }]}
          >
            <Input placeholder="请输入姓名" />
          </Form.Item>
          <Form.Item name="phone" label="电话">
            <Input placeholder="请输入电话（可选）" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[
              { required: true, message: '请输入密码' },
              { min: 8, message: '密码至少8位' },
              { pattern: /\d/, message: '密码必须包含数字' },
              { pattern: /[^A-Za-z0-9]/, message: '密码必须包含特殊字符' },
            ]}
          >
            <Input.Password placeholder="请输入密码" />
          </Form.Item>
          <Form.Item
            name="role"
            label="角色"
            rules={[{ required: true, message: '请选择角色' }]}
          >
            <Select placeholder="请选择角色">
              {roleOptions
                .filter(r => isOwner || r.value !== 'admin')
                .map(r => (
                  <Select.Option key={r.value} value={r.value}>
                    {r.label}
                  </Select.Option>
                ))}
            </Select>
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                创建
              </Button>
              <Button onClick={() => setCreateModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      {/* 编辑成员弹窗 */}
      <Modal
        title="编辑成员"
        open={editModalVisible}
        onCancel={() => {
          setEditModalVisible(false);
          editForm.resetFields();
          setSelectedMember(null);
        }}
        footer={null}
      >
        <Form form={editForm} layout="vertical" onFinish={handleUpdate}>
          <Form.Item
            name="email"
            label="邮箱"
            rules={[{ type: 'email', message: '请输入有效的邮箱' }]}
          >
            <Input placeholder="请输入邮箱" />
          </Form.Item>
          <Form.Item name="name" label="姓名">
            <Input placeholder="请输入姓名" />
          </Form.Item>
          <Form.Item name="phone" label="电话">
            <Input placeholder="请输入电话" />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                保存
              </Button>
              <Button onClick={() => setEditModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      {/* 重置密码弹窗 */}
      <Modal
        title={`重置密码 - ${selectedMember?.name || selectedMember?.email}`}
        open={passwordModalVisible}
        onCancel={() => {
          setPasswordModalVisible(false);
          passwordForm.resetFields();
          setSelectedMember(null);
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
              <Button onClick={() => setPasswordModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      {/* 修改角色弹窗 */}
      <Modal
        title={`修改角色 - ${selectedMember?.name || selectedMember?.email}`}
        open={roleModalVisible}
        onCancel={() => {
          setRoleModalVisible(false);
          roleForm.resetFields();
          setSelectedMember(null);
        }}
        footer={null}
      >
        <Form form={roleForm} layout="vertical" onFinish={handleUpdateRole}>
          <Form.Item
            name="role"
            label="角色"
            rules={[{ required: true, message: '请选择角色' }]}
          >
            <Select placeholder="请选择角色">
              {roleOptions
                .filter(r => isOwner || r.value !== 'admin')
                .map(r => (
                  <Select.Option key={r.value} value={r.value}>
                    {r.label}
                  </Select.Option>
                ))}
            </Select>
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                确定
              </Button>
              <Button onClick={() => setRoleModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
};

export default TeamMembers;
