import React, { useCallback, useEffect, useState } from 'react';
import {
  Table,
  Button,
  Space,
  Modal,
  Form,
  Input,
  InputNumber,
  Select,
  Switch,
  message,
  Tag,
  App,
  Drawer,
  Card,
  Statistic,
  Divider,
} from 'antd';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ThunderboltOutlined,
  ReloadOutlined,
  CalculatorOutlined,
} from '@ant-design/icons';
import { pricingRuleApi } from '../api';

const PROVIDER_OPTIONS = [
  { value: '*', label: '* (任意)' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'gpt', label: 'GPT' },
  { value: 'veo', label: 'Veo' },
  { value: 'sora', label: 'Sora' },
  { value: 'grok', label: 'Grok' },
  { value: 'claude', label: 'Claude（暂未接入）', disabled: true },
];

const SCOPE_OPTIONS = [
  { value: 'image', label: 'image (图片)' },
  { value: 'video', label: 'video (视频)' },
  { value: 'analysis', label: 'analysis (分析)' },
  { value: 'chat', label: 'chat (文本/Prompt 润色)' },
];

interface RuleRow {
  id: number;
  provider: string;
  scope: string;
  match_json: string;
  credits: number;
  formula: string;
  priority: number;
  enabled: boolean;
  note: string;
  created_at: string;
  updated_at: string;
}

