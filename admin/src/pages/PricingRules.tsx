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
  Collapse,
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
  { value: '*', label: '任意服务商' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'gpt', label: 'GPT' },
  { value: 'veo', label: 'Veo' },
  { value: 'sora', label: 'Sora' },
  { value: 'grok', label: 'Grok' },
  { value: 'claude', label: 'Claude（暂未接入）', disabled: true },
];

const PROVIDER_LABELS: Record<string, string> = {
  '*': '任意服务商',
  gemini: 'Gemini',
  gpt: 'GPT',
  veo: 'Veo',
  sora: 'Sora',
  grok: 'Grok',
  claude: 'Claude',
};

const SCOPE_OPTIONS = [
  { value: 'image', label: '图片生成' },
  { value: 'video', label: '视频生成' },
  { value: 'analysis', label: '图片/内容分析' },
  { value: 'chat', label: '聊天 / Prompt 润色' },
];

const SCOPE_LABELS: Record<string, string> = {
  image: '图片生成',
  video: '视频生成',
  analysis: '图片/内容分析',
  chat: '聊天 / Prompt 润色',
};

const TARGET_OPTIONS = [
  { value: 'default', label: '默认价格' },
  { value: 'model', label: '指定模型' },
  { value: 'mode', label: '指定模式' },
  { value: 'advanced', label: '高级规则' },
];

const SIMPLE_PRIORITY: Record<string, number> = {
  default: 10,
  model: 100,
  mode: 100,
};

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

interface RuleMatchInfo {
  targetType: 'default' | 'model' | 'mode' | 'advanced';
  value?: string;
}

const parseMatchJSON = (value?: string): Record<string, any> | null => {
  const text = (value || '').trim();
  if (!text || text === '{}' || text === 'null') {
    return {};
  }
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed;
    }
  } catch {
    return null;
  }
  return null;
};

const inferRuleMatchInfo = (record?: Partial<RuleRow> | null): RuleMatchInfo => {
  const parsed = parseMatchJSON(record?.match_json);
  if (!parsed || (record?.formula || '').trim()) {
    return { targetType: 'advanced' };
  }
  const keys = Object.keys(parsed);
  if (keys.length === 0) {
    return { targetType: 'default' };
  }
  if (keys.length === 1 && keys[0] === 'model') {
    return { targetType: 'model', value: String(parsed.model ?? '') };
  }
  if (keys.length === 1 && keys[0] === 'mode') {
    return { targetType: 'mode', value: String(parsed.mode ?? '') };
  }
  return { targetType: 'advanced' };
};

const formatMatchScope = (record: RuleRow) => {
  const info = inferRuleMatchInfo(record);
  if (info.targetType === 'default') {
    return <Tag color="default">默认</Tag>;
  }
  if (info.targetType === 'model') {
    return (
      <Space size={4}>
        <Tag color="blue">模型</Tag>
        <code style={{ fontSize: 11 }}>{info.value}</code>
      </Space>
    );
  }
  if (info.targetType === 'mode') {
    return (
      <Space size={4}>
        <Tag color="purple">模式</Tag>
        <code style={{ fontSize: 11 }}>{info.value}</code>
      </Space>
    );
  }
  return (
    <Space size={4}>
      <Tag color="orange">高级规则</Tag>
      <code style={{ fontSize: 11 }}>{record.match_json || '{}'}</code>
    </Space>
  );
};

