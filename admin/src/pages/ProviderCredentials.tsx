import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  AutoComplete,
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
  Tooltip,
  Alert,
} from 'antd';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ThunderboltOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { clientModelApi, providerCredentialApi } from '../api';

type ProviderOption = { value: string; label: string; disabled?: boolean };

const PROVIDER_OPTIONS: ProviderOption[] = [
  { value: 'gemini', label: 'Gemini' },
  { value: 'gpt', label: 'GPT' },
  { value: 'veo', label: 'Veo' },
  { value: 'sora', label: 'Sora' },
  { value: 'grok', label: 'Grok' },
  { value: 'claude', label: 'Claude（暂未接入）', disabled: true },
];

const PROVIDER_FILTER_OPTIONS = PROVIDER_OPTIONS.map(({ value, label }) => ({ value, label }));

// 各 Provider 支持的 mode 取值（与客户端 StudioContracts.cs 中的 *RequestModes 对齐）
const MODE_OPTIONS_BY_PROVIDER: Record<string, { value: string; label: string }[]> = {
  gemini: [
    { value: 'official', label: '官转 Official' },
    { value: 'duoyuan', label: '多元 DuoYuan' },
  ],
  gpt: [
    { value: 'official', label: '官转 Official' },
    { value: 'gzxsy', label: '工作室 Gzxsy' },
  ],
  veo: [
    { value: 'google', label: 'Google Native' },
    { value: 'adapter', label: 'Adapter API' },
    { value: 'duoyuan', label: '多元 DuoYuan' },
  ],
  sora: [
    { value: 'async', label: 'Async' },
    { value: 'chat', label: 'Chat' },
  ],
  grok: [
    { value: 'official', label: '官转 Official' },
    { value: 'duoyuan', label: '多元 DuoYuan' },
    { value: 'suchuang', label: '速创 SuChuang' },
  ],
  claude: [{ value: 'official', label: '官转 Official' }],
};

const HEALTH_LABEL: Record<string, { label: string; color: string }> = {
  unknown: { label: '未知', color: 'default' },
  healthy: { label: '健康', color: 'green' },
  degraded: { label: '降级', color: 'orange' },
  down: { label: '宕机', color: 'red' },
};

interface CredentialRow {
  id: string;
  provider: string;
  mode: string;
  channel_name: string;
  upstream_base: string;
  default_model?: string;
  custom_headers?: string;
  enabled: boolean;
  is_default: boolean;
  priority: number;
  health_status: string;
  last_used_at?: string;
  api_key_set: boolean;
  api_key_cipher_size?: number;
  note?: string;
  created_at: string;
}

interface ClientModelRow {
  id: string;
  model_key: string;
  display_name: string;
  provider: string;
  scope: string;
  enabled: boolean;
  routes?: unknown[];
}

interface UpstreamCapabilityRow {
  provider: string;
  mode: string;
  model: string;
  display_name: string;
  aspect_ratios: string[];
  durations: string[];
  resolutions: string[];
  max_images: number;
}

