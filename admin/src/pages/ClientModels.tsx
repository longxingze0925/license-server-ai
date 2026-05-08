import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  App,
  Alert,
  AutoComplete,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { clientModelApi, providerCredentialApi } from '../api';

const PROVIDER_OPTIONS = [
  { value: 'gemini', label: 'Gemini' },
  { value: 'gpt', label: 'GPT' },
  { value: 'veo', label: 'Veo' },
  { value: 'sora', label: 'Sora' },
  { value: 'grok', label: 'Grok' },
];

const SCOPE_OPTIONS = [
  { value: 'video', label: '视频' },
  { value: 'image', label: '图片' },
  { value: 'analysis', label: '分析' },
  { value: 'chat', label: '文字' },
];

const MODE_OPTIONS = [
  { value: 'text_to_video', label: '文生视频' },
  { value: 'image_to_video', label: '图生视频' },
  { value: 'video_to_video', label: '爆款复刻' },
  { value: 'text_to_image', label: '文生图片' },
  { value: 'image_to_image', label: '图生图片' },
];

const DEFAULT_MODES_BY_SCOPE: Record<string, string[]> = {
  video: ['text_to_video', 'image_to_video'],
  image: ['text_to_image', 'image_to_image'],
  analysis: [],
  chat: [],
};

interface CredentialRow {
  id: string;
  provider: string;
  mode: string;
  channel_name: string;
  default_model?: string;
  enabled: boolean;
  health_status?: string;
}

interface ClientModelRouteRow {
  id: string;
  client_model_id: string;
  credential_id: string;
  upstream_model: string;
  enabled: boolean;
  is_default: boolean;
  priority: number;
  sort_order: number;
  aspect_ratios: string[];
  durations: string[];
  resolutions: string[];
  max_images: number;
  effective_aspect_ratios?: string[];
  effective_durations?: string[];
  effective_resolutions?: string[];
  effective_max_images?: number;
  note?: string;
  credential?: CredentialRow;
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
  note?: string;
}

interface ClientModelRow {
  id: string;
  model_key: string;
  display_name: string;
  provider: string;
  scope: string;
  enabled: boolean;
  sort_order: number;
  supported_modes: string[];
  supported_scopes: string[];
  aspect_ratios: string[];
  durations: string[];
  note?: string;
  routes: ClientModelRouteRow[];
  created_at: string;
}