const buildSubmitPayload = (values: any) => {
  const targetType = values.target_type || 'default';
  let matchJSON = '{}';
  let priority = SIMPLE_PRIORITY[targetType] ?? 10;
  let formula = '';

  if (targetType === 'model') {
    matchJSON = JSON.stringify({ model: String(values.model_match || '').trim() });
  } else if (targetType === 'mode') {
    matchJSON = JSON.stringify({ mode: String(values.mode_match || '').trim() });
  } else if (targetType === 'advanced') {
    matchJSON = values.match_json || '{}';
    formula = String(values.formula || '').trim();
    priority = values.priority ?? 0;
  }

  return {
    provider: values.provider,
    scope: values.scope,
    match_json: matchJSON,
    credits: values.credits,
    formula,
    priority,
    enabled: values.enabled,
    note: values.note,
  };
};

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
  const targetType = Form.useWatch('target_type', form);

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
    form.setFieldsValue({
      provider: 'grok',
      scope: 'video',
      target_type: 'default',
      enabled: true,
      priority: 10,
      credits: 10,
      match_json: '{}',
    });
    setModalVisible(true);
  };

  const handleEdit = (record: RuleRow) => {
    const matchInfo = inferRuleMatchInfo(record);
    setCurrent(record);
    form.setFieldsValue({
      ...record,
      target_type: matchInfo.targetType,
      model_match: matchInfo.targetType === 'model' ? matchInfo.value : undefined,
      mode_match: matchInfo.targetType === 'mode' ? matchInfo.value : undefined,
      match_json: record.match_json || '{}',
    });
    setModalVisible(true);
  };

  const handleDelete = (record: RuleRow) => {
    modal.confirm({
      title: '确认删除',
      content: `价格 #${record.id}（${PROVIDER_LABELS[record.provider] || record.provider} / ${SCOPE_LABELS[record.scope] || record.scope}）`,
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
    const payload = buildSubmitPayload(values);
    if (!payload.credits && !payload.formula) {
      message.error('扣点必须大于 0');
      return;
    }
    if (current) {
      await pricingRuleApi.update(current.id, payload);
      message.success('更新成功');
    } else {
      await pricingRuleApi.create(payload);
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
      message.error('请求参数不是合法 JSON');
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
      title: '服务商', dataIndex: 'provider', key: 'provider', width: 120,
      render: (v: string) => <Tag color={v === '*' ? 'default' : 'blue'}>{PROVIDER_LABELS[v] || v}</Tag>,
    },
    {
      title: '功能', dataIndex: 'scope', key: 'scope', width: 140,
      render: (v: string) => <Tag color="cyan">{SCOPE_LABELS[v] || v}</Tag>,
    },
    {
      title: '适用范围', dataIndex: 'match_json', key: 'match_json', ellipsis: true,
      render: (_: string, record: RuleRow) => formatMatchScope(record),
    },
    {
      title: '计费方式', key: 'billing_type', width: 110,
      render: (_: any, record: RuleRow) => record.formula ? <Tag color="orange">动态公式</Tag> : <Tag color="green">固定扣点</Tag>,
    },
    {
      title: '扣点', key: 'cost', width: 140,
      render: (_: any, record: RuleRow) => record.formula ? <code style={{ fontSize: 11 }}>{record.formula}</code> : record.credits,
    },
    {
      title: '状态', dataIndex: 'enabled', key: 'enabled', width: 80,
      render: (v: boolean) => <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '停用'}</Tag>,
    },
    {
      title: '备注', dataIndex: 'note', key: 'note', ellipsis: true,
      render: (v: string) => v || '-',
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
        <h2 style={{ margin: 0 }}>模型价格</h2>
        <Space>
          <Button icon={<CalculatorOutlined />} onClick={() => { setPreviewResult(null); previewForm.resetFields(); setPreviewOpen(true); }}>试算</Button>
          <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>新建价格</Button>
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
        title={current ? `编辑价格 #${current.id}` : '新建模型价格'}
        open={modalVisible} onOk={handleSubmit} onCancel={() => setModalVisible(false)}
        width={680} destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="provider" label="服务商" rules={[{ required: true, message: '请选择服务商' }]}>
            <Select options={PROVIDER_OPTIONS} />
          </Form.Item>
          <Form.Item name="scope" label="功能" rules={[{ required: true, message: '请选择功能' }]}>
            <Select options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="target_type" label="适用范围" rules={[{ required: true, message: '请选择适用范围' }]}>
            <Select options={TARGET_OPTIONS} />
          </Form.Item>
          {targetType === 'model' && (
            <Form.Item name="model_match" label="模型名" rules={[{ required: true, message: '请输入模型名' }]}>
              <Input placeholder="例如 grok-imagine-video" />
            </Form.Item>
          )}
          {targetType === 'mode' && (
            <Form.Item name="mode_match" label="模式名" rules={[{ required: true, message: '请输入模式名' }]}>
              <Input placeholder="例如 suchuang" />
            </Form.Item>
          )}
          <Form.Item
            name="credits"
            label={targetType === 'advanced' ? '固定扣点' : '扣点'}
            rules={[
              {
                validator: async (_, value) => {
                  const currentTargetType = form.getFieldValue('target_type');
                  const formula = String(form.getFieldValue('formula') || '').trim();
                  if (currentTargetType === 'advanced') {
                    if ((!value || value <= 0) && !formula) {
                      throw new Error('固定扣点和动态公式至少填一个');
                    }
                    return;
                  }
                  if (!value || value <= 0) {
                    throw new Error('请输入大于 0 的扣点');
                  }
                },
              },
            ]}
            extra={targetType === 'advanced' ? '高级规则可只填动态公式，不填固定扣点。' : undefined}
          >
            <InputNumber min={targetType === 'advanced' ? 0 : 1} max={100000} style={{ width: '100%' }} />
          </Form.Item>
          <Space size="large">
            <Form.Item name="enabled" label="状态" valuePropName="checked">
              <Switch checkedChildren="启用" unCheckedChildren="停用" />
            </Form.Item>
          </Space>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={256} />
          </Form.Item>

          <Collapse
            size="small"
            items={[
              {
                key: 'advanced',
                label: '高级设置',
                children: (
                  <>
                    <Form.Item name="match_json" label="匹配条件 JSON" extra='空对象 {} 表示默认价格；例：{"model":"grok-imagine-video","duration_seconds":8}'>
                      <Input.TextArea rows={2} disabled={targetType !== 'advanced'} />
                    </Form.Item>
                    <Form.Item name="formula" label="动态公式" extra="只有选择高级规则时才会生效；例：duration_seconds * 2 + reference_image_count">
                      <Input disabled={targetType !== 'advanced'} placeholder="例：duration_seconds * 2 + reference_image_count" />
                    </Form.Item>
                    <Form.Item name="priority" label="优先级">
                      <InputNumber min={0} max={1000} disabled={targetType !== 'advanced'} />
                    </Form.Item>
                  </>
                ),
              },
            ]}
          />
        </Form>
      </Modal>

      <Drawer
        title="计价试算" open={previewOpen} onClose={() => setPreviewOpen(false)}
        width={520} destroyOnClose
        extra={<Button type="primary" icon={<ThunderboltOutlined />} onClick={handlePreview}>试算</Button>}
      >
        <Form form={previewForm} layout="vertical">
          <Form.Item name="provider" label="服务商" rules={[{ required: true }]}>
            <Select options={PROVIDER_OPTIONS.filter(o => o.value !== '*')} />
          </Form.Item>
          <Form.Item name="scope" label="功能" rules={[{ required: true }]}>
            <Select options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="params_json" label="请求参数 JSON" extra='例：{"model":"grok-imagine-video","duration_seconds":8,"reference_image_count":3}'>
            <Input.TextArea rows={4} placeholder="{}" />
          </Form.Item>
        </Form>
        {previewResult && (
          <>
            <Divider />
            <Card>
              {previewResult.matched ? (
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Statistic title="扣点数" value={previewResult.cost} valueStyle={{ color: '#1677ff' }} />
                  <div>命中价格：<Tag>#{previewResult.rule_id}</Tag></div>
                  <pre style={{ background: '#f5f5f5', padding: 8, fontSize: 11, maxHeight: 200, overflow: 'auto' }}>
                    {JSON.stringify(previewResult.rule, null, 2)}
                  </pre>
                </Space>
              ) : (
                <div>
                  <Tag color="red">未匹配到价格</Tag>
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
