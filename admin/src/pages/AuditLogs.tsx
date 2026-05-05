import React, { useCallback, useEffect, useState } from 'react';
import { Table, Form, Select, DatePicker, Button, Tag, Modal, Descriptions, Card, Row, Col, Statistic } from 'antd';
import { FileTextOutlined } from '@ant-design/icons';
import { auditApi } from '../api';
import dayjs from 'dayjs';

const { Option } = Select;
const { RangePicker } = DatePicker;

const AuditLogs: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any[]>([]);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentLog, setCurrentLog] = useState<any>(null);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 });
  const [filters, setFilters] = useState<any>();
  const [stats, setStats] = useState<any>(null);

  const fetchData = useCallback(async (page = 1, pageSize = 20, filterParams: any = {}) => {
    setLoading(true);
    try {
      const res: any = await auditApi.list({ page, page_size: pageSize, ...filterParams });
      const list = res?.list || res?.data?.list || [];
      const total = res?.total || res?.data?.total || 0;
      setData(Array.isArray(list) ? list : []);
      setPagination({ current: page, pageSize, total });
    } catch (error) {
      console.error(error);
      setData([]);
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchStats = useCallback(async () => {
    try {
      const res: any = await auditApi.getStats({ days: 7 });
      setStats(res);
    } catch (error) {
      console.error(error);
    }
  }, []);

  useEffect(() => {
    fetchData();
    fetchStats();
  }, [fetchData, fetchStats]);

  const handleView = (record: any) => {
    setCurrentLog(record);
    setDetailVisible(true);
  };

  const handleTableChange = (pag: any) => {
    fetchData(pag.current, pag.pageSize, filters);
  };

  const handleSearch = (values: any) => {
    const params: any = {};
    if (values.action) params.action = values.action;
    if (values.resource) params.resource = values.resource;
    if (values.dateRange && values.dateRange.length === 2) {
      params.start_date = values.dateRange[0].format('YYYY-MM-DD');
      params.end_date = values.dateRange[1].format('YYYY-MM-DD');
    }
    setFilters(params);
    fetchData(1, pagination.pageSize, params);
  };

  const getActionTag = (action: string) => {
    const actionMap: Record<string, { color: string; text: string }> = {
      create: { color: 'green', text: '创建' },
      update: { color: 'blue', text: '更新' },
      delete: { color: 'red', text: '删除' },
      login: { color: 'purple', text: '登录' },
      revoke: { color: 'orange', text: '吊销' },
      reset: { color: 'cyan', text: '重置' },
    };
    const a = actionMap[action] || { color: 'default', text: action };
    return <Tag color={a.color}>{a.text}</Tag>;
  };

  const getResourceTag = (resource: string) => {
    const resourceMap: Record<string, string> = {
      user: '用户',
      application: '应用',
      license: '授权',
      subscription: '订阅',
      device: '设备',
      organization: '组织',
      script: '脚本',
      release: '版本',
      hotupdate: '热更新',
      publish_task: '发布任务',
      provider_credential: 'Provider 凭证',
      pricing_rule: '计价规则',
      credit: '额度',
      instruction: '实时指令',
    };
    return resourceMap[resource] || resource;
  };

  const getStatusTag = (code: number) => {
    if (code >= 200 && code < 300) {
      return <Tag color="green">{code}</Tag>;
    } else if (code >= 400 && code < 500) {
      return <Tag color="orange">{code}</Tag>;
    } else if (code >= 500) {
      return <Tag color="red">{code}</Tag>;
    }
    return <Tag>{code}</Tag>;
  };

  const columns = [
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm:ss'),
    },
    { title: '用户', dataIndex: 'user_email', key: 'user_email', ellipsis: true },
    { title: '操作', dataIndex: 'action', key: 'action', render: (a: string) => getActionTag(a) },
    { title: '资源', dataIndex: 'resource', key: 'resource', render: (r: string) => getResourceTag(r) },
    { title: '描述', dataIndex: 'description', key: 'description' },
    { title: 'IP地址', dataIndex: 'ip_address', key: 'ip_address' },
    { title: '状态码', dataIndex: 'response_code', key: 'response_code', render: (c: number) => getStatusTag(c) },
    { title: '耗时', dataIndex: 'duration', key: 'duration', render: (d: number) => `${d}ms` },
    {
      title: '操作',
      key: 'action_btn',
      width: 80,
      render: (_: any, record: any) => (
        <Button type="link" size="small" icon={<FileTextOutlined />} onClick={() => handleView(record)}>
          详情
        </Button>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>操作日志</h2>
      </div>

      {/* 统计卡片 */}
      {stats && (
        <Row gutter={16} style={{ marginBottom: 16 }}>
          <Col span={8}>
            <Card size="small">
              <Statistic
                title="近7天操作次数"
                value={stats.action_stats?.reduce((sum: number, item: any) => sum + item.count, 0) || 0}
              />
            </Card>
          </Col>
          <Col span={8}>
            <Card size="small">
              <Statistic
                title="活跃用户数"
                value={stats.user_stats?.length || 0}
              />
            </Card>
          </Col>
          <Col span={8}>
            <Card size="small">
              <Statistic
                title="涉及资源类型"
                value={stats.resource_stats?.length || 0}
              />
            </Card>
          </Col>
        </Row>
      )}

      {/* 搜索筛选 */}
      <Form layout="inline" onFinish={handleSearch} style={{ marginBottom: 16 }}>
        <Form.Item name="action">
          <Select placeholder="操作类型" allowClear style={{ width: 120 }}>
            <Option value="create">创建</Option>
            <Option value="update">更新</Option>
            <Option value="delete">删除</Option>
            <Option value="login">登录</Option>
            <Option value="revoke">吊销</Option>
            <Option value="reset">重置</Option>
          </Select>
        </Form.Item>
        <Form.Item name="resource">
          <Select placeholder="资源类型" allowClear style={{ width: 120 }}>
            <Option value="user">用户</Option>
            <Option value="application">应用</Option>
            <Option value="license">授权</Option>
            <Option value="subscription">订阅</Option>
            <Option value="device">设备</Option>
            <Option value="organization">组织</Option>
          </Select>
        </Form.Item>
        <Form.Item name="dateRange">
          <RangePicker />
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
        size="small"
      />

      {/* 详情弹窗 */}
      <Modal
        title="日志详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={700}
      >
        {currentLog && (
          <Descriptions column={2} bordered size="small" labelStyle={{ width: 100 }}>
            <Descriptions.Item label="时间">
              {dayjs(currentLog.created_at).format('YYYY-MM-DD HH:mm:ss')}
            </Descriptions.Item>
            <Descriptions.Item label="状态码">{getStatusTag(currentLog.response_code)}</Descriptions.Item>
            <Descriptions.Item label="操作">{getActionTag(currentLog.action)}</Descriptions.Item>
            <Descriptions.Item label="资源">{getResourceTag(currentLog.resource)}</Descriptions.Item>
            <Descriptions.Item label="描述">{currentLog.description}</Descriptions.Item>
            <Descriptions.Item label="耗时">{currentLog.duration}ms</Descriptions.Item>
            <Descriptions.Item label="操作用户" span={2}>{currentLog.user_email || '-'}</Descriptions.Item>
            <Descriptions.Item label="IP地址" span={2}>{currentLog.ip_address}</Descriptions.Item>
            {currentLog.request_body && (
              <Descriptions.Item label="请求内容" span={2}>
                <pre style={{
                  margin: 0,
                  fontSize: 12,
                  maxHeight: 300,
                  overflow: 'auto',
                  background: '#f5f5f5',
                  padding: 12,
                  borderRadius: 4,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all'
                }}>
                  {(() => {
                    try {
                      return JSON.stringify(JSON.parse(currentLog.request_body), null, 2);
                    } catch {
                      return currentLog.request_body;
                    }
                  })()}
                </pre>
              </Descriptions.Item>
            )}
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

export default AuditLogs;
