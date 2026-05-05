import React, { useCallback, useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Tabs, Button, Descriptions, Tag, Input, message, Spin, Card, Table, Space, Modal, Form, InputNumber, Upload, Select, Progress, App, Checkbox } from 'antd';
import { ArrowLeftOutlined, CopyOutlined, KeyOutlined, PlusOutlined, UploadOutlined, EditOutlined, CodeOutlined, RollbackOutlined, SendOutlined } from '@ant-design/icons';
import { appApi, hotUpdateApi, secureScriptApi, instructionApi, publishTaskApi } from '../api';
import { useAuthStore } from '../store';
import dayjs from 'dayjs';

const { Option } = Select;
const { TextArea } = Input;

const AppDetail: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const role = user?.role || '';
  const canUpdateApp = ['owner', 'admin', 'developer'].includes(role);
  const canDeleteAppResource = ['owner', 'admin'].includes(role);
  const [loading, setLoading] = useState(true);
  const [app, setApp] = useState<any>(null);
  const [activeTab, setActiveTab] = useState('info');

  // 热更新相关
  const [versions, setVersions] = useState<any[]>([]);
  const [versionLoading, setVersionLoading] = useState(false);
  const [versionModalVisible, setVersionModalVisible] = useState(false);
  const [versionForm] = Form.useForm();
  const [versionFileList, setVersionFileList] = useState<any[]>([]);
  const [creatingVersion, setCreatingVersion] = useState(false);
  const [versionUploadPercent, setVersionUploadPercent] = useState(0);
  const [versionActionLoading, setVersionActionLoading] = useState<Record<string, boolean>>({});
  const [versionStatusFilter, setVersionStatusFilter] = useState<string>('all');
  const [versionDetailVisible, setVersionDetailVisible] = useState(false);
  const [versionDetailLoading, setVersionDetailLoading] = useState(false);
  const [savingVersionDetail, setSavingVersionDetail] = useState(false);
  const [currentVersionDetail, setCurrentVersionDetail] = useState<any>(null);
  const [versionLogs, setVersionLogs] = useState<any[]>([]);
  const [versionLogsLoading, setVersionLogsLoading] = useState(false);
  const [versionLogStatusFilter, setVersionLogStatusFilter] = useState<string>('all');
  const [versionLogOnlyErrors, setVersionLogOnlyErrors] = useState(false);
  const [versionDetailForm] = Form.useForm();

  // 安全脚本相关
  const [scripts, setScripts] = useState<any[]>([]);
  const [scriptLoading, setScriptLoading] = useState(false);
  const [scriptModalVisible, setScriptModalVisible] = useState(false);
  const [scriptForm] = Form.useForm();
  const [currentScript, setCurrentScript] = useState<any>(null);
  const [contentModalVisible, setContentModalVisible] = useState(false);
  const [contentForm] = Form.useForm();

  // 实时指令相关
  const [onlineDevices, setOnlineDevices] = useState<any[]>([]);
  const [instructionModalVisible, setInstructionModalVisible] = useState(false);
  const [instructionForm] = Form.useForm();

  const parseStringList = (value: any): string[] => {
    if (Array.isArray(value)) return value.map(String).map(v => v.trim()).filter(Boolean);
    if (!value) return [];
    const text = String(value).trim();
    if (!text) return [];
    try {
      const parsed = JSON.parse(text);
      if (Array.isArray(parsed)) return parsed.map(String).map(v => v.trim()).filter(Boolean);
    } catch {
      // 支持逗号分隔输入
    }
    return text.split(',').map(v => v.trim()).filter(Boolean);
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

  const fetchApp = useCallback(async () => {
    setLoading(true);
    try {
      const result = await appApi.get(id!);
      setApp(result);
    } catch {
      message.error('获取应用信息失败');
      navigate('/apps');
    } finally {
      setLoading(false);
    }
  }, [id, navigate]);

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    message.success('已复制到剪贴板');
  };

  const handleRegenerateKeys = async () => {
    if (!canUpdateApp) {
      return;
    }

    modal.confirm({
      title: '重新生成密钥',
      content: '重新生成密钥后，旧密钥将失效，确定继续吗？',
      onOk: async () => {
        try {
          const result: any = await appApi.regenerateKeys(id!);
          message.success('密钥已重新生成');
          setApp({ ...app, ...result });
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  // ==================== 热更新管理 ====================
  const fetchVersions = useCallback(async () => {
    setVersionLoading(true);
    try {
      const params = versionStatusFilter !== 'all' ? { status: versionStatusFilter } : undefined;
      const result: any = await hotUpdateApi.list(id!, params);
      setVersions(result?.list ?? (Array.isArray(result) ? result : []));
    } catch (error) {
      console.error(error);
    } finally {
      setVersionLoading(false);
    }
  }, [id, versionStatusFilter]);

  const handleCreateVersion = async () => {
    if (!canUpdateApp) {
      return;
    }

    if (creatingVersion) {
      return;
    }

    try {
      const values = await versionForm.validateFields();
      if (versionFileList.length === 0) {
        message.error('请选择更新包文件');
        return;
      }
      setCreatingVersion(true);
      setVersionUploadPercent(0);

      const formData = new FormData();
      formData.append('file', versionFileList[0].originFileObj);
      formData.append('version', values.version);
      formData.append('update_type', values.update_type || 'full');
      formData.append('update_mode', 'mixed');
      formData.append('changelog', values.changelog || '');
      formData.append('rollout_percentage', String(values.rollout_percentage ?? 100));
      formData.append('force_update', 'false');
      if (values.min_app_version) {
        formData.append('min_app_version', values.min_app_version);
      }

      await hotUpdateApi.create(id!, formData, (percent) => {
        setVersionUploadPercent(percent);
      });
      setVersionUploadPercent(100);
      message.success('创建成功');
      setVersionModalVisible(false);
      versionForm.resetFields();
      setVersionFileList([]);
      fetchVersions();
    } catch (error) {
      console.error(error);
    } finally {
      setCreatingVersion(false);
    }
  };

  const handlePublishVersion = async (versionId: string) => {
    if (!canUpdateApp) {
      return;
    }

    const loadingKey = `${versionId}:publish`;
    if (versionActionLoading[loadingKey]) {
      return;
    }
    try {
      setVersionActionLoading((prev) => ({ ...prev, [loadingKey]: true }));
      const task: any = await publishTaskApi.createHotUpdateTask(versionId, 'publish');
      await waitPublishTask(task?.task_id);
      message.success('发布成功');
      fetchVersions();
    } catch (error) {
      console.error(error);
      message.error((error as any)?.message || '发布失败');
    } finally {
      setVersionActionLoading((prev) => ({ ...prev, [loadingKey]: false }));
    }
  };

  const handleDeprecateVersion = async (versionId: string) => {
    if (!canUpdateApp) {
      return;
    }

    modal.confirm({
      title: '确认废弃',
      content: '废弃后该版本将不再提供下载，确定吗？',
      onOk: async () => {
        const loadingKey = `${versionId}:deprecate`;
        if (versionActionLoading[loadingKey]) {
          return;
        }
        try {
          setVersionActionLoading((prev) => ({ ...prev, [loadingKey]: true }));
          const task: any = await publishTaskApi.createHotUpdateTask(versionId, 'deprecate');
          await waitPublishTask(task?.task_id);
          message.success('已废弃');
          fetchVersions();
        } catch (error) {
          console.error(error);
          message.error((error as any)?.message || '废弃失败');
        } finally {
          setVersionActionLoading((prev) => ({ ...prev, [loadingKey]: false }));
        }
      },
    });
  };

  const handleRollbackVersion = async (versionId: string) => {
    if (!canUpdateApp) {
      return;
    }

    modal.confirm({
      title: '确认回滚',
      content: '回滚后该版本将停止推送，确定吗？',
      onOk: async () => {
        const loadingKey = `${versionId}:rollback`;
        if (versionActionLoading[loadingKey]) {
          return;
        }
        try {
          setVersionActionLoading((prev) => ({ ...prev, [loadingKey]: true }));
          const task: any = await publishTaskApi.createHotUpdateTask(versionId, 'rollback');
          await waitPublishTask(task?.task_id);
          message.success('回滚成功');
          fetchVersions();
        } catch (error) {
          console.error(error);
          message.error((error as any)?.message || '回滚失败');
        } finally {
          setVersionActionLoading((prev) => ({ ...prev, [loadingKey]: false }));
        }
      },
    });
  };

  const handleDeleteVersion = async (versionId: string) => {
    if (!canDeleteAppResource) {
      return;
    }

    modal.confirm({
      title: '确认删除',
      content: '删除后无法恢复，确定吗？',
      onOk: async () => {
        try {
          await hotUpdateApi.delete(versionId);
          message.success('删除成功');
          fetchVersions();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const fetchVersionLogs = async (
    versionId: string,
    status: string = versionLogStatusFilter,
    onlyErrors: boolean = versionLogOnlyErrors,
  ) => {
    setVersionLogsLoading(true);
    try {
      const params: Record<string, string | number | boolean> = { page_size: 50 };
      if (status && status !== 'all') {
        params.status = status;
      }
      if (onlyErrors) {
        params.has_error = true;
      }
      const logsResult: any = await hotUpdateApi.getLogs(versionId, params);
      setVersionLogs(logsResult?.list ?? (Array.isArray(logsResult) ? logsResult : []));
    } catch (error) {
      console.error(error);
      message.warning('热更新日志加载失败');
      setVersionLogs([]);
    } finally {
      setVersionLogsLoading(false);
    }
  };

  const handleViewVersion = async (versionId: string) => {
    setVersionDetailVisible(true);
    setVersionDetailLoading(true);
    setVersionLogs([]);
    setVersionLogStatusFilter('all');
    setVersionLogOnlyErrors(false);
    try {
      const detail: any = await hotUpdateApi.get(versionId);
      setCurrentVersionDetail(detail);
      versionDetailForm.setFieldsValue({
        changelog: detail?.changelog || '',
        rollout_percentage: detail?.rollout_percentage ?? 100,
        min_app_version: detail?.min_app_version || '',
      });

      await fetchVersionLogs(versionId, 'all', false);
    } catch (error) {
      console.error(error);
      message.error('获取热更新详情失败');
      setVersionDetailVisible(false);
    } finally {
      setVersionDetailLoading(false);
    }
  };

  const handleVersionLogStatusChange = async (status: string) => {
    setVersionLogStatusFilter(status);
    if (!currentVersionDetail?.id) {
      return;
    }
    await fetchVersionLogs(currentVersionDetail.id, status, versionLogOnlyErrors);
  };

  const handleVersionLogOnlyErrorsChange = async (event: any) => {
    const checked = event.target.checked;
    setVersionLogOnlyErrors(checked);
    if (!currentVersionDetail?.id) {
      return;
    }
    await fetchVersionLogs(currentVersionDetail.id, versionLogStatusFilter, checked);
  };

  const handleSaveVersionDetail = async () => {
    if (!canUpdateApp) {
      return;
    }

    if (!currentVersionDetail?.id || savingVersionDetail) {
      return;
    }

    try {
      const values = await versionDetailForm.validateFields();
      setSavingVersionDetail(true);

      await hotUpdateApi.update(currentVersionDetail.id, {
        changelog: values.changelog || '',
        rollout_percentage: values.rollout_percentage ?? 100,
        min_app_version: values.min_app_version || '',
      });

      const detail: any = await hotUpdateApi.get(currentVersionDetail.id);
      setCurrentVersionDetail(detail);
      versionDetailForm.setFieldsValue({
        changelog: detail?.changelog || '',
        rollout_percentage: detail?.rollout_percentage ?? 100,
        min_app_version: detail?.min_app_version || '',
      });
      message.success('保存成功');
      fetchVersions();
    } catch (error) {
      console.error(error);
    } finally {
      setSavingVersionDetail(false);
    }
  };

  const waitPublishTask = async (taskId: string, timeoutMs = 120000): Promise<any> => {
    if (!taskId) {
      throw new Error('任务创建失败');
    }

    const start = Date.now();
    while (Date.now() - start < timeoutMs) {
      const task: any = await publishTaskApi.get(taskId);
      if (task.status === 'success') {
        return task;
      }
      if (task.status === 'failed') {
        throw new Error(task.error_message || '任务执行失败');
      }
      await new Promise((resolve) => setTimeout(resolve, 1200));
    }
    throw new Error('任务执行超时，请在审计日志中查看结果');
  };

  const isVersionActionBusy = (versionId: string) =>
    Object.keys(versionActionLoading).some((key) => key.startsWith(`${versionId}:`) && versionActionLoading[key]);

  // ==================== 安全脚本 ====================
  const fetchScripts = useCallback(async () => {
    setScriptLoading(true);
    try {
      const result: any = await secureScriptApi.list(id!);
      setScripts(result?.list ?? (Array.isArray(result) ? result : []));
    } catch (error) {
      console.error(error);
    } finally {
      setScriptLoading(false);
    }
  }, [id]);

  const handleCreateScript = async () => {
    if (!canUpdateApp) {
      return;
    }

    try {
      const values = await scriptForm.validateFields();
      if (currentScript) {
        await secureScriptApi.update(currentScript.id, buildScriptUpdatePayload(values));
        message.success('更新成功');
      } else {
        await secureScriptApi.create(id!, buildScriptFormData(values));
        message.success('创建成功');
      }
      setScriptModalVisible(false);
      scriptForm.resetFields();
      setCurrentScript(null);
      fetchScripts();
    } catch (error) {
      console.error(error);
    }
  };

  const handleEditScriptContent = async () => {
    if (!canUpdateApp) {
      return;
    }

    try {
      const values = await contentForm.validateFields();
      const formData = new FormData();
      formData.append('file', new File([values.content || ''], getScriptFileName(currentScript?.script_type), { type: 'text/plain' }));
      formData.append('version', values.version);
      await secureScriptApi.updateContent(currentScript.id, formData);
      message.success('脚本内容已更新');
      setContentModalVisible(false);
      fetchScripts();
    } catch (error) {
      console.error(error);
    }
  };

  const handlePublishScript = async (scriptId: string) => {
    if (!canUpdateApp) {
      return;
    }

    try {
      await secureScriptApi.publish(scriptId);
      message.success('发布成功');
      fetchScripts();
    } catch (error) {
      console.error(error);
    }
  };

  // ==================== 实时指令 ====================
  const fetchOnlineDevices = useCallback(async () => {
    try {
      const result: any = await instructionApi.getOnlineDevices(id!);
      setOnlineDevices(result?.devices ?? result?.list ?? (Array.isArray(result) ? result : []));
    } catch (error) {
      console.error(error);
    }
  }, [id]);

  useEffect(() => {
    if (id) {
      fetchApp();
    }
  }, [fetchApp, id]);

  useEffect(() => {
    if (id && app) {
      if (activeTab === 'versions') fetchVersions();
      if (activeTab === 'scripts') fetchScripts();
      if (activeTab === 'instructions') fetchOnlineDevices();
    }
  }, [activeTab, app, fetchOnlineDevices, fetchScripts, fetchVersions, id]);

  const handleSendInstruction = async () => {
    if (!canUpdateApp) {
      return;
    }

    try {
      const values = await instructionForm.validateFields();
      const result: any = await instructionApi.send({
        app_id: id,
        ...values,
        payload: values.payload || '{}',
      });
      const sentCount = Number(result?.sent_count ?? (result?.sent ? 1 : 0));
      if (sentCount > 0) {
        message.success(`指令已发送到 ${sentCount} 台设备`);
      } else {
        message.warning('当前没有在线设备，指令已创建但未发送');
      }
      setInstructionModalVisible(false);
      instructionForm.resetFields();
      fetchOnlineDevices();
    } catch (error) {
      console.error(error);
    }
  };

  // ==================== 渲染 ====================
  const getStatusTag = (status: string) => {
    const map: Record<string, { color: string; text: string }> = {
      draft: { color: 'default', text: '草稿' },
      published: { color: 'green', text: '已发布' },
      deprecated: { color: 'orange', text: '已废弃' },
      rollback: { color: 'red', text: '已回滚' },
      active: { color: 'green', text: '启用' },
      disabled: { color: 'red', text: '禁用' },
    };
    const s = map[status] || { color: 'default', text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const getUpdateTypeTag = (type: string) => {
    const map: Record<string, { color: string; text: string }> = {
      full: { color: 'blue', text: '全量更新' },
      patch: { color: 'cyan', text: '增量更新' },
    };
    const t = map[type] || { color: 'default', text: type };
    return <Tag color={t.color}>{t.text}</Tag>;
  };

  const getUpdateLogStatusTag = (status: string) => {
    const map: Record<string, { color: string; text: string }> = {
      pending: { color: 'default', text: '待处理' },
      downloading: { color: 'processing', text: '下载中' },
      installing: { color: 'blue', text: '安装中' },
      success: { color: 'green', text: '成功' },
      failed: { color: 'red', text: '失败' },
      rollback: { color: 'orange', text: '已回滚' },
    };
    const current = map[status] || { color: 'default', text: status };
    return <Tag color={current.color}>{current.text}</Tag>;
  };

  if (loading) {
    return <div style={{ textAlign: 'center', padding: 50 }}><Spin size="large" /></div>;
  }

  if (!app) {
    return null;
  }

  const tabItems = [
    {
      key: 'info',
      label: '基本信息',
      children: (
        <Card>
          <Descriptions column={2} bordered size="small">
            <Descriptions.Item label="应用名称">{app.name}</Descriptions.Item>
            <Descriptions.Item label="状态">{getStatusTag(app.status)}</Descriptions.Item>
            <Descriptions.Item label="App Key" span={2}>
              <code>{app.app_key}</code>
              <Button type="link" size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(app.app_key)} />
            </Descriptions.Item>
            {app.app_secret && (
              <Descriptions.Item label="App Secret" span={2}>
                <code>{app.app_secret.slice(0, 20)}...</code>
                <Button type="link" size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(app.app_secret)} />
              </Descriptions.Item>
            )}
            <Descriptions.Item label="默认设备数">{app.max_devices_default}</Descriptions.Item>
            <Descriptions.Item label="心跳间隔">{app.heartbeat_interval}秒</Descriptions.Item>
            <Descriptions.Item label="离线容忍">{app.offline_tolerance}秒</Descriptions.Item>
            <Descriptions.Item label="宽限期">{app.grace_period_days}天</Descriptions.Item>
            <Descriptions.Item label="描述" span={2}>{app.description || '-'}</Descriptions.Item>
            <Descriptions.Item label="公钥" span={2}>
              <TextArea value={app.public_key} rows={3} readOnly style={{ marginBottom: 8 }} />
              <Button size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(app.public_key)}>复制公钥</Button>
            </Descriptions.Item>
            <Descriptions.Item label="创建时间">{dayjs(app.created_at).format('YYYY-MM-DD HH:mm')}</Descriptions.Item>
            <Descriptions.Item label="更新时间">{app.updated_at ? dayjs(app.updated_at).format('YYYY-MM-DD HH:mm') : '-'}</Descriptions.Item>
          </Descriptions>
          {canUpdateApp && app.app_secret && (
            <div style={{ marginTop: 16 }}>
              <Button icon={<KeyOutlined />} onClick={handleRegenerateKeys}>重新生成密钥</Button>
            </div>
          )}
        </Card>
      ),
    },
    {
      key: 'versions',
      label: '热更新管理',
      children: (
        <Card
          title="热更新列表"
          extra={(
            <Space>
              <Select value={versionStatusFilter} style={{ width: 150 }} onChange={setVersionStatusFilter}>
                <Option value="all">全部状态</Option>
                <Option value="draft">草稿</Option>
                <Option value="published">已发布</Option>
                <Option value="deprecated">已废弃</Option>
                <Option value="rollback">已回滚</Option>
              </Select>
              {canUpdateApp && (
                <Button type="primary" icon={<PlusOutlined />} onClick={() => { versionForm.resetFields(); setVersionFileList([]); setVersionUploadPercent(0); setVersionModalVisible(true); }}>创建热更新</Button>
              )}
            </Space>
          )}
        >
          <Table
            loading={versionLoading}
            dataSource={versions}
            rowKey="id"
            columns={[
              { title: '版本', dataIndex: 'to_version', key: 'to_version', render: (v: string, record: any) => <strong>{v || record.version}</strong> },
              { title: '更新类型', dataIndex: 'patch_type', key: 'patch_type', render: (t: string) => getUpdateTypeTag(t) },
              { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => getStatusTag(s) },
              {
                title: '灰度', dataIndex: 'rollout_percentage', key: 'rollout_percentage',
                render: (v: number) => <Progress percent={v ?? 100} size="small" style={{ width: 80 }} />
              },
              {
                title: '统计', key: 'stats',
                render: (_: any, record: any) => (
                  <Space size="small">
                    <span title="下载">↓{record.download_count || 0}</span>
                    <span title="成功" style={{ color: 'green' }}>✓{record.success_count || 0}</span>
                    <span title="失败" style={{ color: 'red' }}>✗{record.fail_count || 0}</span>
                  </Space>
                )
              },
              { title: '发布时间', dataIndex: 'published_at', key: 'published_at', render: (v: string) => v ? dayjs(v).format('MM-DD HH:mm') : '-' },
              {
                title: '操作', key: 'action', width: 260,
                render: (_: any, record: any) => (
                  <Space>
                    <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleViewVersion(record.id)}>
                      {canUpdateApp ? '详情/编辑' : '详情'}
                    </Button>
                    {canUpdateApp && record.status === 'draft' && (
                      <>
                        <Button
                          type="link"
                          size="small"
                          loading={versionActionLoading[`${record.id}:publish`]}
                          disabled={isVersionActionBusy(record.id)}
                          onClick={() => handlePublishVersion(record.id)}
                        >
                          发布
                        </Button>
                        {canDeleteAppResource && (
                          <Button type="link" size="small" danger disabled={isVersionActionBusy(record.id)} onClick={() => handleDeleteVersion(record.id)}>删除</Button>
                        )}
                      </>
                    )}
                    {canUpdateApp && record.status === 'published' && (
                      <>
                        <Button
                          type="link"
                          size="small"
                          icon={<RollbackOutlined />}
                          loading={versionActionLoading[`${record.id}:rollback`]}
                          disabled={isVersionActionBusy(record.id)}
                          onClick={() => handleRollbackVersion(record.id)}
                        >
                          回滚
                        </Button>
                        <Button
                          type="link"
                          size="small"
                          danger
                          loading={versionActionLoading[`${record.id}:deprecate`]}
                          disabled={isVersionActionBusy(record.id)}
                          onClick={() => handleDeprecateVersion(record.id)}
                        >
                          废弃
                        </Button>
                      </>
                    )}
                    {canDeleteAppResource && (record.status === 'rollback' || record.status === 'deprecated') && (
                      <Button type="link" size="small" danger disabled={isVersionActionBusy(record.id)} onClick={() => handleDeleteVersion(record.id)}>删除</Button>
                    )}
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      ),
    },
    {
      key: 'scripts',
      label: '安全脚本',
      children: (
        <Card
          title="脚本列表"
          extra={canUpdateApp ? <Button type="primary" icon={<PlusOutlined />} onClick={() => { scriptForm.resetFields(); setCurrentScript(null); setScriptModalVisible(true); }}>创建脚本</Button> : null}
        >
          <Table
            loading={scriptLoading}
            dataSource={scripts}
            rowKey="id"
            columns={[
              { title: '脚本名称', dataIndex: 'name', key: 'name' },
              { title: '版本', dataIndex: 'version', key: 'version' },
              { title: '类型', dataIndex: 'script_type', key: 'script_type' },
              { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => getStatusTag(s) },
              { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm') },
              {
                title: '操作', key: 'action',
                render: (_: any, record: any) => canUpdateApp ? (
                  <Space>
                    <Button type="link" size="small" icon={<CodeOutlined />} onClick={() => { setCurrentScript(record); contentForm.setFieldsValue({ version: record.version || '1.0.0', content: '' }); setContentModalVisible(true); }}>编辑内容</Button>
                    <Button type="link" size="small" icon={<EditOutlined />} onClick={() => { setCurrentScript(record); scriptForm.setFieldsValue({ ...record, required_features: listToInputText(record.required_features), allowed_devices: listToInputText(record.allowed_devices), rollout_percentage: record.rollout_percentage ?? 100 }); setScriptModalVisible(true); }}>编辑</Button>
                    {record.status === 'draft' && <Button type="link" size="small" onClick={() => handlePublishScript(record.id)}>发布</Button>}
                  </Space>
                ) : '-',
              },
            ]}
          />
        </Card>
      ),
    },
    {
      key: 'instructions',
      label: '实时指令',
      children: (
        <Card
          title={`在线设备 (${onlineDevices.length})`}
          extra={canUpdateApp ? <Button type="primary" icon={<SendOutlined />} onClick={() => { instructionForm.resetFields(); setInstructionModalVisible(true); }}>发送指令</Button> : null}
        >
          <Table
            dataSource={onlineDevices}
            rowKey="device_id"
            columns={[
              { title: '设备ID', dataIndex: 'device_id', key: 'device_id', render: (v: string) => <code>{v?.slice(0, 8)}...</code> },
              { title: '机器码', dataIndex: 'machine_id', key: 'machine_id', render: (v: string) => <code>{v?.slice(0, 16)}...</code> },
              { title: '连接时间', dataIndex: 'connected_at', key: 'connected_at', render: (v: string) => v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '-' },
            ]}
          />
        </Card>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', gap: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/apps')}>返回</Button>
        <h2 style={{ margin: 0 }}>{app.name}</h2>
        {getStatusTag(app.status)}
      </div>

      <Tabs activeKey={activeTab} onChange={setActiveTab} items={tabItems} />

      {/* 热更新创建弹窗 */}
      <Modal
        title="创建热更新"
        open={versionModalVisible}
        onOk={handleCreateVersion}
        onCancel={() => setVersionModalVisible(false)}
        okText={creatingVersion ? '上传中...' : '确定'}
        okButtonProps={{ loading: creatingVersion }}
        cancelButtonProps={{ disabled: creatingVersion }}
        maskClosable={!creatingVersion}
        closable={!creatingVersion}
        keyboard={!creatingVersion}
        width={600}
      >
        <Form form={versionForm} layout="vertical">
          <Form.Item name="version" label="版本号" rules={[{ required: true, message: '请输入版本号' }]}>
            <Input placeholder="如: 1.0.1" />
          </Form.Item>
          <Form.Item name="update_type" label="更新类型" initialValue="full">
            <Select>
              <Option value="full">全量更新（完整安装包）</Option>
              <Option value="patch">增量更新（补丁包）</Option>
            </Select>
          </Form.Item>
          <Form.Item name="changelog" label="更新日志">
            <TextArea rows={3} placeholder="本次更新内容说明" />
          </Form.Item>
          <Form.Item name="rollout_percentage" label="灰度比例" initialValue={100}>
            <InputNumber min={0} max={100} addonAfter="%" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="min_app_version" label="最低支持版本">
            <Input placeholder="如: 1.0.0（可选）" />
          </Form.Item>
          <Form.Item label="更新包文件" required>
            <Upload
              fileList={versionFileList}
              beforeUpload={() => false}
              disabled={creatingVersion}
              onChange={({ fileList }) => setVersionFileList(fileList)}
              maxCount={1}
            >
              <Button icon={<UploadOutlined />} disabled={creatingVersion}>选择文件</Button>
            </Upload>
          </Form.Item>
          {(creatingVersion || versionUploadPercent > 0) && (
            <Form.Item label="上传进度">
              <Progress percent={versionUploadPercent} status={creatingVersion ? 'active' : 'success'} />
            </Form.Item>
          )}
        </Form>
      </Modal>

      {/* 热更新详情/编辑弹窗 */}
      <Modal
        title="热更新详情"
        open={versionDetailVisible}
        onOk={handleSaveVersionDetail}
        onCancel={() => {
          setVersionDetailVisible(false);
          setCurrentVersionDetail(null);
          setVersionLogs([]);
          setVersionLogStatusFilter('all');
          setVersionLogOnlyErrors(false);
          versionDetailForm.resetFields();
        }}
        okText={savingVersionDetail ? '保存中...' : '保存'}
        okButtonProps={{ loading: savingVersionDetail, disabled: versionDetailLoading || !currentVersionDetail }}
        footer={canUpdateApp ? undefined : null}
        width={900}
      >
        {versionDetailLoading ? (
          <div style={{ textAlign: 'center', padding: '32px 0' }}>
            <Spin size="large" />
          </div>
        ) : currentVersionDetail ? (
          <Tabs
            items={[
              {
                key: 'info',
                label: '基本信息',
                children: (
                  <>
                    <Descriptions column={2} bordered size="small" style={{ marginBottom: 16 }}>
                      <Descriptions.Item label="目标版本">{currentVersionDetail.to_version || '-'}</Descriptions.Item>
                      <Descriptions.Item label="更新类型">{getUpdateTypeTag(currentVersionDetail.patch_type)}</Descriptions.Item>
                      <Descriptions.Item label="状态">{getStatusTag(currentVersionDetail.status)}</Descriptions.Item>
                      <Descriptions.Item label="发布时间">{currentVersionDetail.published_at ? dayjs(currentVersionDetail.published_at).format('YYYY-MM-DD HH:mm') : '-'}</Descriptions.Item>
                      <Descriptions.Item label="下载次数">{currentVersionDetail.download_count || 0}</Descriptions.Item>
                      <Descriptions.Item label="更新结果">
                        成功 {currentVersionDetail.success_count || 0} / 失败 {currentVersionDetail.fail_count || 0}
                      </Descriptions.Item>
                    </Descriptions>

                    <Form form={versionDetailForm} layout="vertical" disabled={!canUpdateApp}>
                      <Form.Item name="changelog" label="更新日志">
                        <TextArea rows={4} placeholder="本次更新内容说明" />
                      </Form.Item>
                      <Form.Item name="rollout_percentage" label="灰度比例">
                        <InputNumber min={0} max={100} addonAfter="%" style={{ width: '100%' }} />
                      </Form.Item>
                      <Form.Item name="min_app_version" label="最低支持版本">
                        <Input placeholder="如: 1.0.0（可留空）" />
                      </Form.Item>
                    </Form>
                  </>
                ),
              },
              {
                key: 'logs',
                label: `更新日志 (${versionLogs.length})`,
                children: (
                  <>
                    <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12, marginBottom: 12 }}>
                      <Checkbox checked={versionLogOnlyErrors} onChange={handleVersionLogOnlyErrorsChange}>
                        仅显示有错误信息
                      </Checkbox>
                      <Select value={versionLogStatusFilter} style={{ width: 180 }} onChange={handleVersionLogStatusChange}>
                        <Option value="all">全部状态</Option>
                        <Option value="failed">仅看失败</Option>
                        <Option value="success">仅看成功</Option>
                        <Option value="downloading">下载中</Option>
                        <Option value="installing">安装中</Option>
                        <Option value="rollback">已回滚</Option>
                        <Option value="pending">待处理</Option>
                      </Select>
                    </div>
                    <Table
                      rowKey="id"
                      loading={versionLogsLoading}
                      dataSource={versionLogs}
                      pagination={{ pageSize: 10 }}
                      columns={[
                        { title: '设备ID', dataIndex: 'device_id', key: 'device_id', ellipsis: true },
                        { title: '机器码', dataIndex: 'machine_id', key: 'machine_id', ellipsis: true },
                        { title: '状态', dataIndex: 'status', key: 'status', render: (status: string) => getUpdateLogStatusTag(status) },
                        { title: '来源版本', dataIndex: 'from_version', key: 'from_version', render: (value: string) => value || '-' },
                        { title: '目标版本', dataIndex: 'to_version', key: 'to_version', render: (value: string) => value || '-' },
                        { title: '开始时间', dataIndex: 'started_at', key: 'started_at', render: (value: string) => value ? dayjs(value).format('MM-DD HH:mm') : '-' },
                        { title: '完成时间', dataIndex: 'completed_at', key: 'completed_at', render: (value: string) => value ? dayjs(value).format('MM-DD HH:mm') : '-' },
                        { title: '错误信息', dataIndex: 'error_message', key: 'error_message', ellipsis: true, render: (value: string) => value || '-' },
                      ]}
                    />
                  </>
                ),
              },
            ]}
          />
        ) : null}
      </Modal>

      {/* 安全脚本创建/编辑弹窗 */}
      <Modal
        title={currentScript ? '编辑脚本' : '创建脚本'}
        open={scriptModalVisible}
        onOk={handleCreateScript}
        onCancel={() => setScriptModalVisible(false)}
        width={500}
      >
        <Form form={scriptForm} layout="vertical">
          <Form.Item name="name" label="脚本名称" rules={[{ required: true }]}>
            <Input placeholder="脚本名称" />
          </Form.Item>
          <Form.Item name="version" label="版本" rules={[{ required: true }]}>
            <Input placeholder="如: 1.0.0" />
          </Form.Item>
          <Form.Item name="script_type" label="脚本类型" rules={[{ required: true }]}>
            <Select>
              <Option value="python">Python</Option>
              <Option value="lua">Lua</Option>
              <Option value="instruction">指令脚本</Option>
            </Select>
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
          <Form.Item name="parameters" label="参数定义(JSON)" initialValue="{}">
            <TextArea rows={3} placeholder='{"name": "value"}' style={{ fontFamily: 'monospace' }} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <TextArea rows={2} placeholder="脚本描述" />
          </Form.Item>
          {!currentScript && (
            <Form.Item name="content" label="脚本内容" rules={[{ required: true }]}>
              <TextArea rows={10} style={{ fontFamily: 'monospace' }} placeholder="输入脚本内容" />
            </Form.Item>
          )}
        </Form>
      </Modal>

      {/* 脚本内容编辑弹窗 */}
      <Modal
        title="编辑脚本内容"
        open={contentModalVisible}
        onOk={handleEditScriptContent}
        onCancel={() => setContentModalVisible(false)}
        width={800}
      >
        <Form form={contentForm} layout="vertical">
          <Form.Item name="version" label="新版本号" rules={[{ required: true }]}>
            <Input placeholder="如: 1.0.1" />
          </Form.Item>
          <Form.Item name="content" label="脚本内容" rules={[{ required: true }]}>
            <TextArea rows={15} style={{ fontFamily: 'monospace' }} placeholder="后端不返回明文内容，请粘贴完整新脚本内容" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 发送指令弹窗 */}
      <Modal
        title="发送指令"
        open={instructionModalVisible}
        onOk={handleSendInstruction}
        onCancel={() => setInstructionModalVisible(false)}
        width={500}
      >
        <Form form={instructionForm} layout="vertical">
          <Form.Item name="type" label="指令类型" initialValue="custom" rules={[{ required: true }]}>
            <Select>
              <Option value="get_status">获取状态</Option>
              <Option value="restart">重启应用</Option>
              <Option value="screenshot">截图</Option>
              <Option value="ocr">OCR识别</Option>
              <Option value="exec_script">执行脚本</Option>
              <Option value="custom">自定义指令</Option>
            </Select>
          </Form.Item>
          <Form.Item name="machine_id" label="目标设备" initialValue="">
            <Select
              showSearch
              placeholder="选择在线设备，留空表示广播"
              options={[
                { label: '全部在线设备', value: '' },
                ...onlineDevices.map(device => ({
                  label: `${device.machine_id} (${device.os || '未知'})`,
                  value: device.machine_id,
                })),
              ]}
              filterOption={(input, option) =>
                (option?.label ?? '').toLowerCase().includes(input.toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item name="payload" label="指令内容" initialValue="{}" rules={[{ required: true }]}>
            <TextArea rows={3} placeholder="JSON格式的指令参数" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default AppDetail;