const ProviderCredentials: React.FC = () => {
  const { modal } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<CredentialRow[]>([]);
  const [clientModels, setClientModels] = useState<ClientModelRow[]>([]);
  const [upstreamCapabilities, setUpstreamCapabilities] = useState<UpstreamCapabilityRow[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [modalVisible, setModalVisible] = useState(false);
  const [current, setCurrent] = useState<CredentialRow | null>(null);
  const [form] = Form.useForm();
  const selectedProvider = Form.useWatch('provider', form);
  const selectedMode = Form.useWatch('mode', form);
  const [providerFilter, setProviderFilter] = useState<string>();
  const [modeOptions, setModeOptions] = useState<{ value: string; label: string }[]>([]);
  const selectedBindClientModelId = Form.useWatch('bind_client_model_id', form);
  const selectedBindUpstreamModel = Form.useWatch('bind_upstream_model', form);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const result: any = await providerCredentialApi.list({
        page,
        page_size: pageSize,
        provider: providerFilter,
      });
      setData(result?.list || []);
      setTotal(result?.total || 0);
    } catch (error) {
      console.error(error);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, providerFilter]);

  const fetchClientModels = useCallback(async () => {
    const result: any = await clientModelApi.list({ include_disabled: true, page: 1, page_size: 500 });
    setClientModels(result?.list || []);
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    fetchClientModels().catch(console.error);
  }, [fetchClientModels]);

  useEffect(() => {
    if (!selectedProvider || !selectedMode) {
      setUpstreamCapabilities([]);
      return;
    }
    clientModelApi.upstreamCapabilities({
      provider: selectedProvider,
      mode: selectedMode,
    }).then((result: any) => {
      setUpstreamCapabilities(result || []);
    }).catch((error) => {
      console.error(error);
      setUpstreamCapabilities([]);
    });
  }, [selectedProvider, selectedMode]);

  const bindClientModelOptions = useMemo(() => clientModels
    .filter(item => !selectedProvider || item.provider === selectedProvider)
    .map(item => ({
      value: item.id,
      label: `${item.display_name} / ${item.model_key}`,
      disabled: !item.enabled,
    })), [clientModels, selectedProvider]);

  const selectedBindClientModel = useMemo(
    () => clientModels.find(item => item.id === selectedBindClientModelId),
    [clientModels, selectedBindClientModelId]
  );

  const upstreamModelOptions = useMemo(() => upstreamCapabilities.map(item => ({
    value: item.model,
    label: `${item.display_name || item.model}${item.model ? ` / ${item.model}` : ''}`,
  })), [upstreamCapabilities]);

  const selectedBindCapability = useMemo(
    () => upstreamCapabilities.find(item => item.model === selectedBindUpstreamModel),
    [upstreamCapabilities, selectedBindUpstreamModel]
  );

  const handleProviderChange = (provider: string) => {
    setModeOptions(MODE_OPTIONS_BY_PROVIDER[provider] ?? []);
    form.setFieldValue('mode', undefined);
    form.setFieldValue('bind_client_model_id', undefined);
    form.setFieldValue('bind_upstream_model', undefined);
  };

  const handleCreate = () => {
    setCurrent(null);
    form.resetFields();
    setModeOptions([]);
    setUpstreamCapabilities([]);
    form.setFieldsValue({ enabled: true, priority: 0, is_default: false, bind_route_enabled: true });
    setModalVisible(true);
  };

  const handleEdit = (record: CredentialRow) => {
    setCurrent(record);
    setModeOptions(MODE_OPTIONS_BY_PROVIDER[record.provider] ?? []);
    form.setFieldsValue({
      provider: record.provider,
      mode: record.mode,
      channel_name: record.channel_name,
      upstream_base: record.upstream_base,
      default_model: record.default_model,
      custom_headers: record.custom_headers,
      enabled: record.enabled,
      is_default: record.is_default,
      priority: record.priority,
      note: record.note,
      api_key: '', // 留空表示不修改
    });
    setModalVisible(true);
  };

  const handleDelete = (record: CredentialRow) => {
    modal.confirm({
      title: '确认删除',
      content: `确定要删除凭证 "${record.channel_name}" 吗？`,
      okType: 'danger',
      onOk: async () => {
        try {
          await providerCredentialApi.delete(record.id);
          message.success('删除成功');
          fetchData();
        } catch (e) {
          console.error(e);
        }
      },
    });
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      if (current) {
        const payload: any = { ...values };
        if (!payload.api_key) {
          delete payload.api_key; // 留空 = 不修改
        }
        await providerCredentialApi.update(current.id, payload);
        message.success('更新成功');
      } else {
        if (!values.api_key) {
          message.error('新建必须填写 API Key');
          return;
        }
        const {
          bind_client_model_id,
          bind_upstream_model,
          bind_route_enabled,
          ...credentialPayload
        } = values;
        if (bind_client_model_id && !bind_upstream_model) {
          message.error('绑定客户端模型时请选择或填写上游真实模型');
          return;
        }
        const created: any = await providerCredentialApi.create(credentialPayload);
        if (bind_client_model_id) {
          if (!created?.id) {
            message.warning('凭证已创建，但接口没有返回凭证 ID，未能自动绑定客户端模型');
          } else {
            await clientModelApi.createRoute(bind_client_model_id, {
              credential_id: created.id,
              upstream_model: bind_upstream_model,
              enabled: bind_route_enabled !== false,
              is_default: (selectedBindClientModel?.routes?.length || 0) === 0,
              priority: credentialPayload.priority || 0,
              sort_order: selectedBindClientModel?.routes?.length || 0,
              aspect_ratios: selectedBindCapability?.aspect_ratios || [],
              durations: selectedBindCapability?.durations || [],
              resolutions: selectedBindCapability?.resolutions || [],
              max_images: selectedBindCapability?.max_images || 0,
            });
            message.success('创建成功，已绑定客户端模型');
          }
        } else {
          message.success('创建成功');
        }
      }
      setModalVisible(false);
      fetchData();
      fetchClientModels().catch(console.error);
    } catch {
      // form 校验错误已被 antd 捕获
    }
  };

  const handleTest = async (record: CredentialRow) => {
    const hide = message.loading(`正在测试 ${record.channel_name}...`, 0);
    try {
      const result: any = await providerCredentialApi.test(record.id);
      hide();
      if (result?.ok) {
        modal.success({
          title: '连通性测试成功',
          content: (
            <div>
              <p>HTTP 状态：{result.http_status}</p>
              <p>耗时：{result.latency_ms} ms</p>
              <p>探测方式：{result.probe_method || 'GET'}</p>
              <p>探测地址：<code>{result.probe_url}</code></p>
            </div>
          ),
        });
      } else {
        modal.error({
          title: '连通性测试失败',
          content: (
            <div>
              <p>HTTP 状态：{result?.http_status ?? '-'}</p>
              <p>耗时：{result?.latency_ms} ms</p>
              <p>探测方式：{result?.probe_method || 'GET'}</p>
              <p>原因：{result?.reason || '上游返回非 2xx'}</p>
              {result?.upstream_sample && (
                <pre style={{ background: '#f5f5f5', padding: 8, maxHeight: 200, overflow: 'auto' }}>
                  {result.upstream_sample}
                </pre>
              )}
            </div>
          ),
          width: 600,
        });
      }
      fetchData();
    } catch (e) {
      hide();
      console.error(e);
    }
  };

  const columns = [
    {
      title: 'Provider',
      dataIndex: 'provider',
      key: 'provider',
      width: 110,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: 'Mode',
      dataIndex: 'mode',
      key: 'mode',
      width: 110,
    },
    {
      title: '通道名',
      dataIndex: 'channel_name',
      key: 'channel_name',
    },
    {
      title: '上游地址',
      dataIndex: 'upstream_base',
      key: 'upstream_base',
      ellipsis: true,
      render: (v: string) => (
        <Tooltip title={v}>
          <code style={{ fontSize: 12 }}>{v}</code>
        </Tooltip>
      ),
    },
    {
      title: '默认',
      dataIndex: 'is_default',
      key: 'is_default',
      width: 60,
      render: (v: boolean) => (v ? <Tag color="gold">默认</Tag> : null),
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 80,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 70,
      render: (v: boolean) => (
        <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '禁用'}</Tag>
      ),
    },
    {
      title: '健康度',
      dataIndex: 'health_status',
      key: 'health_status',
      width: 90,
      render: (v: string) => {
        const meta = HEALTH_LABEL[v] || HEALTH_LABEL.unknown;
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: '最近调用',
      dataIndex: 'last_used_at',
      key: 'last_used_at',
      width: 160,
      render: (v?: string) => (v ? v.replace('T', ' ').slice(0, 19) : '-'),
    },
    {
      title: '操作',
      key: 'action',
      width: 260,
      render: (_: any, record: CredentialRow) => (
        <Space>
          <Tooltip title={record.provider === 'claude' ? 'Claude 暂未接入代理能力，不能测试为可用凭证' : undefined}>
            <Button
              type="primary"
              size="small"
              icon={<ThunderboltOutlined />}
              disabled={record.provider === 'claude'}
              onClick={() => handleTest(record)}
            >
              测试
            </Button>
          </Tooltip>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>
            编辑
          </Button>
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            onClick={() => handleDelete(record)}
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>AI Provider 凭证</h2>
        <Space>
          <Select
            allowClear
            placeholder="按 Provider 过滤"
            style={{ width: 180 }}
            options={PROVIDER_FILTER_OPTIONS}
            value={providerFilter}
            onChange={(v) => {
              setProviderFilter(v);
              setPage(1);
            }}
          />
          <Button icon={<ReloadOutlined />} onClick={fetchData}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>
            新建凭证
          </Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, ps) => {
            setPage(p);
            setPageSize(ps);
          },
        }}
      />

      <Modal
        title={current ? '编辑凭证' : '新建凭证'}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={640}
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item
            name="provider"
            label="Provider"
            rules={[{ required: true, message: '请选择 Provider' }]}
          >
            <Select
              options={PROVIDER_OPTIONS}
              onChange={handleProviderChange}
              disabled={!!current}
              placeholder="选择 AI 服务商"
            />
          </Form.Item>

          <Form.Item name="mode" label="Mode" rules={[{ required: true, message: '请选择接入方式' }]}>
            <Select options={modeOptions} placeholder="选择接入方式（mode）" disabled={!!current} />
          </Form.Item>

          {selectedProvider === 'grok' && selectedMode === 'duoyuan' && (
            <Alert
              showIcon
              type="info"
              style={{ marginBottom: 16 }}
              message="Grok 多元模式会按多元开发文档调用 /v1/video/create 创建任务、/v1/video/query 查询结果；默认模型建议填 grok-video-3。"
            />
          )}

          {selectedProvider === 'grok' && selectedMode === 'suchuang' && (
            <Alert
              showIcon
              type="info"
              style={{ marginBottom: 16 }}
              message="Grok 速创模式会调用 /v1/videos/generations；默认模型建议按速创平台文档填写。"
            />
          )}

          {selectedProvider === 'veo' && selectedMode === 'duoyuan' && (
            <Alert
              showIcon
              type="info"
              style={{ marginBottom: 16 }}
              message="Veo 多元模式会按多元开发文档调用 /v1/video/create 创建任务、/v1/video/query 查询结果；默认模型按多元后台可用模型填写。"
            />
          )}

          {selectedProvider === 'claude' && (
            <Alert
              showIcon
              type="warning"
              style={{ marginBottom: 16 }}
              message="Claude 当前只保留类型定义，代理 adapter 未接入，不能配置为可用凭证。"
            />
          )}

          <Form.Item
            name="channel_name"
            label="通道名"
            rules={[{ required: true, message: '请输入通道名' }, { max: 64 }]}
          >
            <Input placeholder="例如：veo-主力1号" />
          </Form.Item>

          <Form.Item
            name="upstream_base"
            label="上游 Base URL"
            rules={[{ required: true, message: '请输入上游地址' }]}
          >
            <Input placeholder="https://api.openai.com" />
          </Form.Item>

          <Form.Item
            name="api_key"
            label={current ? 'API Key（留空 = 不修改）' : 'API Key'}
            rules={current ? [] : [{ required: true, message: '请输入 API Key' }]}
            extra="入库立即信封加密；保存后无法再查看明文"
          >
            <Input.Password placeholder={current ? '留空表示不修改原 Key' : 'sk-...'} autoComplete="new-password" />
          </Form.Item>

          <Form.Item name="default_model" label="默认模型 (可选)">
            <Input placeholder="例如：gpt-4o-mini / gemini-2.5-flash / veo-3" />
          </Form.Item>

          {!current && (
            <>
              <Alert
                showIcon
                type="info"
                style={{ marginBottom: 16 }}
                message="如果这个凭证要给客户端模型使用，可以在这里直接绑定；保存后会自动创建一条真实渠道路由。"
              />
              <Form.Item name="bind_client_model_id" label="绑定到客户端模型（可选）">
                <Select
                  allowClear
                  showSearch
                  options={bindClientModelOptions}
                  optionFilterProp="label"
                  placeholder="例如：Veo 3.1 Fast"
                />
              </Form.Item>
              {selectedBindClientModelId && (
                <>
                  <Form.Item
                    name="bind_upstream_model"
                    label="上游真实模型"
                    rules={[{ required: true, message: '请选择或填写上游真实模型' }, { max: 120 }]}
                    extra="客户端仍然只显示上面的客户端模型名称；这里填的是发给真实渠道的模型 ID。"
                  >
                    <AutoComplete
                      options={upstreamModelOptions}
                      filterOption={(input, option) =>
                        String(option?.label ?? '').toLowerCase().includes(input.toLowerCase())
                        || String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
                      }
                      placeholder="例如：veo_3_1-fast / grok-video-3"
                    />
                  </Form.Item>
                  <Form.Item name="bind_route_enabled" label="路由启用" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                </>
              )}
            </>
          )}

          <Form.Item name="custom_headers" label="自定义请求头 JSON (可选)">
            <Input.TextArea rows={2} placeholder='{"X-DuoYuan-Token": "xxx"}' />
          </Form.Item>

          <Space size="large">
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="is_default" label="设为默认" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="priority" label="优先级">
              <InputNumber min={0} max={1000} />
            </Form.Item>
          </Space>

          <Form.Item name="note" label="备注 (可选)">
            <Input.TextArea rows={2} maxLength={256} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default ProviderCredentials;
