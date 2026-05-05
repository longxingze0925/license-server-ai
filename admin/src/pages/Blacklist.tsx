import React, { useCallback, useEffect, useState } from 'react';
import { Table, Button, Space, message, Tag, Input, Card, Statistic, Row, Col, Spin, App } from 'antd';
import { DeleteOutlined, SearchOutlined, StopOutlined } from '@ant-design/icons';
import { blacklistApi } from '../api';
import { useAuthStore } from '../store';

const Blacklist: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const [loading, setLoading] = useState(false);
  const [pageLoading, setPageLoading] = useState(true);
  const [data, setData] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [searchText, setSearchText] = useState('');
  const canUpdateDevice = ['owner', 'admin', 'developer'].includes(user?.role || '');

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const result: any = await blacklistApi.list({ page, page_size: pageSize });
      const list = result?.list ?? (Array.isArray(result) ? result : []);
      setData(list);
      setTotal(result?.total || 0);
    } catch (error) {
      console.error(error);
      setData([]);
    } finally {
      setLoading(false);
      setPageLoading(false);
    }
  }, [page, pageSize]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleRemove = (record: any) => {
    if (!canUpdateDevice) {
      return;
    }

    modal.confirm({
      title: '确认移除',
      content: `确定要将机器码 "${record.machine_id}" 从黑名单中移除吗？移除后该设备将可以正常使用。`,
      onOk: async () => {
        try {
          await blacklistApi.remove(record.machine_id, record.app_id ? { app_id: record.app_id } : { scope: 'global' });
          message.success('已从黑名单移除');
          fetchData();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const filteredData = data.filter(item =>
    item.machine_id?.toLowerCase().includes(searchText.toLowerCase()) ||
    item.reason?.toLowerCase().includes(searchText.toLowerCase()) ||
    item.device_info?.toLowerCase().includes(searchText.toLowerCase())
  );

  const columns = [
    { title: '机器码', dataIndex: 'machine_id', key: 'machine_id', ellipsis: true },
    { title: '设备信息', dataIndex: 'device_info', key: 'device_info', ellipsis: true, render: (v: string) => v || '-' },
    { title: '操作系统', dataIndex: 'os', key: 'os', render: (v: string) => v || '-' },
    { title: '拉黑原因', dataIndex: 'reason', key: 'reason', ellipsis: true, render: (v: string) => v || '-' },
    {
      title: '拉黑类型', dataIndex: 'blacklist_type', key: 'blacklist_type',
      render: (type: string) => {
        const typeMap: Record<string, { color: string; text: string }> = {
          manual: { color: 'orange', text: '手动拉黑' },
          auto: { color: 'red', text: '自动拉黑' },
          security: { color: 'volcano', text: '安全拉黑' },
        };
        const t = typeMap[type] || { color: 'default', text: type || '未知' };
        return <Tag color={t.color}>{t.text}</Tag>;
      }
    },
    { title: '拉黑时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { title: '操作人', dataIndex: 'created_by', key: 'created_by', render: (v: string) => v || '系统' },
    ...(canUpdateDevice ? [{
      title: '操作', key: 'action',
      render: (_: any, record: any) => (
        <Space>
          <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleRemove(record)}>
            移除
          </Button>
        </Space>
      ),
    }] : []),
  ];

  // 统计数据
  const manualCount = data.filter(d => d.blacklist_type === 'manual').length;
  const autoCount = data.filter(d => d.blacklist_type === 'auto').length;
  const securityCount = data.filter(d => d.blacklist_type === 'security').length;

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
        <h2 style={{ margin: 0 }}>黑名单管理</h2>
      </div>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic
              title="黑名单总数"
              value={total}
              prefix={<StopOutlined />}
              valueStyle={{ color: '#ff4d4f' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="当前页手动拉黑"
              value={manualCount}
              valueStyle={{ color: '#fa8c16' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="当前页自动拉黑"
              value={autoCount}
              valueStyle={{ color: '#f5222d' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="当前页安全拉黑"
              value={securityCount}
              valueStyle={{ color: '#cf1322' }}
            />
          </Card>
        </Col>
      </Row>

      <Card>
        <div style={{ marginBottom: 16 }}>
          <Input
            placeholder="搜索机器码、原因、设备信息..."
            prefix={<SearchOutlined />}
            value={searchText}
            onChange={e => setSearchText(e.target.value)}
            style={{ width: 300 }}
            allowClear
          />
        </div>
        <Table
          columns={columns}
          dataSource={filteredData}
          rowKey="id"
          loading={loading}
          pagination={{
            current: page,
            pageSize: pageSize,
            total: total,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (t) => `共 ${t} 条记录`,
            onChange: (p, ps) => {
              setPage(p);
              setPageSize(ps);
            },
          }}
        />
      </Card>
    </div>
  );
};

export default Blacklist;
