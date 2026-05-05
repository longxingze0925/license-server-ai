import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Table, Button, Space, Modal, Form, Input, InputNumber, message, Tag, App, Select } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, SettingOutlined, MinusCircleOutlined } from '@ant-design/icons';
import { appApi } from '../api';
import { useAuthStore } from '../store';

const Apps: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const navigate = useNavigate();
  const role = user?.role || '';
  const canCreateOrUpdateApp = ['owner', 'admin', 'developer'].includes(role);
  const canDeleteApp = ['owner', 'admin'].includes(role);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any[]>([]);
  const [modalVisible, setModalVisible] = useState(false);
  const [currentApp, setCurrentApp] = useState<any>(null);
  const [form] = Form.useForm();

  const renderAuthMode = (mode?: string) => {
    if (mode === 'license') return <Tag color="blue">授权码</Tag>;
    if (mode === 'subscription') return <Tag color="purple">账号订阅</Tag>;
    return <Tag color="green">两者都支持</Tag>;
  };

  useEffect(() => {
    fetchData();
  }, []);

  const fetchData = async () => {
    setLoading(true);
    try {
      const result: any = await appApi.list();
      setData(result || []);
    } catch (error) {
      console.error(error);
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = () => {
    if (!canCreateOrUpdateApp) {
      return;
    }

    setCurrentApp(null);
    form.resetFields();
    setModalVisible(true);
  };

  const handleEdit = (record: any) => {
    if (!canCreateOrUpdateApp) {
      return;
    }

    setCurrentApp(record);
    form.setFieldsValue({
      ...record,
      features: record.features || [],
    });
    setModalVisible(true);
  };

  const handleDelete = (record: any) => {
    if (!canDeleteApp) {
      return;
    }

    modal.confirm({
      title: '确认删除',
      content: `确定要删除应用 "${record.name}" 吗？`,
      onOk: async () => {
        try {
          await appApi.delete(record.id);
          message.success('删除成功');
          fetchData();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleSubmit = async () => {
    if (!canCreateOrUpdateApp) {
      return;
    }

    try {
      const values = await form.validateFields();
      if (currentApp) {
        await appApi.update(currentApp.id, values);
        message.success('更新成功');
      } else {
        await appApi.create(values);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchData();
    } catch (error) {
      console.error(error);
    }
  };

  const columns = [
    { title: '应用名称', dataIndex: 'name', key: 'name' },
    { title: 'App Key', dataIndex: 'app_key', key: 'app_key', render: (text: string) => <code>{text?.slice(0, 16)}...</code> },
    {
      title: '状态', dataIndex: 'status', key: 'status',
      render: (status: string) => (
        <Tag color={status === 'active' ? 'green' : 'red'}>
          {status === 'active' ? '启用' : '禁用'}
        </Tag>
      ),
    },
    { title: '授权模式', dataIndex: 'auth_mode', key: 'auth_mode', render: renderAuthMode },
    { title: '默认设备数', dataIndex: 'max_devices_default', key: 'max_devices_default' },
    { title: '心跳间隔', dataIndex: 'heartbeat_interval', key: 'heartbeat_interval', render: (v: number) => `${v}秒` },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => v?.slice(0, 10) },
    {
      title: '操作', key: 'action', width: 220,
      render: (_: any, record: any) => (
        <Space>
          <Button type="primary" size="small" icon={<SettingOutlined />} onClick={() => navigate(`/apps/${record.id}`)}>管理</Button>
          {canCreateOrUpdateApp && <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>编辑</Button>}
          {canDeleteApp && <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)}>删除</Button>}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <h2 style={{ margin: 0 }}>应用管理</h2>
        {canCreateOrUpdateApp && <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>创建应用</Button>}
      </div>

      <Table columns={columns} dataSource={data} rowKey="id" loading={loading} />

      {/* 创建/编辑弹窗 */}
      <Modal
        title={currentApp ? '编辑应用' : '创建应用'}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="应用名称" rules={[{ required: true, message: '请输入应用名称' }]}>
            <Input placeholder="请输入应用名称" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea placeholder="请输入描述" rows={3} />
          </Form.Item>
          <Form.Item name="auth_mode" label="授权模式" initialValue="both" rules={[{ required: true, message: '请选择授权模式' }]}>
            <Select>
              <Select.Option value="both">两者都支持</Select.Option>
              <Select.Option value="license">仅授权码</Select.Option>
              <Select.Option value="subscription">仅账号订阅</Select.Option>
            </Select>
          </Form.Item>
          <Form.Item name="max_devices_default" label="默认设备数" initialValue={1}>
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="heartbeat_interval" label="心跳间隔(秒)" initialValue={3600}>
            <InputNumber min={60} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="grace_period_days" label="宽限期(天)" initialValue={3}>
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="功能列表">
            <Form.List name="features">
              {(fields, { add, remove }) => (
                <>
                  {fields.map(({ key, name, ...restField }) => (
                    <Space key={key} style={{ display: 'flex', marginBottom: 8 }} align="baseline">
                      <Form.Item
                        {...restField}
                        name={name}
                        rules={[{ required: true, message: '请输入功能名称' }]}
                        style={{ marginBottom: 0 }}
                      >
                        <Input placeholder="功能名称，如：export" style={{ width: 200 }} />
                      </Form.Item>
                      <MinusCircleOutlined onClick={() => remove(name)} style={{ color: '#ff4d4f' }} />
                    </Space>
                  ))}
                  <Button type="dashed" onClick={() => add()} block icon={<PlusOutlined />}>
                    添加功能
                  </Button>
                </>
              )}
            </Form.List>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default Apps;