const PricingRules: React.FC = () => {
  const { modal } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<RuleRow[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const [modalVisible, setModalVisible] = useState(false);
  const [current, setCurrent] = useState<RuleRow | null>(null);
  const [form] = Form.useForm();

  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewForm] = Form.useForm();
  const [previewResult, setPreviewResult] = useState<any>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const result: any = await pricingRuleApi.list({ page, page_size: pageSize });
      setData(result?.list || []);
      setTotal(result?.total || 0);
    } finally { setLoading(false); }
  }, [page, pageSize]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleCreate = () => {
    setCurrent(null);
    form.resetFields();
    form.setFieldsValue({ enabled: true, priority: 0, credits: 1, match_json: '{}' });
    setModalVisible(true);
  };

  const handleEdit = (record: RuleRow) => {
    setCurrent(record);
    form.setFieldsValue({ ...record, match_json: record.match_json || '{}' });
    setModalVisible(true);
  };

  const handleDelete = (record: RuleRow) => {
    modal.confirm({
      title: '确认删除',
      content: `规则 #${record.id}（${record.provider} / ${record.scope}）`,
      okType: 'danger',
      onOk: async () => {
        await pricingRuleApi.delete(record.id);
        message.success('删除成功');
        fetchData();
      },
    });
  };

  const handleSubmit = async () => {
    const values = await form.validateFields();
    if (!values.credits && !values.formula) {
      message.error('credits 与 formula 至少填一个');
      return;
    }
    if (current) {
      await pricingRuleApi.update(current.id, values);
      message.success('更新成功');
    } else {
      await pricingRuleApi.create(values);
      message.success('创建成功');
    }
    setModalVisible(false);
    fetchData();
  };

  const handlePreview = async () => {
    const values = await previewForm.validateFields();
    let parsedParams: Record<string, any> = {};
    try {
      parsedParams = values.params_json ? JSON.parse(values.params_json) : {};
    } catch {
      message.error('params 不是合法 JSON');
      return;
    }
    const result: any = await pricingRuleApi.preview({
      provider: values.provider,
      scope: values.scope,
      params: parsedParams,
    });
    setPreviewResult(result);
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    {
      title: 'Provider', dataIndex: 'provider', key: 'provider', width: 100,
      render: (v: string) => <Tag color={v === '*' ? 'default' : 'blue'}>{v}</Tag>,
    },
    {
      title: 'Scope', dataIndex: 'scope', key: 'scope', width: 100,
      render: (v: string) => <Tag color="cyan">{v}</Tag>,
    },
    {
      title: '匹配条件', dataIndex: 'match_json', key: 'match_json', ellipsis: true,
      render: (v: string) => v && v !== '{}' ? <code style={{ fontSize: 11 }}>{v}</code> : <span style={{ color: '#aaa' }}>(任意)</span>,
    },
    { title: 'credits', dataIndex: 'credits', key: 'credits', width: 80 },
    {
      title: 'formula', dataIndex: 'formula', key: 'formula', ellipsis: true,
      render: (v: string) => v ? <code style={{ fontSize: 11 }}>{v}</code> : '-',
    },
    { title: '优先级', dataIndex: 'priority', key: 'priority', width: 70 },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled', width: 60,
      render: (v: boolean) => <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '禁用'}</Tag>,
    },
    {
      title: '操作', key: 'action', width: 160,
      render: (_: any, record: RuleRow) => (
        <Space>
          <Button size="small" type="link" icon={<EditOutlined />} onClick={() => handleEdit(record)}>编辑</Button>
          <Button size="small" type="link" danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)}>删除</Button>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>计价规则</h2>
        <Space>
          <Button icon={<CalculatorOutlined />} onClick={() => { setPreviewResult(null); previewForm.resetFields(); setPreviewOpen(true); }}>试算</Button>
          <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>新建规则</Button>
        </Space>
      </div>

      <Table
        columns={columns} dataSource={data} rowKey="id" loading={loading}
        pagination={{
          current: page, pageSize, total, showSizeChanger: true,
          showTotal: t => `共 ${t} 条`,
          onChange: (p, ps) => { setPage(p); setPageSize(ps); },
        }}
      />

      <Modal
        title={current ? `编辑规则 #${current.id}` : '新建计价规则'}
        open={modalVisible} onOk={handleSubmit} onCancel={() => setModalVisible(false)}
        width={600} destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
            <Select options={PROVIDER_OPTIONS} />
          </Form.Item>
          <Form.Item name="scope" label="Scope" rules={[{ required: true }]} extra="能力发现里的 text，对应这里的 chat 计价；Prompt 润色/聊天都配 chat。">
            <Select options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="match_json" label="匹配条件 JSON" extra='例：{"model":"grok-imagine-video","duration_seconds":8}；duration 兼容旧写法，推荐使用 duration_seconds；参考图可用 reference_image_count；空对象 {} 表示任意'>
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="credits" label="基础扣点" extra="formula 非空时此值忽略">
            <InputNumber min={0} max={100000} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="formula" label="动态公式（可选）" extra='支持参数变量与 + - * /，例：duration_seconds * 2 + reference_image_count'>
            <Input placeholder="例：duration_seconds * 2 + reference_image_count" />
          </Form.Item>
          <Space size="large">
            <Form.Item name="priority" label="优先级（数字大优先）">
              <InputNumber min={0} max={1000} />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={256} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title="计价试算" open={previewOpen} onClose={() => setPreviewOpen(false)}
        width={520} destroyOnClose
        extra={<Button type="primary" icon={<ThunderboltOutlined />} onClick={handlePreview}>试算</Button>}
      >
        <Form form={previewForm} layout="vertical">
          <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
            <Select options={PROVIDER_OPTIONS.filter(o => o.value !== '*')} />
          </Form.Item>
          <Form.Item name="scope" label="Scope" rules={[{ required: true }]} extra="能力发现里的 text，对应这里的 chat 计价。">
            <Select options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="params_json" label="请求参数 JSON" extra='例：{"model":"grok-imagine-video","duration_seconds":8,"reference_image_count":3}'>
            <Input.TextArea rows={4} placeholder='{}' />
          </Form.Item>
        </Form>
        {previewResult && (
          <>
            <Divider />
            <Card>
              {previewResult.matched ? (
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Statistic title="扣点数" value={previewResult.cost} valueStyle={{ color: '#1677ff' }} />
                  <div>命中规则：<Tag>#{previewResult.rule_id}</Tag></div>
                  <pre style={{ background: '#f5f5f5', padding: 8, fontSize: 11, maxHeight: 200, overflow: 'auto' }}>
                    {JSON.stringify(previewResult.rule, null, 2)}
                  </pre>
                </Space>
              ) : (
                <div>
                  <Tag color="red">未匹配到规则</Tag>
                  <p style={{ color: '#aaa', marginTop: 8 }}>{previewResult.reason}</p>
                </div>
              )}
            </Card>
          </>
        )}
      </Drawer>
    </div>
  );
};

export default PricingRules;
