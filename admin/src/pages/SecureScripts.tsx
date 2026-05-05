import React, { useCallback, useEffect, useState } from 'react';
import { Table, Button, Space, Modal, Form, Input, InputNumber, message, Tag, Select, Descriptions, Tabs, Spin, App } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, CodeOutlined } from '@ant-design/icons';
import { secureScriptApi, appApi } from '../api';
import { useAuthStore } from '../store';

const SecureScripts: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const [loading, setLoading] = useState(false);
  const [pageLoading, setPageLoading] = useState(true);
  const [data, setData] = useState<any[]>([]);
  const [apps, setApps] = useState<any[]>([]);
  const [selectedApp, setSelectedApp] = useState<string>('');
  const [modalVisible, setModalVisible] = useState(false);
  const [contentModalVisible, setContentModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentScript, setCurrentScript] = useState<any>(null);
  const [deliveries, setDeliveries] = useState<any[]>([]);
  const [form] = Form.useForm();
  const [contentForm] = Form.useForm();
  const [editMode, setEditMode] = useState(false);
  const canUpdateScript = ['owner', 'admin', 'developer'].includes(user?.role || '');
  const canDeleteScript = user?.role === 'owner' || user?.role === 'admin';

  const parseStringList = (value: any): string[] => {
    if (Array.isArray(value)) return value.map(String).map(v => v.trim()).filter(Boolean);
    if (!value) return [];
    if (typeof value === 'string') {
      const text = value.trim();
      if (!text) return [];
      try {
        const parsed = JSON.parse(text);
        if (Array.isArray(parsed)) return parsed.map(String).map(v => v.trim()).filter(Boolean);
      } catch {
        // 支持逗号分隔输入
      }
      return text.split(',').map(v => v.trim()).filter(Boolean);
    }
    return [];
  };

  const listToInputText = (value: any) => parseStringList(value).join(',');

  const getScriptFileName = (scriptType?: string) => {
    if (scriptType === 'lua') return 'script.lua';
    if (scriptType === 'instruction') return 'script.json';
    return 'script.py';
  };

  const buildScriptFormData = (values: any) => {
    const formData = new FormData();
    const scriptType = values.script_type || 'python';
    formData.append('file', new File([values.content || ''], getScriptFileName(scriptType), { type: 'text/plain' }));
    formData.append('name', values.name || '');
    formData.append('version', values.version || '1.0.0');
    formData.append('script_type', scriptType);
    formData.append('description', values.description || '');
    formData.append('entry_point', values.entry_point || '');
    formData.append('timeout', String(values.timeout || 300));
    formData.append('memory_limit', String(values.memory_limit || 512));
    formData.append('parameters', values.parameters || '{}');
    formData.append('rollout_percentage', String(values.rollout_percentage ?? 100));
    formData.append('required_features', JSON.stringify(parseStringList(values.required_features)));
    formData.append('allowed_devices', JSON.stringify(parseStringList(values.allowed_devices)));
    return formData;
  };

  const buildScriptUpdatePayload = (values: any) => ({
    ...values,
    required_features: parseStringList(values.required_features),
    allowed_devices: parseStringList(values.allowed_devices),
  });

  useEffect(() => {
    fetchApps();
  }, []);

  const fetchApps = async () => {
    try {
      const result: any = await appApi.list();
      const appList = Array.isArray(result) ? result : (result?.list || []);
      setApps(appList);
      if (appList.length > 0) {
        setSelectedApp(appList[0].id);
      }
    } catch (error) {
      console.error('fetchApps error:', error);
      setApps([]);
    } finally {
      setPageLoading(false);
    }
  };

  const fetchData = useCallback(async () => {
    if (!selectedApp) return;
    setLoading(true);
    try {
      const result: any = await secureScriptApi.list(selectedApp);
      const list = result?.list ?? (Array.isArray(result) ? result : []);
      setData(list);
    } catch (error) {
      console.error(error);
      setData([]);
    } finally {
      setLoading(false);
    }
  }, [selectedApp]);

  useEffect(() => {
    if (selectedApp) {
      fetchData();
    }
  }, [fetchData, selectedApp]);

  const handleCreate = () => {
    if (!canUpdateScript) return;
    setCurrentScript(null);
    setEditMode(false);
    form.resetFields();
    setModalVisible(true);
  };

  const handleEdit = (record: any) => {
    if (!canUpdateScript) return;
    setCurrentScript(record);
    setEditMode(true);
    form.setFieldsValue({
      ...record,
      required_features: listToInputText(record.required_features),
      allowed_devices: listToInputText(record.allowed_devices),
      rollout_percentage: record.rollout_percentage ?? 100,
    });
    setModalVisible(true);
  };

  const handleView = async (record: any) => {
    try {
      const detail = await secureScriptApi.get(record.id);
      setCurrentScript(detail);
      const deliveriesResult: any = await secureScriptApi.getDeliveries(record.id);
      const list = deliveriesResult?.list ?? (Array.isArray(deliveriesResult) ? deliveriesResult : []);
      setDeliveries(list);
      setDetailVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const handleEditContent = (record: any) => {
    if (!canUpdateScript) return;
    setCurrentScript(record);
    contentForm.setFieldsValue({ version: record.version || '1.0.0', content: '' });
    setContentModalVisible(true);
  };

  const handleSubmit = async () => {
    if (!canUpdateScript) return;
    try {
      const values = await form.validateFields();
      if (editMode && currentScript) {
        await secureScriptApi.update(currentScript.id, buildScriptUpdatePayload(values));
        message.success('更新成功');
      } else {
        await secureScriptApi.create(selectedApp, buildScriptFormData(values));
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchData();
    } catch (error) {
      console.error(error);
    }
  };

  const handleContentSubmit = async () => {
    if (!canUpdateScript) return;
    try {
      const values = await contentForm.validateFields();
      const formData = new FormData();
      formData.append('file', new File([values.content || ''], getScriptFileName(currentScript?.script_type), { type: 'text/plain' }));
      formData.append('version', values.version);
      await secureScriptApi.updateContent(currentScript.id, formData);
      message.success('脚本内容已更新');
      setContentModalVisible(false);
      fetchData();
    } catch (error) {
      console.error(error);
    }
  };

  const handlePublish = async (id: string) => {
    if (!canUpdateScript) return;
    modal.confirm({
      title: '确认发布',
      content: '确定要发布此脚本吗？发布后客户端将可以获取此脚本。',
      onOk: async () => {
        try {
          await secureScriptApi.publish(id);
          message.success('发布成功');
          fetchData();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleDeprecate = async (id: string) => {
    if (!canUpdateScript) return;
    modal.confirm({
      title: '确认废弃',
      content: '确定要废弃此脚本吗？废弃后客户端将无法获取此脚本。',
      onOk: async () => {
        try {
          await secureScriptApi.deprecate(id);
          message.success('已废弃');
          fetchData();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleDelete = (record: any) => {
    if (!canDeleteScript) {
      return;
    }

    modal.confirm({
      title: '确认删除',
      content: `确定要删除脚本 "${record.name}" 吗？`,
      onOk: async () => {
        try {
          await secureScriptApi.delete(record.id);
          message.success('删除成功');
          fetchData();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      draft: { color: 'default', text: '草稿' },
      published: { color: 'green', text: '已发布' },
      deprecated: { color: 'orange', text: '已废弃' },
    };
    const s = statusMap[status] || { color: 'default', text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const getTypeTag = (type: string) => {
    const typeMap: Record<string, { color: string; text: string }> = {
      python: { color: 'blue', text: 'Python' },
      lua: { color: 'purple', text: 'Lua' },
      instruction: { color: 'cyan', text: '指令脚本' },
    };
    const t = typeMap[type] || { color: 'default', text: type };
    return <Tag color={t.color}>{t.text}</Tag>;
  };

  const columns = [
    { title: '脚本名称', dataIndex: 'name', key: 'name' },
    { title: '脚本类型', dataIndex: 'script_type', key: 'script_type', render: getTypeTag },
    { title: '版本', dataIndex: 'version', key: 'version' },
    { title: '所需功能', dataIndex: 'required_features', key: 'required_features', render: (v: string) => listToInputText(v) || '-' },
    { title: '状态', dataIndex: 'status', key: 'status', render: getStatusTag },
    { title: '下发次数', dataIndex: 'delivery_count', key: 'delivery_count', render: (v: number) => v || 0 },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', render: (v: string) => v?.slice(0, 10) },
    {
      title: '操作', key: 'action',
      render: (_: any, record: any) => (
        <Space>
          <Button type="link" size="small" onClick={() => handleView(record)}>详情</Button>
          {canUpdateScript && <Button type="link" size="small" icon={<CodeOutlined />} onClick={() => handleEditContent(record)}>编辑内容</Button>}
          {canUpdateScript && <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>编辑</Button>}
          {canUpdateScript && record.status === 'draft' && (
            <Button type="link" size="small" onClick={() => handlePublish(record.id)}>发布</Button>
          )}
          {canUpdateScript && record.status === 'published' && (
            <Button type="link" size="small" onClick={() => handleDeprecate(record.id)}>废弃</Button>
          )}
          {canDeleteScript && (
            <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)}>删除</Button>
          )}
        </Space>
      ),
    },
  ];

  const deliveryColumns = [
    { title: '设备ID', dataIndex: 'device_id', key: 'device_id', ellipsis: true },
    { title: '机器码', dataIndex: 'machine_id', key: 'machine_id', ellipsis: true },
    {
      title: '状态', dataIndex: 'status', key: 'status',
      render: (status: string) => {
        const map: Record<string, { color: string; text: string }> = {
          pending: { color: 'blue', text: '待下发' },
          executing: { color: 'cyan', text: '执行中' },
          success: { color: 'green', text: '成功' },
          failed: { color: 'red', text: '失败' },
          expired: { color: 'orange', text: '已过期' },
        };
        const s = map[status] || { color: 'default', text: status };
        return <Tag color={s.color}>{s.text}</Tag>;
      }
    },
    { title: '下发时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') || '-' },
    { title: '完成时间', dataIndex: 'completed_at', key: 'completed_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') || '-' },
  ];

  if (pageLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%', minHeight: 300 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>安全脚本管理</h2>
        <Space>
          <Select
            style={{ width: 200 }}
            placeholder="选择应用"
            value={selectedApp || undefined}
            onChange={setSelectedApp}
            options={apps.map(app => ({ label: app.name, value: app.id }))}
          />
          {canUpdateScript && (
            <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate} disabled={!selectedApp}>
              创建脚本
            </Button>
          )}
        </Space>
      </div>

      <Table columns={columns} dataSource={data} rowKey="id" loading={loading} />

      {/* 创建/编辑弹窗 */}
      <Modal
        title={editMode ? '编辑脚本' : '创建脚本'}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="脚本名称" rules={[{ required: true, message: '请输入脚本名称' }]}>
            <Input placeholder="请输入脚本名称" />
          </Form.Item>
          <Form.Item name="script_type" label="脚本类型" initialValue="python" rules={[{ required: true }]}>
            <Select options={[
              { label: 'Python', value: 'python' },
              { label: 'Lua', value: 'lua' },
              { label: '指令脚本', value: 'instruction' },
            ]} />
          </Form.Item>
          <Form.Item name="version" label="版本号" initialValue="1.0.0" rules={[{ required: true, message: '请输入版本号' }]}>
            <Input placeholder="如：1.0.0" />
          </Form.Item>
          <Form.Item name="required_features" label="所需功能">
            <Input placeholder="逗号分隔，留空表示无限制" />
          </Form.Item>
          <Form.Item name="allowed_devices" label="允许设备">
            <Input placeholder="设备ID逗号分隔，留空表示全部设备" />
          </Form.Item>
          <Form.Item name="rollout_percentage" label="灰度比例" initialValue={100}>
            <InputNumber min={0} max={100} addonAfter="%" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="entry_point" label="入口函数">
            <Input placeholder="可选" />
          </Form.Item>
          <Form.Item name="timeout" label="超时时间(秒)" initialValue={300}>
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="memory_limit" label="内存限制(MB)" initialValue={512}>
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="parameters" label="参数定义(JSON)" initialValue="{}">
            <Input.TextArea rows={3} placeholder='{"name": "value"}' style={{ fontFamily: 'monospace' }} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} placeholder="脚本描述..." />
          </Form.Item>
          {!editMode && (
            <Form.Item name="content" label="脚本内容" rules={[{ required: true, message: '请输入脚本内容' }]}>
              <Input.TextArea rows={12} placeholder="请输入脚本内容..." style={{ fontFamily: 'monospace' }} />
            </Form.Item>
          )}
        </Form>
      </Modal>

      {/* 编辑内容弹窗 */}
      <Modal
        title="编辑脚本内容"
        open={contentModalVisible}
        onOk={handleContentSubmit}
        onCancel={() => setContentModalVisible(false)}
        width={800}
      >
        <Form form={contentForm} layout="vertical">
          <Form.Item name="version" label="新版本号" rules={[{ required: true, message: '请输入新版本号' }]}>
            <Input placeholder="如：1.0.1" />
          </Form.Item>
          <Form.Item name="content" label="脚本内容" rules={[{ required: true, message: '请输入脚本内容' }]}>
            <Input.TextArea rows={20} placeholder="后端不返回明文内容，请粘贴完整新脚本内容" style={{ fontFamily: 'monospace' }} />
          </Form.Item>
        </Form>
      </Modal>

      {/* 详情弹窗 */}
      <Modal
        title="脚本详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={900}
      >
        {currentScript && (
          <Tabs items={[
            {
              key: 'info',
              label: '基本信息',
              children: (
                <Descriptions column={2} bordered size="small">
                  <Descriptions.Item label="脚本名称">{currentScript.name}</Descriptions.Item>
                  <Descriptions.Item label="脚本类型">{getTypeTag(currentScript.script_type)}</Descriptions.Item>
                  <Descriptions.Item label="版本">{currentScript.version}</Descriptions.Item>
                  <Descriptions.Item label="状态">{getStatusTag(currentScript.status)}</Descriptions.Item>
                  <Descriptions.Item label="所需功能">{listToInputText(currentScript.required_features) || '-'}</Descriptions.Item>
                  <Descriptions.Item label="下发次数">{currentScript.delivery_count || 0}</Descriptions.Item>
                  <Descriptions.Item label="内容哈希" span={2}>{currentScript.content_hash || currentScript.hash || '-'}</Descriptions.Item>
                  <Descriptions.Item label="描述" span={2}>{currentScript.description || '-'}</Descriptions.Item>
                  <Descriptions.Item label="创建时间">{currentScript.created_at?.slice(0, 19).replace('T', ' ')}</Descriptions.Item>
                  <Descriptions.Item label="更新时间">{currentScript.updated_at?.slice(0, 19).replace('T', ' ')}</Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'meta',
              label: '存储信息',
              children: (
                <Descriptions column={2} bordered size="small">
                  <Descriptions.Item label="原始大小">{currentScript.original_size || 0}</Descriptions.Item>
                  <Descriptions.Item label="加密大小">{currentScript.encrypted_size || 0}</Descriptions.Item>
                  <Descriptions.Item label="超时时间">{currentScript.timeout || 300}s</Descriptions.Item>
                  <Descriptions.Item label="内存限制">{currentScript.memory_limit || 512}MB</Descriptions.Item>
                  <Descriptions.Item label="参数定义" span={2}>{currentScript.parameters || '-'}</Descriptions.Item>
                  <Descriptions.Item label="允许设备" span={2}>{listToInputText(currentScript.allowed_devices) || '-'}</Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'deliveries',
              label: '下发记录',
              children: <Table columns={deliveryColumns} dataSource={deliveries} rowKey="id" size="small" pagination={{ pageSize: 10 }} />,
            },
          ]} />
        )}
      </Modal>
    </div>
  );
};

export default SecureScripts;
