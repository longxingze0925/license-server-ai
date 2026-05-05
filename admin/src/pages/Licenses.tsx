import React, { useCallback, useEffect, useState } from 'react';
import { Table, Button, Space, Modal, Form, Input, Select, message, Tag, InputNumber, Descriptions, Checkbox, App } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, StopOutlined, ReloadOutlined, CopyOutlined, DownloadOutlined } from '@ant-design/icons';
import { licenseApi, appApi, customerApi, exportApi } from '../api';
import { useAuthStore } from '../store';
import dayjs from 'dayjs';
import type { AxiosResponse } from 'axios';

const { Option } = Select;

const Licenses: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any[]>([]);
  const [apps, setApps] = useState<any[]>([]);
  const [customers, setCustomers] = useState<any[]>([]);
  const [customerLoading, setCustomerLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentLicense, setCurrentLicense] = useState<any>(null);
  const [form] = Form.useForm();
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [filters, setFilters] = useState<any>({});
  const [selectedAppFeatures, setSelectedAppFeatures] = useState<string[]>([]);
  const [exporting, setExporting] = useState(false);
  const canManageLicense = ['owner', 'admin', 'developer'].includes(user?.role || '');
  const canExportLicense = user?.role === 'owner' || user?.role === 'admin' || user?.role === 'developer';

  const fetchData = useCallback(async (page = 1, pageSize = 10, filterParams: any = {}) => {
    setLoading(true);
    try {
      const result: any = await licenseApi.list({ page, page_size: pageSize, ...filterParams });
      setData(result.list || []);
      setPagination({ current: page, pageSize, total: result.total || 0 });
    } catch (error) {
      console.error(error);
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchApps = useCallback(async () => {
    try {
      const result: any = await appApi.list();
      setApps(result || []);
    } catch (error) {
      console.error(error);
    }
  }, []);

  const fetchCustomers = useCallback(async (keyword = '') => {
    setCustomerLoading(true);
    try {
      const result: any = await customerApi.list({
        page: 1,
        page_size: 100,
        keyword: keyword.trim() || undefined,
      });
      setCustomers(result.list || []);
    } catch (error) {
      console.error(error);
    } finally {
      setCustomerLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    fetchApps();
    fetchCustomers();
  }, [fetchApps, fetchData, fetchCustomers]);

  const handleCreate = () => {
    if (!canManageLicense) {
      return;
    }

    setCurrentLicense(null);
    form.resetFields();
    setSelectedAppFeatures([]);
    setModalVisible(true);
  };

  const handleEdit = async (record: any) => {
    if (!canManageLicense) {
      return;
    }

    try {
      const detail: any = await licenseApi.get(record.id);
      if (detail.customer_id && !customers.some(customer => customer.id === detail.customer_id)) {
        setCustomers(prev => [
          {
            id: detail.customer_id,
            email: detail.customer_email,
            name: detail.customer_name,
          },
          ...prev,
        ]);
      }
      setCurrentLicense(detail);
      const app = apps.find(a => a.id === detail.app_id);
      setSelectedAppFeatures(app?.features || []);
      form.setFieldsValue({
        ...detail,
        features: parseFeatureList(detail.features),
        notes: detail.notes || '',
      });
      setModalVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const handleAppChange = (appId: string) => {
    const app = apps.find(a => a.id === appId);
    setSelectedAppFeatures(app?.features || []);
    // 默认全选所有功能
    form.setFieldsValue({ features: app?.features || [] });
    // 设置默认设备数
    if (app?.max_devices_default) {
      form.setFieldsValue({ max_devices: app.max_devices_default });
    }
  };

  const handleView = async (record: any) => {
    try {
      const detail = await licenseApi.get(record.id);
      setCurrentLicense(detail);
      setDetailVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const handleDelete = (record: any) => {
    if (!canManageLicense) {
      return;
    }

    modal.confirm({
      title: '确认删除',
      content: `确定要删除此授权吗？`,
      onOk: async () => {
        try {
          await licenseApi.delete(record.id);
          message.success('删除成功');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleSubmit = async () => {
    if (!canManageLicense) {
      return;
    }

    try {
      const values = await form.validateFields();
      const submitData: any = {
        app_id: values.app_id,
        customer_id: values.customer_id,
        type: 'subscription', // 默认使用订阅类型
        max_devices: values.max_devices,
        unbind_limit: values.unbind_limit,
        duration_days: values.duration_days,
        features: values.features || [],
        notes: values.notes,
      };
      if (currentLicense) {
        await licenseApi.update(currentLicense.id, {
          max_devices: submitData.max_devices,
          unbind_limit: submitData.unbind_limit,
          features: submitData.features,
          notes: submitData.notes || '',
        });
        message.success('更新成功');
      } else {
        await licenseApi.create(submitData);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchData(pagination.current, pagination.pageSize, filters);
    } catch (error) {
      console.error(error);
    }
  };

  const handleRevoke = (record: any) => {
    if (!canManageLicense) {
      return;
    }

    modal.confirm({
      title: '吊销授权',
      content: '确定要吊销此授权吗？吊销后将无法使用。',
      onOk: async () => {
        try {
          await licenseApi.revoke(record.id);
          message.success('授权已吊销');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleReset = (record: any) => {
    if (!canManageLicense) {
      return;
    }

    modal.confirm({
      title: '重置设备',
      content: '确定要重置此授权的设备绑定吗？',
      onOk: async () => {
        try {
          await licenseApi.resetDevices(record.id);
          message.success('设备已重置');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleResetUnbindCount = (record: any) => {
    if (!canManageLicense) {
      return;
    }

    modal.confirm({
      title: '重置解绑次数',
      content: '确定要重置该授权的客户端解绑次数吗？',
      onOk: async () => {
        try {
          await licenseApi.resetUnbindCount(record.id);
          message.success('解绑次数已重置');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    message.success('已复制到剪贴板');
  };

  const handleTableChange = (pag: any) => {
    fetchData(pag.current, pag.pageSize, filters);
  };

  const handleSearch = (values: any) => {
    setFilters(values);
    fetchData(1, pagination.pageSize, values);
  };

  const handleExport = async () => {
    if (!canExportLicense) {
      return;
    }

    if (exporting) {
      return;
    }

    setExporting(true);
    try {
      const response = await exportApi.licenses(filters);
      saveBlob(response.data, getDownloadFilename(response, `licenses_export_${dayjs().format('YYYYMMDD_HHmmss')}.csv`));
      message.success('导出任务已开始，请等待下载');
    } catch (error) {
      console.error(error);
      message.error('导出失败');
    } finally {
      setExporting(false);
    }
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      pending: { color: 'default', text: '待激活' },
      active: { color: 'green', text: '已激活' },
      expired: { color: 'red', text: '已过期' },
      revoked: { color: 'orange', text: '已吊销' },
    };
    const s = statusMap[status] || { color: 'default', text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const columns = [
    {
      title: '授权码',
      dataIndex: 'license_key',
      key: 'license_key',
      render: (text: string) => (
        <Space>
          <code>{text?.slice(0, 16)}...</code>
          <Button type="link" size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(text)} />
        </Space>
      ),
    },
    {
      title: '应用',
      dataIndex: 'app_id',
      key: 'app_id',
      render: (id: string) => apps.find(a => a.id === id)?.name || id,
    },
    { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => getStatusTag(s) },
    { title: '设备数', dataIndex: 'max_devices', key: 'max_devices' },
    {
      title: '解绑剩余',
      key: 'unbind_remaining',
      render: (_: any, record: any) => `${record.unbind_remaining ?? 0}/${record.unbind_limit ?? 0}`,
    },
    {
      title: '过期时间',
      dataIndex: 'expires_at',
      key: 'expires_at',
      render: (v: string) => v ? dayjs(v).format('YYYY-MM-DD') : '永久',
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => v?.slice(0, 10) },
    {
      title: '操作', key: 'action', width: 280,
      render: (_: any, record: any) => (
        <Space>
          <Button type="link" size="small" onClick={() => handleView(record)}>详情</Button>
          {canManageLicense && <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>编辑</Button>}
          {canManageLicense && <Button type="link" size="small" onClick={() => handleResetUnbindCount(record)}>重置解绑</Button>}
          {canManageLicense && <Button type="link" size="small" icon={<ReloadOutlined />} onClick={() => handleReset(record)}>重置</Button>}
          {canManageLicense && record.status === 'active' && (
            <Button type="link" size="small" danger icon={<StopOutlined />} onClick={() => handleRevoke(record)}>吊销</Button>
          )}
          {canManageLicense && <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)}>删除</Button>}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <h2 style={{ margin: 0 }}>授权管理</h2>
        <Space>
          {canExportLicense && <Button icon={<DownloadOutlined />} loading={exporting} onClick={handleExport}>导出</Button>}
          {canManageLicense && <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>创建授权</Button>}
        </Space>
      </div>

      {/* 搜索筛选 */}
      <Form layout="inline" onFinish={handleSearch} style={{ marginBottom: 16 }}>
        <Form.Item name="app_id">
          <Select placeholder="选择应用" allowClear style={{ width: 150 }}>
            {apps.map(app => <Option key={app.id} value={app.id}>{app.name}</Option>)}
          </Select>
        </Form.Item>
        <Form.Item name="status">
          <Select placeholder="状态" allowClear style={{ width: 120 }}>
            <Option value="pending">待激活</Option>
            <Option value="active">已激活</Option>
            <Option value="expired">已过期</Option>
            <Option value="revoked">已吊销</Option>
          </Select>
        </Form.Item>
        <Form.Item name="keyword">
          <Input placeholder="搜索授权码" allowClear />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit">搜索</Button>
        </Form.Item>
      </Form>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={pagination}
        onChange={handleTableChange}
      />

      {/* 创建/编辑弹窗 */}
      <Modal
        title={currentLicense ? '编辑授权' : '创建授权'}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="app_id" label="应用" rules={[{ required: true, message: '请选择应用' }]}>
            <Select placeholder="选择应用" disabled={!!currentLicense} onChange={handleAppChange}>
              {apps.map(app => <Option key={app.id} value={app.id}>{app.name}</Option>)}
            </Select>
          </Form.Item>
          <Form.Item name="customer_id" label="客户" rules={[{ required: true, message: '请选择客户' }]}>
            <Select
              placeholder="搜索客户邮箱 / 姓名 / 公司"
              showSearch
              filterOption={false}
              loading={customerLoading}
              onSearch={fetchCustomers}
              onOpenChange={(open) => {
                if (open) {
                  fetchCustomers();
                }
              }}
            >
              {customers.map(customer => (
                <Option key={customer.id} value={customer.id}>
                  {customer.email}{customer.name ? ` (${customer.name})` : ''}
                </Option>
              ))}
            </Select>
          </Form.Item>
          <Form.Item name="max_devices" label="最大设备数" initialValue={1}>
            <InputNumber min={1} max={100} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="unbind_limit"
            label="终身解绑总次数"
            initialValue={5}
            rules={[{ required: true, message: '请输入终身解绑总次数' }]}
            extra="客户端累计解绑总上限；填 0 表示禁止客户端自助解绑，超限后只能管理员后台解绑"
          >
            <InputNumber min={0} max={1000} style={{ width: '100%' }} />
          </Form.Item>
          {!currentLicense && (
            <Form.Item name="duration_days" label="有效天数" initialValue={365} rules={[{ required: true, message: '请输入有效天数' }]} extra="-1表示永久有效">
              <InputNumber min={-1} style={{ width: '100%' }} />
            </Form.Item>
          )}
          <Form.Item name="features" label="功能权限">
            {selectedAppFeatures.length > 0 ? (
              <Checkbox.Group>
                <Space direction="vertical">
                  {selectedAppFeatures.map(feature => (
                    <Checkbox key={feature} value={feature}>{feature}</Checkbox>
                  ))}
                </Space>
              </Checkbox.Group>
            ) : (
              <span style={{ color: '#999' }}>请先选择应用，或该应用未配置功能列表</span>
            )}
          </Form.Item>
          <Form.Item name="notes" label="备注">
            <Input.TextArea placeholder="备注信息" rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      {/* 详情弹窗 */}
      <Modal
        title="授权详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={700}
      >
        {currentLicense && (
          <Descriptions column={2} bordered size="small">
            <Descriptions.Item label="授权码" span={2}>
              <code>{currentLicense.license_key}</code>
              <Button type="link" size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(currentLicense.license_key)} />
            </Descriptions.Item>
            <Descriptions.Item label="应用">
              {apps.find(a => a.id === currentLicense.app_id)?.name || currentLicense.app_id}
            </Descriptions.Item>
            <Descriptions.Item label="客户">
              {currentLicense.customer_email || currentLicense.customer_name || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="状态">{getStatusTag(currentLicense.status)}</Descriptions.Item>
            <Descriptions.Item label="最大设备数">{currentLicense.max_devices}</Descriptions.Item>
            <Descriptions.Item label="已用设备数">{currentLicense.used_devices || 0}</Descriptions.Item>
            <Descriptions.Item label="终身解绑总次数">{currentLicense.unbind_limit ?? 0}</Descriptions.Item>
            <Descriptions.Item label="已用解绑次数">{currentLicense.unbind_used ?? 0}</Descriptions.Item>
            <Descriptions.Item label="剩余解绑次数">{currentLicense.unbind_remaining ?? 0}</Descriptions.Item>
            <Descriptions.Item label="有效天数">
              {currentLicense.duration_days === -1 ? '永久' : `${currentLicense.duration_days} 天`}
            </Descriptions.Item>
            <Descriptions.Item label="过期时间">
              {currentLicense.expires_at ? dayjs(currentLicense.expires_at).format('YYYY-MM-DD HH:mm') : '激活后计算'}
            </Descriptions.Item>
            <Descriptions.Item label="激活时间">
              {currentLicense.activated_at ? dayjs(currentLicense.activated_at).format('YYYY-MM-DD HH:mm') : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="最后心跳">
              {currentLicense.last_heartbeat ? dayjs(currentLicense.last_heartbeat).format('YYYY-MM-DD HH:mm:ss') : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="创建时间" span={2}>
              {dayjs(currentLicense.created_at).format('YYYY-MM-DD HH:mm')}
            </Descriptions.Item>
            <Descriptions.Item label="功能权限" span={2}>
              {formatFeatureList(currentLicense.features)}
            </Descriptions.Item>
            <Descriptions.Item label="备注" span={2}>{currentLicense.notes || '-'}</Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function getDownloadFilename(response: AxiosResponse<Blob>, fallback: string) {
  const disposition = response.headers['content-disposition'];
  if (typeof disposition !== 'string') {
    return fallback;
  }

  const utf8Name = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (utf8Name) {
    return decodeURIComponent(utf8Name);
  }

  return disposition.match(/filename="?([^"]+)"?/i)?.[1] || fallback;
}

function parseFeatureList(features: any): string[] {
  if (!features) {
    return [];
  }
  try {
    const parsed = typeof features === 'string' ? JSON.parse(features) : features;
    if (Array.isArray(parsed)) {
      return parsed.filter((item): item is string => typeof item === 'string');
    }
    if (parsed && typeof parsed === 'object') {
      return Object.keys(parsed).filter(key => parsed[key]);
    }
  } catch {
    return [];
  }
  return [];
}

function formatFeatureList(features: any): string {
  const list = parseFeatureList(features);
  return list.length > 0 ? list.join(', ') : '-';
}

export default Licenses;