const ClientModels: React.FC = () => {
  const { modal } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<ClientModelRow[]>([]);
  const [credentials, setCredentials] = useState<CredentialRow[]>([]);
  const [modelModalVisible, setModelModalVisible] = useState(false);
  const [routeModalVisible, setRouteModalVisible] = useState(false);
  const [currentModel, setCurrentModel] = useState<ClientModelRow | null>(null);
  const [currentRoute, setCurrentRoute] = useState<ClientModelRouteRow | null>(null);
  const [routeModel, setRouteModel] = useState<ClientModelRow | null>(null);
  const [upstreamCapabilities, setUpstreamCapabilities] = useState<UpstreamCapabilityRow[]>([]);
  const [modelForm] = Form.useForm();
  const [routeForm] = Form.useForm();
  const selectedScope = Form.useWatch('scope', modelForm);
  const selectedRouteCredentialId = Form.useWatch('credential_id', routeForm);
  const selectedUpstreamModel = Form.useWatch('upstream_model', routeForm);

  const fetchCredentials = useCallback(async () => {
    const result: any = await providerCredentialApi.list({ page: 1, page_size: 200, enabled: true });
    setCredentials(result?.list || []);
  }, []);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const result: any = await clientModelApi.list({ include_disabled: true, page: 1, page_size: 200 });
      setData(result?.list || []);
    } catch (error) {
      console.error(error);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    fetchCredentials().catch(console.error);
  }, [fetchData, fetchCredentials]);

  useEffect(() => {
    if (!selectedScope || currentModel) {
      return;
    }
    modelForm.setFieldsValue({
      supported_scopes: [selectedScope],
      supported_modes: DEFAULT_MODES_BY_SCOPE[selectedScope] ?? [],
    });
  }, [selectedScope, currentModel, modelForm]);

  const credentialOptions = useMemo(() => {
    const provider = routeModel?.provider;
    return credentials
      .filter(item => !provider || item.provider === provider)
      .map(item => ({
        value: item.id,
        label: `${item.channel_name} / ${item.mode}${item.default_model ? ` / ${item.default_model}` : ''}`,
      }));
  }, [credentials, routeModel]);

  const selectedRouteCredential = useMemo(
    () => credentials.find(item => item.id === selectedRouteCredentialId),
    [credentials, selectedRouteCredentialId]
  );

  const upstreamModelOptions = useMemo(() => upstreamCapabilities.map(item => ({
    value: item.model,
    label: `${item.display_name || item.model}${item.model ? ` / ${item.model}` : ''}`,
  })), [upstreamCapabilities]);

  const summarizeRouteCapabilities = (record: ClientModelRow) => {
    const enabledRoutes = (record.routes || []).filter(route => route.enabled && route.credential?.enabled !== false);
    const aspectRatios = uniqueRouteValues(enabledRoutes, route => route.effective_aspect_ratios || route.aspect_ratios || []);
    const durations = uniqueRouteValues(enabledRoutes, route => route.effective_durations || route.durations || []);
    const resolutions = uniqueRouteValues(enabledRoutes, route => route.effective_resolutions || route.resolutions || []);
    const maxImages = Math.max(0, ...enabledRoutes.map(route => route.effective_max_images || route.max_images || 0));
    return { aspectRatios, durations, resolutions, maxImages };
  };

  useEffect(() => {
    if (!selectedRouteCredential) {
      setUpstreamCapabilities([]);
      return;
    }
    clientModelApi.upstreamCapabilities({
      provider: selectedRouteCredential.provider,
      mode: selectedRouteCredential.mode,
    }).then((result: any) => {
      setUpstreamCapabilities(result || []);
    }).catch((error) => {
      console.error(error);
      setUpstreamCapabilities([]);
    });
  }, [selectedRouteCredential]);

  useEffect(() => {
    if (!selectedUpstreamModel || currentRoute) {
      return;
    }
    const capability = upstreamCapabilities.find(item => item.model === selectedUpstreamModel);
    if (!capability) {
      return;
    }
    routeForm.setFieldsValue({
      aspect_ratios: capability.aspect_ratios || [],
      durations: capability.durations || [],
      resolutions: capability.resolutions || [],
      max_images: capability.max_images || 0,
    });
  }, [selectedUpstreamModel, upstreamCapabilities, currentRoute, routeForm]);

  const handleCreateModel = () => {
    setCurrentModel(null);
    modelForm.resetFields();
    modelForm.setFieldsValue({
      enabled: true,
      sort_order: 0,
      scope: 'video',
      supported_scopes: ['video'],
      supported_modes: DEFAULT_MODES_BY_SCOPE.video,
    });
    setModelModalVisible(true);
  };

  const handleEditModel = (record: ClientModelRow) => {
    setCurrentModel(record);
    modelForm.setFieldsValue({
      model_key: record.model_key,
      display_name: record.display_name,
      provider: record.provider,
      scope: record.scope,
      enabled: record.enabled,
      sort_order: record.sort_order,
      supported_modes: record.supported_modes || [],
      supported_scopes: record.supported_scopes || [record.scope],
      note: record.note,
    });
    setModelModalVisible(true);
  };

  const handleSubmitModel = async () => {
    try {
      const values = await modelForm.validateFields();
      if (currentModel) {
        await clientModelApi.update(currentModel.id, values);
        message.success('更新成功');
      } else {
        await clientModelApi.create(values);
        message.success('创建成功');
      }
      setModelModalVisible(false);
      fetchData();
    } catch {
      // antd 已展示校验错误
    }
  };

  const handleDeleteModel = (record: ClientModelRow) => {
    modal.confirm({
      title: '确认删除',
      content: `确定删除客户端模型 "${record.display_name}" 吗？`,
      okType: 'danger',
      onOk: async () => {
        await clientModelApi.delete(record.id);
        message.success('删除成功');
        fetchData();
      },
    });
  };

  const handleCreateRoute = (record: ClientModelRow) => {
    setRouteModel(record);
    setCurrentRoute(null);
    routeForm.resetFields();
    routeForm.setFieldsValue({
      enabled: true,
      is_default: record.routes.length === 0,
      priority: 0,
      sort_order: 0,
      aspect_ratios: [],
      durations: [],
      resolutions: [],
      max_images: 0,
    });
    setRouteModalVisible(true);
  };

  const handleEditRoute = (modelRow: ClientModelRow, route: ClientModelRouteRow) => {
    setRouteModel(modelRow);
    setCurrentRoute(route);
    routeForm.setFieldsValue({
      credential_id: route.credential_id,
      upstream_model: route.upstream_model,
      enabled: route.enabled,
      is_default: route.is_default,
      priority: route.priority,
      sort_order: route.sort_order,
      aspect_ratios: route.aspect_ratios || route.effective_aspect_ratios || [],
      durations: route.durations || route.effective_durations || [],
      resolutions: route.resolutions || route.effective_resolutions || [],
      max_images: route.max_images || route.effective_max_images || 0,
      note: route.note,
    });
    setRouteModalVisible(true);
  };

  const handleSubmitRoute = async () => {
    if (!routeModel) {
      return;
    }
    try {
      const values = await routeForm.validateFields();
      if (currentRoute) {
        await clientModelApi.updateRoute(currentRoute.id, values);
        message.success('路由已更新');
      } else {
        await clientModelApi.createRoute(routeModel.id, values);
        message.success('路由已添加');
      }
      setRouteModalVisible(false);
      fetchData();
    } catch {
      // antd 已展示校验错误
    }
  };

  const handleDeleteRoute = (route: ClientModelRouteRow) => {
    modal.confirm({
      title: '确认删除',
      content: `确定删除路由 "${route.upstream_model}" 吗？`,
      okType: 'danger',
      onOk: async () => {
        await clientModelApi.deleteRoute(route.id);
        message.success('删除成功');
        fetchData();
      },
    });
  };

  const routeTable = (record: ClientModelRow) => (
    <div style={{ padding: '8px 16px 12px' }}>
      <div style={{ marginBottom: 12, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <strong>真实渠道路由</strong>
        <Button size="small" icon={<PlusOutlined />} onClick={() => handleCreateRoute(record)}>
          添加路由
        </Button>
      </div>
      <Table
        size="small"
        rowKey="id"
        pagination={false}
        dataSource={record.routes || []}
        columns={[
          {
            title: '真实渠道',
            key: 'credential',
            render: (_: any, route: ClientModelRouteRow) => route.credential
              ? `${route.credential.channel_name} / ${route.credential.mode}`
              : route.credential_id,
          },
          {
            title: '上游模型',
            dataIndex: 'upstream_model',
            key: 'upstream_model',
          },
          {
            title: '能力',
            key: 'capability',
            render: (_: any, route: ClientModelRouteRow) => (
              <Space wrap>
                {(route.effective_aspect_ratios || []).slice(0, 3).map(value => <Tag key={`a-${value}`}>{value}</Tag>)}
                {(route.effective_durations || []).slice(0, 3).map(value => <Tag key={`d-${value}`}>{value}s</Tag>)}
                {route.effective_max_images ? <Tag>图 {route.effective_max_images}</Tag> : null}
              </Space>
            ),
          },
          {
            title: '默认',
            dataIndex: 'is_default',
            key: 'is_default',
            width: 70,
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
            render: (v: boolean) => <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '禁用'}</Tag>,
          },
          {
            title: '操作',
            key: 'action',
            width: 150,
            render: (_: any, route: ClientModelRouteRow) => (
              <Space>
                <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEditRoute(record, route)}>
                  编辑
                </Button>
                <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleDeleteRoute(route)}>
                  删除
                </Button>
              </Space>
            ),
          },
        ]}
      />
    </div>
  );

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>客户端模型</h2>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={handleCreateModel}>新建模型</Button>
        </Space>
      </div>

      <Table
        rowKey="id"
        loading={loading}
        dataSource={data}
        expandable={{ expandedRowRender: routeTable }}
        columns={[
          {
            title: '客户端显示',
            dataIndex: 'display_name',
            key: 'display_name',
            render: (v: string, record: ClientModelRow) => (
              <Space direction="vertical" size={0}>
                <span>{v}</span>
                <code style={{ fontSize: 12 }}>{record.model_key}</code>
              </Space>
            ),
          },
          {
            title: 'Provider',
            dataIndex: 'provider',
            key: 'provider',
            width: 100,
            render: (v: string) => <Tag color="blue">{v}</Tag>,
          },
          {
            title: '能力',
            dataIndex: 'scope',
            key: 'scope',
            width: 90,
            render: (v: string) => SCOPE_OPTIONS.find(item => item.value === v)?.label || v,
          },
          {
            title: '模式',
            dataIndex: 'supported_modes',
            key: 'supported_modes',
            render: (values: string[]) => (
              <Space wrap>
                {(values || []).map(value => <Tag key={value}>{MODE_OPTIONS.find(item => item.value === value)?.label || value}</Tag>)}
              </Space>
            ),
          },
          {
            title: '路由能力',
            key: 'route_capability',
            render: (_: any, record: ClientModelRow) => {
              const capability = summarizeRouteCapabilities(record);
              const empty = capability.aspectRatios.length === 0
                && capability.durations.length === 0
                && capability.resolutions.length === 0
                && capability.maxImages === 0;
              if (empty) {
                return <Tag color="default">添加路由后自动生成</Tag>;
              }
              return (
                <Space wrap>
                  {capability.aspectRatios.slice(0, 3).map(value => <Tag key={`a-${value}`}>{value}</Tag>)}
                  {capability.durations.slice(0, 3).map(value => <Tag key={`d-${value}`}>{value}s</Tag>)}
                  {capability.resolutions.slice(0, 2).map(value => <Tag key={`r-${value}`}>{value}</Tag>)}
                  {capability.maxImages > 0 ? <Tag>图 {capability.maxImages}</Tag> : null}
                </Space>
              );
            },
          },
          {
            title: '路由数',
            key: 'routes',
            width: 80,
            render: (_: any, record: ClientModelRow) => record.routes?.length || 0,
          },
          {
            title: '排序',
            dataIndex: 'sort_order',
            key: 'sort_order',
            width: 80,
          },
          {
            title: '启用',
            dataIndex: 'enabled',
            key: 'enabled',
            width: 70,
            render: (v: boolean) => <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '禁用'}</Tag>,
          },
          {
            title: '操作',
            key: 'action',
            width: 230,
            render: (_: any, record: ClientModelRow) => (
              <Space>
                <Button type="link" size="small" icon={<PlusOutlined />} onClick={() => handleCreateRoute(record)}>
                  路由
                </Button>
                <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEditModel(record)}>
                  编辑
                </Button>
                <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleDeleteModel(record)}>
                  删除
                </Button>
              </Space>
            ),
          },
        ]}
        pagination={{ pageSize: 20, showTotal: t => `共 ${t} 条` }}
      />

      <Modal
        title={currentModel ? '编辑客户端模型' : '新建客户端模型'}
        open={modelModalVisible}
        onOk={handleSubmitModel}
        onCancel={() => setModelModalVisible(false)}
        width={720}
        destroyOnClose
      >
        <Form form={modelForm} layout="vertical" preserve={false}>
          <Form.Item name="display_name" label="客户端显示名称" rules={[{ required: true, message: '请输入显示名称' }, { max: 120 }]}>
            <Input placeholder="例如：Veo 3.1" />
          </Form.Item>
          <Form.Item name="model_key" label="客户端模型标识" rules={[{ required: true, message: '请输入模型标识' }, { max: 80 }]}>
            <Input placeholder="例如：veo-3.1" disabled={!!currentModel} />
          </Form.Item>
          <Space size="large" align="start">
            <Form.Item name="provider" label="Provider" rules={[{ required: true, message: '请选择 Provider' }]}>
              <Select style={{ width: 180 }} options={PROVIDER_OPTIONS} disabled={!!currentModel} />
            </Form.Item>
            <Form.Item name="scope" label="能力类型" rules={[{ required: true, message: '请选择能力类型' }]}>
              <Select style={{ width: 160 }} options={SCOPE_OPTIONS} disabled={!!currentModel} />
            </Form.Item>
            <Form.Item name="sort_order" label="排序">
              <InputNumber min={0} max={10000} />
            </Form.Item>
          </Space>
          <Form.Item name="supported_modes" label="支持模式">
            <Select mode="tags" options={MODE_OPTIONS} placeholder="选择或输入模式" />
          </Form.Item>
          <Form.Item name="supported_scopes" label="支持能力">
            <Select mode="tags" options={SCOPE_OPTIONS} placeholder="选择或输入能力类型" />
          </Form.Item>
          <Alert
            showIcon
            type="info"
            style={{ marginBottom: 16 }}
            message="比例、时长、分辨率由真实渠道路由自动汇总"
          />
          <Space size="large">
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={256} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={currentRoute ? '编辑路由' : '添加路由'}
        open={routeModalVisible}
        onOk={handleSubmitRoute}
        onCancel={() => setRouteModalVisible(false)}
        width={640}
        destroyOnClose
      >
        <Form form={routeForm} layout="vertical" preserve={false}>
          <Form.Item name="credential_id" label="真实渠道" rules={[{ required: true, message: '请选择真实渠道' }]}>
            <Select
              showSearch
              options={credentialOptions}
              optionFilterProp="label"
              placeholder="选择 Provider 凭证"
            />
          </Form.Item>
          <Form.Item name="upstream_model" label="上游真实模型" rules={[{ required: true, message: '请输入上游模型' }, { max: 120 }]}>
            <AutoComplete
              options={upstreamModelOptions}
              filterOption={(input, option) =>
                String(option?.label ?? '').toLowerCase().includes(input.toLowerCase())
                || String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
              }
              placeholder="选择或输入上游模型"
            />
          </Form.Item>
          <Form.Item name="aspect_ratios" label="比例选项">
            <Select mode="tags" placeholder="例如 16:9、9:16、1:1" />
          </Form.Item>
          <Form.Item name="durations" label="时长选项">
            <Select mode="tags" placeholder="例如 5、8、12" />
          </Form.Item>
          <Form.Item name="resolutions" label="分辨率选项">
            <Select mode="tags" placeholder="例如 720p、1080p、4K" />
          </Form.Item>
          <Form.Item name="max_images" label="最大参考图数量">
            <InputNumber min={0} max={20} />
          </Form.Item>
          <Space size="large">
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="is_default" label="默认" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="priority" label="优先级">
              <InputNumber min={0} max={10000} />
            </Form.Item>
            <Form.Item name="sort_order" label="排序">
              <InputNumber min={0} max={10000} />
            </Form.Item>
          </Space>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={256} />
          </Form.Item>
          {routeModel && (
            <Tooltip title="客户端只看到模型名称；真实渠道和上游模型不会下发给客户端">
              <Tag color="blue">{routeModel.display_name}</Tag>
            </Tooltip>
          )}
        </Form>
      </Modal>
    </div>
  );
};

function uniqueRouteValues(
  routes: ClientModelRouteRow[],
  selectValues: (route: ClientModelRouteRow) => string[]
) {
  const out: string[] = [];
  const seen = new Set<string>();
  routes.forEach(route => {
    selectValues(route).forEach(value => {
      const normalized = String(value || '').trim();
      const key = normalized.toLowerCase();
      if (!key || seen.has(key)) {
        return;
      }
      seen.add(key);
      out.push(normalized);
    });
  });
  return out;
}

export default ClientModels;
