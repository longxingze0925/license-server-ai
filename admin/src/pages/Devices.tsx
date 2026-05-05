import React, { useCallback, useEffect, useState } from 'react';
import { Table, Button, Space, Modal, Form, Input, Select, message, Tag, Descriptions, Tooltip, App } from 'antd';
import { StopOutlined, CheckCircleOutlined, DeleteOutlined, DesktopOutlined, MobileOutlined, AppleOutlined, WindowsOutlined, AndroidOutlined } from '@ant-design/icons';
import { deviceApi } from '../api';
import { useAuthStore } from '../store';
import dayjs from 'dayjs';

const { Option } = Select;

const Devices: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any[]>([]);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentDevice, setCurrentDevice] = useState<any>(null);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [filters, setFilters] = useState<any>({});
  const canUpdateDevice = ['owner', 'admin', 'developer'].includes(user?.role || '');
  const canDeleteDevice = canUpdateDevice;

  const fetchData = useCallback(async (page = 1, pageSize = 10, filterParams: any = {}) => {
    setLoading(true);
    try {
      const res: any = await deviceApi.list({ page, page_size: pageSize, ...filterParams });
      const result = res?.data || res;
      const items = result?.list || result?.items || result;
      setData(Array.isArray(items) ? items : []);
      setPagination({ current: page, pageSize, total: result?.total || 0 });
    } catch (error) {
      console.error(error);
      setData([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleView = (record: any) => {
    setCurrentDevice(record);
    setDetailVisible(true);
  };

  const handleBlacklist = (record: any) => {
    if (!canUpdateDevice) {
      return;
    }

    modal.confirm({
      title: '加入黑名单',
      content: '确定要将此设备加入黑名单吗？加入后该设备将无法使用任何授权。',
      onOk: async () => {
        try {
          await deviceApi.blacklist(record.id);
          message.success('已加入黑名单');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleRemoveFromBlacklist = (record: any) => {
    if (!canUpdateDevice) {
      return;
    }

    modal.confirm({
      title: '移出黑名单',
      content: '确定要将此设备从当前应用黑名单移出吗？',
      onOk: async () => {
        try {
          await deviceApi.unblacklist(record.id);
          message.success('已移出黑名单');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleUnbind = (record: any) => {
    if (!canDeleteDevice) {
      return;
    }

    modal.confirm({
      title: '解绑设备',
      content: '确定要解绑此设备吗？解绑后该设备需要重新激活。',
      onOk: async () => {
        try {
          await deviceApi.unbind(record.id);
          message.success('设备已解绑');
          fetchData(pagination.current, pagination.pageSize, filters);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleTableChange = (pag: any) => {
    fetchData(pag.current, pag.pageSize, filters);
  };

  const handleSearch = (values: any) => {
    setFilters(values);
    fetchData(1, pagination.pageSize, values);
  };

  const getOsIcon = (osType: string) => {
    const os = osType?.toLowerCase() || '';
    if (os.includes('windows')) return <WindowsOutlined />;
    if (os.includes('mac') || os.includes('darwin') || os.includes('ios')) return <AppleOutlined />;
    if (os.includes('android')) return <AndroidOutlined />;
    if (os.includes('mobile')) return <MobileOutlined />;
    return <DesktopOutlined />;
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      active: { color: 'green', text: '在线' },
      inactive: { color: 'default', text: '离线' },
      blacklisted: { color: 'black', text: '黑名单' },
    };
    const s = statusMap[status] || { color: 'default', text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const columns = [
    {
      title: '设备名称',
      dataIndex: 'device_name',
      key: 'device_name',
      render: (text: string, record: any) => (
        <Space>
          {getOsIcon(record.os_type)}
          <span>{text || record.hostname || '未知设备'}</span>
        </Space>
      ),
    },
    {
      title: '机器码',
      dataIndex: 'machine_id',
      key: 'machine_id',
      render: (text: string) => (
        <Tooltip title={text}>
          <code>{text?.slice(0, 16)}...</code>
        </Tooltip>
      ),
    },
    {
      title: '操作系统',
      key: 'os',
      render: (_: any, record: any) => `${record.os_type || '-'} ${record.os_version || ''}`.trim(),
    },
    {
      title: 'IP地址',
      dataIndex: 'ip_address',
      key: 'ip_address',
      render: (ip: string, record: any) => (
        <Tooltip title={`${record.ip_country || ''} ${record.ip_city || ''}`}>
          {ip || '-'}
        </Tooltip>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => getStatusTag(status),
    },
    {
      title: '最后心跳',
      dataIndex: 'last_heartbeat_at',
      key: 'last_heartbeat_at',
      render: (v: string) => v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '-',
    },
    {
      title: '绑定时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => v?.slice(0, 10),
    },
    {
      title: '操作',
      key: 'action',
      width: canUpdateDevice ? 200 : 80,
      render: (_: any, record: any) => (
        <Space>
          <Button type="link" size="small" onClick={() => handleView(record)}>详情</Button>
          {canUpdateDevice && (
            record.status === 'blacklisted' ? (
              <Button type="link" size="small" icon={<CheckCircleOutlined />} onClick={() => handleRemoveFromBlacklist(record)}>
                移出黑名单
              </Button>
            ) : (
              <Button type="link" size="small" danger icon={<StopOutlined />} onClick={() => handleBlacklist(record)}>
                加黑名单
              </Button>
            )
          )}
          {canDeleteDevice && (
            <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleUnbind(record)}>
              解绑
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>设备管理</h2>
      </div>

      {/* 搜索筛选 */}
      <Form layout="inline" onFinish={handleSearch} style={{ marginBottom: 16 }}>
        <Form.Item name="status">
          <Select placeholder="状态" allowClear style={{ width: 120 }}>
            <Option value="active">在线</Option>
            <Option value="inactive">离线</Option>
            <Option value="blacklisted">黑名单</Option>
          </Select>
        </Form.Item>
        <Form.Item name="keyword">
          <Input placeholder="搜索设备名/机器码" allowClear />
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

      {/* 详情弹窗 */}
      <Modal
        title="设备详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={700}
      >
        {currentDevice && (
          <Descriptions column={2} bordered size="small">
            <Descriptions.Item label="设备名称">{currentDevice.device_name || '未知设备'}</Descriptions.Item>
            <Descriptions.Item label="状态">{getStatusTag(currentDevice.status)}</Descriptions.Item>
            <Descriptions.Item label="机器码" span={2}>
              <code style={{ wordBreak: 'break-all' }}>{currentDevice.machine_id}</code>
            </Descriptions.Item>
            <Descriptions.Item label="授权ID">
              <code>{currentDevice.license_id?.slice(0, 8)}...</code>
            </Descriptions.Item>
            <Descriptions.Item label="用户ID">
              {currentDevice.user_id ? <code>{currentDevice.user_id?.slice(0, 8)}...</code> : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="主机名">{currentDevice.hostname || '-'}</Descriptions.Item>
            <Descriptions.Item label="操作系统">{currentDevice.os_type || '-'}</Descriptions.Item>
            <Descriptions.Item label="系统版本">{currentDevice.os_version || '-'}</Descriptions.Item>
            <Descriptions.Item label="应用版本">{currentDevice.app_version || '-'}</Descriptions.Item>
            <Descriptions.Item label="IP地址">{currentDevice.ip_address || '-'}</Descriptions.Item>
            <Descriptions.Item label="IP归属地">{`${currentDevice.ip_country || ''} ${currentDevice.ip_city || ''}`.trim() || '-'}</Descriptions.Item>
            <Descriptions.Item label="最后心跳">
              {currentDevice.last_heartbeat_at ? dayjs(currentDevice.last_heartbeat_at).format('YYYY-MM-DD HH:mm:ss') : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="最后活跃">
              {currentDevice.last_active_at ? dayjs(currentDevice.last_active_at).format('YYYY-MM-DD HH:mm:ss') : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="绑定时间" span={2}>
              {dayjs(currentDevice.created_at).format('YYYY-MM-DD HH:mm:ss')}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

export default Devices;
