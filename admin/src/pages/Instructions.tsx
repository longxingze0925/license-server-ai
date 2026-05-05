import React, { useCallback, useEffect, useState } from 'react';
import { Table, Button, Space, Modal, Form, Input, message, Tag, Select, Card, Row, Col, Statistic, Badge, Spin } from 'antd';
import { SendOutlined, ReloadOutlined, DesktopOutlined } from '@ant-design/icons';
import { instructionApi, appApi } from '../api';

const Instructions: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [pageLoading, setPageLoading] = useState(true);
  const [data, setData] = useState<any[]>([]);
  const [apps, setApps] = useState<any[]>([]);
  const [selectedApp, setSelectedApp] = useState<string>('');
  const [onlineDevices, setOnlineDevices] = useState<any[]>([]);
  const [sendModalVisible, setSendModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentInstruction, setCurrentInstruction] = useState<any>(null);
  const [form] = Form.useForm();

  const formatJsonText = (value: any) => {
    if (value === undefined || value === null || value === '') return '';
    if (typeof value === 'string') {
      try {
        return JSON.stringify(JSON.parse(value), null, 2);
      } catch {
        return value;
      }
    }
    return JSON.stringify(value, null, 2);
  };

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
      console.error(error);
      setApps([]);
    } finally {
      setPageLoading(false);
    }
  };

  const fetchInstructions = useCallback(async () => {
    setLoading(true);
    try {
      const result: any = await instructionApi.list(selectedApp ? { app_id: selectedApp } : undefined);
      const list = result?.list ?? (Array.isArray(result) ? result : []);
      setData(list);
    } catch (error) {
      console.error(error);
      setData([]);
    } finally {
      setLoading(false);
    }
  }, [selectedApp]);

  const fetchOnlineDevices = useCallback(async () => {
    if (!selectedApp) return;
    try {
      const result: any = await instructionApi.getOnlineDevices(selectedApp);
      const list = result?.devices ?? result?.list ?? (Array.isArray(result) ? result : []);
      setOnlineDevices(list);
    } catch (error) {
      console.error(error);
      setOnlineDevices([]);
    }
  }, [selectedApp]);

  useEffect(() => {
    if (selectedApp) {
      fetchInstructions();
      fetchOnlineDevices();
    }
  }, [fetchInstructions, fetchOnlineDevices, selectedApp]);

  const handleSend = () => {
    form.resetFields();
    setSendModalVisible(true);
  };

  const handleView = async (record: any) => {
    try {
      const detail = await instructionApi.get(record.id);
      setCurrentInstruction(detail);
      setDetailVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      const result: any = await instructionApi.send({
        ...values,
        app_id: selectedApp,
      });
      const sentCount = Number(result?.sent_count ?? (result?.sent ? 1 : 0));
      if (sentCount > 0) {
        message.success(`指令已发送到 ${sentCount} 台设备`);
      } else {
        message.warning('当前没有在线设备，指令已创建但未发送');
      }
      setSendModalVisible(false);
      fetchInstructions();
      fetchOnlineDevices();
    } catch (error) {
      console.error(error);
    }
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      pending: { color: 'blue', text: '待执行' },
      sent: { color: 'cyan', text: '已发送' },
      acked: { color: 'geekblue', text: '已确认' },
      executed: { color: 'green', text: '已执行' },
      failed: { color: 'red', text: '失败' },
      expired: { color: 'orange', text: '已过期' },
    };
    const s = statusMap[status] || { color: 'default', text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const getTypeTag = (type: string) => {
    const typeMap: Record<string, { color: string; text: string }> = {
      click: { color: 'blue', text: '点击' },
      input: { color: 'green', text: '输入' },
      screenshot: { color: 'purple', text: '截图' },
      ocr: { color: 'cyan', text: 'OCR识别' },
      exec_script: { color: 'orange', text: '执行脚本' },
      get_status: { color: 'lime', text: '获取状态' },
      restart: { color: 'volcano', text: '重启' },
      shutdown: { color: 'red', text: '关机' },
      custom: { color: 'default', text: '自定义' },
    };
    const t = typeMap[type] || { color: 'default', text: type };
    return <Tag color={t.color}>{t.text}</Tag>;
  };

  const columns = [
    { title: '指令ID', dataIndex: 'id', key: 'id', ellipsis: true, width: 200 },
    { title: '指令类型', dataIndex: 'type', key: 'type', render: getTypeTag },
    { title: '目标设备', dataIndex: 'device_id', key: 'device_id', ellipsis: true, render: (v: string) => v || '全部设备' },
    { title: '状态', dataIndex: 'status', key: 'status', render: getStatusTag },
    {
      title: '设备回执', key: 'result_stats',
      render: (_: any, record: any) => {
        const total = record.result_count || 0;
        if (!total) return '-';
        return `${record.executed_count || 0} 成功 / ${record.failed_count || 0} 失败 / ${total} 总数`;
      }
    },
    { title: '优先级', dataIndex: 'priority', key: 'priority', render: (v: number) => v || 0 },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { title: '确认时间', dataIndex: 'acked_at', key: 'acked_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') || '-' },
    {
      title: '操作', key: 'action',
      render: (_: any, record: any) => (
        <Space>
          <Button type="link" size="small" onClick={() => handleView(record)}>详情</Button>
        </Space>
      ),
    },
  ];

  const deviceColumns = [
    { title: '设备ID', dataIndex: 'device_id', key: 'device_id', ellipsis: true },
    { title: '机器码', dataIndex: 'machine_id', key: 'machine_id', ellipsis: true },
    { title: '操作系统', dataIndex: 'os', key: 'os' },
    { title: '最后心跳', dataIndex: 'last_ping_at', key: 'last_ping_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    {
      title: '状态', key: 'status',
      render: () => <Badge status="success" text="在线" />
    },
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
        <h2 style={{ margin: 0 }}>实时指令管理</h2>
        <Space>
          <Select
            style={{ width: 200 }}
            placeholder="选择应用"
            value={selectedApp || undefined}
            onChange={setSelectedApp}
            options={apps.map(app => ({ label: app.name, value: app.id }))}
          />
          <Button icon={<ReloadOutlined />} onClick={() => { fetchInstructions(); fetchOnlineDevices(); }}>刷新</Button>
          <Button type="primary" icon={<SendOutlined />} onClick={handleSend} disabled={!selectedApp}>
            发送指令
          </Button>
        </Space>
      </div>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic
              title="在线设备"
              value={onlineDevices.length}
              prefix={<DesktopOutlined />}
              valueStyle={{ color: '#3f8600' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="待执行指令"
              value={data.filter(d => d.status === 'pending').length}
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="已执行指令"
              value={data.filter(d => d.status === 'executed').length}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="失败指令"
              value={data.filter(d => d.status === 'failed').length}
              valueStyle={{ color: '#ff4d4f' }}
            />
          </Card>
        </Col>
      </Row>

      <Card title="在线设备" style={{ marginBottom: 16 }} size="small">
        <Table
          columns={deviceColumns}
          dataSource={onlineDevices}
          rowKey="device_id"
          size="small"
          pagination={{ pageSize: 5 }}
        />
      </Card>

      <Card title="指令历史" size="small">
        <Table columns={columns} dataSource={data} rowKey="id" loading={loading} size="small" />
      </Card>

      {/* 发送指令弹窗 */}
      <Modal
        title="发送指令"
        open={sendModalVisible}
        onOk={handleSubmit}
        onCancel={() => setSendModalVisible(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="machine_id" label="目标设备" initialValue="">
            <Select
              placeholder="选择在线设备，留空表示广播"
              options={[
                { label: '全部在线设备', value: '' },
                ...onlineDevices.map(d => ({
                  label: `${d.machine_id} (${d.os || '未知'})`,
                  value: d.machine_id
                }))
              ]}
              showSearch
              filterOption={(input, option) =>
                (option?.label ?? '').toLowerCase().includes(input.toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item name="type" label="指令类型" initialValue="custom" rules={[{ required: true }]}>
            <Select options={[
              { label: '点击', value: 'click' },
              { label: '输入', value: 'input' },
              { label: '截图', value: 'screenshot' },
              { label: 'OCR识别', value: 'ocr' },
              { label: '获取状态', value: 'get_status' },
              { label: '重启', value: 'restart' },
              { label: '关机', value: 'shutdown' },
              { label: '执行脚本', value: 'exec_script' },
              { label: '自定义', value: 'custom' },
            ]} />
          </Form.Item>
          <Form.Item name="priority" label="优先级" initialValue={0}>
            <Select options={[
              { label: '低 (0)', value: 0 },
              { label: '中 (5)', value: 5 },
              { label: '高 (10)', value: 10 },
            ]} />
          </Form.Item>
          <Form.Item name="payload" label="指令内容 (JSON)" rules={[{ required: true, message: '请输入指令内容' }]}>
            <Input.TextArea
              rows={6}
              placeholder='{"action": "click", "x": 100, "y": 200}'
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
        </Form>
      </Modal>

      {/* 详情弹窗 */}
      <Modal
        title="指令详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={700}
      >
        {currentInstruction && (
          <div>
            <Row gutter={[16, 16]}>
              <Col span={12}><strong>指令ID:</strong> {currentInstruction.id}</Col>
              <Col span={12}><strong>指令类型:</strong> {getTypeTag(currentInstruction.type)}</Col>
              <Col span={12}><strong>目标设备:</strong> {currentInstruction.device_id || '全部设备'}</Col>
              <Col span={12}><strong>状态:</strong> {getStatusTag(currentInstruction.status)}</Col>
              <Col span={12}><strong>优先级:</strong> {currentInstruction.priority || 0}</Col>
              <Col span={12}><strong>创建时间:</strong> {currentInstruction.created_at?.slice(0, 19).replace('T', ' ')}</Col>
              <Col span={12}><strong>发送时间:</strong> {currentInstruction.sent_at?.slice(0, 19).replace('T', ' ') || '-'}</Col>
              <Col span={12}><strong>确认时间:</strong> {currentInstruction.acked_at?.slice(0, 19).replace('T', ' ') || '-'}</Col>
              <Col span={24}>
                <strong>指令内容:</strong>
                <Input.TextArea
                  value={formatJsonText(currentInstruction.payload)}
                  rows={6}
                  readOnly
                  style={{ fontFamily: 'monospace', marginTop: 8 }}
                />
              </Col>
              {currentInstruction.result && (
                <Col span={24}>
                  <strong>执行结果:</strong>
                  <Input.TextArea
                    value={formatJsonText(currentInstruction.result)}
                    rows={6}
                    readOnly
                    style={{ fontFamily: 'monospace', marginTop: 8 }}
                  />
                </Col>
              )}
              {Array.isArray(currentInstruction.results) && currentInstruction.results.length > 0 && (
                <Col span={24}>
                  <strong>设备回执:</strong>
                  <Table
                    style={{ marginTop: 8 }}
                    size="small"
                    rowKey="id"
                    pagination={false}
                    dataSource={currentInstruction.results}
                    columns={[
                      { title: '机器码', dataIndex: 'machine_id', key: 'machine_id', ellipsis: true },
                      { title: '设备ID', dataIndex: 'device_id', key: 'device_id', ellipsis: true },
                      { title: '状态', dataIndex: 'status', key: 'status', render: getStatusTag },
                      { title: '确认时间', dataIndex: 'acked_at', key: 'acked_at', render: (v: string) => v?.slice(0, 19).replace('T', ' ') || '-' },
                      { title: '结果', dataIndex: 'result', key: 'result', ellipsis: true, render: (v: string) => v || '-' },
                    ]}
                  />
                </Col>
              )}
              {currentInstruction.error_message && (
                <Col span={24}>
                  <strong>错误信息:</strong>
                  <div style={{ color: '#ff4d4f', marginTop: 8 }}>{currentInstruction.error_message}</div>
                </Col>
              )}
            </Row>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default Instructions;
