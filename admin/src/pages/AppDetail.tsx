import React, { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Tabs, Button, Descriptions, Tag, Input, message, Spin, Card, Table, Space, Modal, Form, InputNumber, Upload, Select, Progress, Switch, App } from 'antd';
import { ArrowLeftOutlined, CopyOutlined, KeyOutlined, PlusOutlined, UploadOutlined, EditOutlined, CodeOutlined, RollbackOutlined, SendOutlined } from '@ant-design/icons';
import { appApi, hotUpdateApi, secureScriptApi, instructionApi } from '../api';
import dayjs from 'dayjs';

const { Option } = Select;
const { TextArea } = Input;

const AppDetail: React.FC = () => {
  const { modal } = App.useApp();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [app, setApp] = useState<any>(null);
  const [activeTab, setActiveTab] = useState('info');

  // 版本管理相关（合并了热更新）
  const [versions, setVersions] = useState<any[]>([]);
  const [versionLoading, setVersionLoading] = useState(false);
  const [versionModalVisible, setVersionModalVisible] = useState(false);
  const [versionForm] = Form.useForm();
  const [versionFileList, setVersionFileList] = useState<any[]>([]);
  const [creatingVersion, setCreatingVersion] = useState(false);
  const [versionUploadPercent, setVersionUploadPercent] = useState(0);

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

  useEffect(() => {
    if (id) {
      fetchApp();
    }
  }, [id]);

  useEffect(() => {
    if (id && app) {
      if (activeTab === 'versions') fetchVersions();
      if (activeTab === 'scripts') fetchScripts();
      if (activeTab === 'instructions') fetchOnlineDevices();
    }
  }, [activeTab, id, app]);

  const fetchApp = async () => {
    setLoading(true);
    try {
      const result = await appApi.get(id!);
      setApp(result);
    } catch (error) {
      message.error('获取应用信息失败');
      navigate('/apps');
    } finally {
      setLoading(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    message.success('已复制到剪贴板');
  };

  const handleRegenerateKeys = async () => {
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

  // ==================== 版本管理（合并热更新） ====================
  const fetchVersions = async () => {
    setVersionLoading(true);
    try {
      const result: any = await hotUpdateApi.list(id!);
      setVersions(result?.list ?? (Array.isArray(result) ? result : []));
    } catch (error) {
      console.error(error);
    } finally {
      setVersionLoading(false);
    }
  };

  const handleCreateVersion = async () => {
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
      formData.append('version_code', values.version_code.toString());
      formData.append('update_type', values.update_type || 'full');
      formData.append('update_mode', values.update_mode || 'mixed');
      formData.append('changelog', values.changelog || '');
      formData.append('rollout_percentage', (values.rollout_percentage || 100).toString());
      formData.append('force_update', values.force_update ? 'true' : 'false');
      formData.append('restart_required', values.restart_required ? 'true' : 'false');
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
    try {
      await hotUpdateApi.publish(versionId);
      message.success('发布成功');
      fetchVersions();
    } catch (error) {
      console.error(error);
    }
  };

  const handleDeprecateVersion = async (versionId: string) => {
    modal.confirm({
      title: '确认废弃',
      content: '废弃后该版本将不再提供下载，确定吗？',
      onOk: async () => {
        try {
          await hotUpdateApi.deprecate(versionId);
          message.success('已废弃');
          fetchVersions();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleRollbackVersion = async (versionId: string) => {
    modal.confirm({
      title: '确认回滚',
      content: '回滚后该版本将停止推送，确定吗？',
      onOk: async () => {
        try {
          await hotUpdateApi.rollback(versionId);
          message.success('回滚成功');
          fetchVersions();
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleDeleteVersion = async (versionId: string) => {
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

  // ==================== 安全脚本 ====================
  const fetchScripts = async () => {
    setScriptLoading(true);
    try {
      const result: any = await secureScriptApi.list(id!);
      setScripts(result?.list ?? (Array.isArray(result) ? result : []));
    } catch (error) {
      console.error(error);
    } finally {
      setScriptLoading(false);
    }
  };

  const handleCreateScript = async () => {
    try {
      const values = await scriptForm.validateFields();
      if (currentScript) {
        await secureScriptApi.update(currentScript.id, values);
        message.success('更新成功');
      } else {
        await secureScriptApi.create(id!, values);
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
    try {
      const values = await contentForm.validateFields();
      await secureScriptApi.updateContent(currentScript.id, values);
      message.success('脚本内容已更新');
      setContentModalVisible(false);
      fetchScripts();
    } catch (error) {
      console.error(error);
    }
  };

  const handlePublishScript = async (scriptId: string) => {
    try {
      await secureScriptApi.publish(scriptId);
      message.success('发布成功');
      fetchScripts();
    } catch (error) {
      console.error(error);
    }
  };

  // ==================== 实时指令 ====================
  const fetchOnlineDevices = async () => {
    try {
      const result: any = await instructionApi.getOnlineDevices(id!);
      setOnlineDevices(result?.list ?? (Array.isArray(result) ? result : []));
    } catch (error) {
      console.error(error);
    }
  };

  const handleSendInstruction = async () => {
    try {
      const values = await instructionForm.validateFields();
      await instructionApi.send({
        app_id: id,
        ...values,
      });
      message.success('指令已发送');
      setInstructionModalVisible(false);
      instructionForm.resetFields();
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

  const getUpdateModeTag = (mode: string) => {
    const map: Record<string, string> = {
      exe: '程序更新',
      script: '脚本更新',
      resource: '资源更新',
      mixed: '混合更新',
    };
    return map[mode] || mode;
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
            <Descriptions.Item label="App Secret" span={2}>
              <code>{app.app_secret?.slice(0, 20)}...</code>
              <Button type="link" size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(app.app_secret)} />
            </Descriptions.Item>
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
          <div style={{ marginTop: 16 }}>
            <Button icon={<KeyOutlined />} onClick={handleRegenerateKeys}>重新生成密钥</Button>
          </div>
        </Card>
      ),
    },
    {
      key: 'versions',
      label: '版本管理',
      children: (
        <Card
          title="版本列表"
          extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { versionForm.resetFields(); setVersionFileList([]); setVersionUploadPercent(0); setVersionModalVisible(true); }}>发布新版本</Button>}
        >
          <Table
            loading={versionLoading}
            dataSource={versions}
            rowKey="id"
            columns={[
              { title: '版本', dataIndex: 'to_version', key: 'to_version', render: (v: string, record: any) => <strong>{v || record.version}</strong> },
              { title: '更新类型', dataIndex: 'patch_type', key: 'patch_type', render: (t: string) => getUpdateTypeTag(t) },
              { title: '更新模式', dataIndex: 'update_mode', key: 'update_mode', render: (m: string) => getUpdateModeTag(m) },
              { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => getStatusTag(s) },
              {
                title: '灰度', dataIndex: 'rollout_percentage', key: 'rollout_percentage',
                render: (v: number) => <Progress percent={v || 100} size="small" style={{ width: 80 }} />
              },
              { title: '强制更新', dataIndex: 'force_update', key: 'force_update', render: (v: boolean) => v ? <Tag color="red">是</Tag> : <Tag>否</Tag> },
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
                title: '操作', key: 'action', width: 200,
                render: (_: any, record: any) => (
                  <Space>
                    {record.status === 'draft' && (
                      <>
                        <Button type="link" size="small" onClick={() => handlePublishVersion(record.id)}>发布</Button>
                        <Button type="link" size="small" danger onClick={() => handleDeleteVersion(record.id)}>删除</Button>
                      </>
                    )}
                    {record.status === 'published' && (
                      <>
                        <Button type="link" size="small" icon={<RollbackOutlined />} onClick={() => handleRollbackVersion(record.id)}>回滚</Button>
                        <Button type="link" size="small" danger onClick={() => handleDeprecateVersion(record.id)}>废弃</Button>
                      </>
                    )}
                    {(record.status === 'rollback' || record.status === 'deprecated') && (
                      <Button type="link" size="small" danger onClick={() => handleDeleteVersion(record.id)}>删除</Button>
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
          extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { scriptForm.resetFields(); setCurrentScript(null); setScriptModalVisible(true); }}>创建脚本</Button>}
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
                render: (_: any, record: any) => (
                  <Space>
                    <Button type="link" size="small" icon={<CodeOutlined />} onClick={() => { setCurrentScript(record); contentForm.setFieldsValue({ content: record.content }); setContentModalVisible(true); }}>编辑内容</Button>
                    <Button type="link" size="small" icon={<EditOutlined />} onClick={() => { setCurrentScript(record); scriptForm.setFieldsValue(record); setScriptModalVisible(true); }}>编辑</Button>
                    {record.status === 'draft' && <Button type="link" size="small" onClick={() => handlePublishScript(record.id)}>发布</Button>}
                  </Space>
                ),
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
          extra={<Button type="primary" icon={<SendOutlined />} onClick={() => { instructionForm.resetFields(); setInstructionModalVisible(true); }}>发送指令</Button>}
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

      {/* 版本发布弹窗 */}
      <Modal
        title="发布新版本"
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
          <Form.Item name="version_code" label="版本代码" rules={[{ required: true, message: '请输入版本代码' }]}>
            <InputNumber min={1} style={{ width: '100%' }} placeholder="如: 101（用于版本比较）" />
          </Form.Item>
          <Form.Item name="update_type" label="更新类型" initialValue="full">
            <Select>
              <Option value="full">全量更新（完整安装包）</Option>
              <Option value="patch">增量更新（补丁包）</Option>
            </Select>
          </Form.Item>
          <Form.Item name="update_mode" label="更新模式" initialValue="mixed">
            <Select>
              <Option value="exe">程序更新</Option>
              <Option value="script">脚本更新</Option>
              <Option value="resource">资源更新</Option>
              <Option value="mixed">混合更新</Option>
            </Select>
          </Form.Item>
          <Form.Item name="changelog" label="更新日志">
            <TextArea rows={3} placeholder="本次更新内容说明" />
          </Form.Item>
          <Form.Item name="rollout_percentage" label="灰度比例" initialValue={100}>
            <InputNumber min={1} max={100} addonAfter="%" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="min_app_version" label="最低支持版本">
            <Input placeholder="如: 1.0.0（可选）" />
          </Form.Item>
          <Space style={{ marginBottom: 16 }}>
            <Form.Item name="force_update" valuePropName="checked" style={{ marginBottom: 0 }}>
              <Switch /> 强制更新
            </Form.Item>
            <Form.Item name="restart_required" valuePropName="checked" style={{ marginBottom: 0 }}>
              <Switch /> 需要重启
            </Form.Item>
          </Space>
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
              <Option value="lua">Lua</Option>
              <Option value="javascript">JavaScript</Option>
              <Option value="python">Python</Option>
            </Select>
          </Form.Item>
          <Form.Item name="description" label="描述">
            <TextArea rows={2} placeholder="脚本描述" />
          </Form.Item>
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
          <Form.Item name="content" label="脚本内容">
            <TextArea rows={15} style={{ fontFamily: 'monospace' }} placeholder="输入脚本内容" />
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
          <Form.Item name="instruction_type" label="指令类型" rules={[{ required: true }]}>
            <Select>
              <Option value="reload_config">重新加载配置</Option>
              <Option value="update_script">更新脚本</Option>
              <Option value="restart">重启应用</Option>
              <Option value="custom">自定义指令</Option>
            </Select>
          </Form.Item>
          <Form.Item name="target_type" label="目标类型" rules={[{ required: true }]}>
            <Select>
              <Option value="all">所有设备</Option>
              <Option value="device">指定设备</Option>
            </Select>
          </Form.Item>
          <Form.Item name="payload" label="指令内容">
            <TextArea rows={3} placeholder="JSON格式的指令参数" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default AppDetail;
