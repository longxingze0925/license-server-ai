import React, { useCallback, useEffect, useState } from 'react';
import { Table, Card, Space, Modal, Tag, Select, Descriptions, Tabs, Spin, Button, message, Empty } from 'antd';
import { DatabaseOutlined, EyeOutlined, EyeInvisibleOutlined } from '@ant-design/icons';
import { appApi } from '../api';
import request from '../api/request';

const { Option } = Select;
const { TabPane } = Tabs;

// 数据类型映射
const dataTypeLabels: Record<string, string> = {
  scripts: '话术管理',
  danmaku_groups: '互动规则',
  ai_config: 'AI配置',
  random_word_ai_config: '随机词AI配置',
};

const DataBackups: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [pageLoading, setPageLoading] = useState(true);
  const [apps, setApps] = useState<any[]>([]);
  const [selectedApp, setSelectedApp] = useState<string>('');
  const [users, setUsers] = useState<any[]>([]);
  const [selectedUser, setSelectedUser] = useState<any>(null);
  const [backups, setBackups] = useState<Record<string, any[]>>({});
  const [detailVisible, setDetailVisible] = useState(false);
  const [backupDetail, setBackupDetail] = useState<any>(null);
  const [showApiKey, setShowApiKey] = useState(false);
  const [secretLoading, setSecretLoading] = useState(false);

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

  const fetchUsers = useCallback(async () => {
    if (!selectedApp) return;
    setLoading(true);
    try {
      const result: any = await request.get('/admin/backups/users', { params: { app_id: selectedApp } });
      setUsers(result?.users || []);
    } catch (error) {
      console.error(error);
      setUsers([]);
    } finally {
      setLoading(false);
    }
  }, [selectedApp]);

  useEffect(() => {
    if (selectedApp) {
      setSelectedUser(null);
      setBackups({});
      fetchUsers();
    }
  }, [fetchUsers, selectedApp]);

  const fetchUserBackups = async (userId: string) => {
    try {
      const result: any = await request.get(`/admin/backups/users/${userId}`, { params: { app_id: selectedApp } });
      setBackups(result?.backups || {});
    } catch (error) {
      console.error(error);
      setBackups({});
    }
  };

  const handleUserSelect = (user: any) => {
    setSelectedUser(user);
    fetchUserBackups(user.user_id);
  };

  const fetchBackupDetail = async (backupId: string, revealSecret = false) => {
    const result: any = await request.get(`/admin/backups/${backupId}`, {
      params: revealSecret ? { reveal_secret: true } : undefined,
    });
    setBackupDetail(result);
  };

  const handleViewDetail = async (backup: any) => {
    try {
      await fetchBackupDetail(backup.id);
      setShowApiKey(false); // 重置 API Key 显示状态
      setDetailVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const toggleApiKeyVisible = async () => {
    if (!backupDetail?.id) return;
    const nextVisible = !showApiKey;
    setSecretLoading(true);
    try {
      await fetchBackupDetail(backupDetail.id, nextVisible);
      setShowApiKey(nextVisible);
    } catch {
      message.error('读取 API Key 失败');
    } finally {
      setSecretLoading(false);
    }
  };

  const handleSetCurrent = async (backupId: string) => {
    try {
      await request.post(`/admin/backups/${backupId}/set-current`);
      message.success('已设置为当前版本');
      if (selectedUser) {
        fetchUserBackups(selectedUser.user_id);
      }
    } catch {
      message.error('操作失败');
    }
  };

  const userColumns = [
    {
      title: '用户邮箱',
      dataIndex: 'email',
      key: 'email',
    },
    {
      title: '用户名',
      dataIndex: 'name',
      key: 'name',
      render: (v: string) => v || '-',
    },
    {
      title: '备份统计',
      dataIndex: 'stats',
      key: 'stats',
      render: (stats: any[]) => (
        <Space>
          {stats?.map((s: any) => (
            <Tag key={s.DataType} color="blue">
              {dataTypeLabels[s.DataType] || s.DataType}: {s.VersionCount}个版本
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: any, record: any) => (
        <Button type="link" onClick={() => handleUserSelect(record)}>
          查看备份
        </Button>
      ),
    },
  ];

  const renderBackupList = (_dataType: string, backupList: any[]) => {
    return (
      <Table
        dataSource={backupList}
        rowKey="id"
        size="small"
        pagination={false}
        columns={[
          {
            title: '版本',
            dataIndex: 'version',
            key: 'version',
            render: (v: number, record: any) => (
              <Space>
                <span>v{v}</span>
                {record.is_current && <Tag color="green">当前</Tag>}
              </Space>
            ),
          },
          {
            title: '设备',
            dataIndex: 'device_name',
            key: 'device_name',
            render: (v: string) => v || '-',
          },
          {
            title: '条目数',
            dataIndex: 'item_count',
            key: 'item_count',
          },
          {
            title: '大小',
            dataIndex: 'data_size',
            key: 'data_size',
            render: (v: number) => {
              if (v < 1024) return `${v} B`;
              if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`;
              return `${(v / 1024 / 1024).toFixed(1)} MB`;
            },
          },
          {
            title: '备份时间',
            dataIndex: 'created_at',
            key: 'created_at',
          },
          {
            title: '操作',
            key: 'action',
            render: (_: any, record: any) => (
              <Space>
                <Button type="link" size="small" onClick={() => handleViewDetail(record)}>
                  查看详情
                </Button>
                {!record.is_current && (
                  <Button type="link" size="small" onClick={() => handleSetCurrent(record.id)}>
                    设为当前
                  </Button>
                )}
              </Space>
            ),
          },
        ]}
      />
    );
  };

  const renderDataJSON = (dataJSON: string, dataType: string) => {
    try {
      const data = JSON.parse(dataJSON);

      if (dataType === 'scripts') {
        // 话术列表
        if (Array.isArray(data)) {
          return (
            <div style={{ maxHeight: 500, overflow: 'auto' }}>
              {data.map((item: any, index: number) => (
                <Card key={index} size="small" style={{ marginBottom: 8 }}>
                  <p><strong>{item.name || `话术 ${index + 1}`}</strong></p>
                  <div style={{ maxHeight: 200, overflow: 'auto', whiteSpace: 'pre-wrap', color: '#666' }}>
                    {item.content}
                  </div>
                </Card>
              ))}
            </div>
          );
        }
      }

      if (dataType === 'danmaku_groups') {
        // 互动规则
        if (Array.isArray(data)) {
          return (
            <div style={{ maxHeight: 400, overflow: 'auto' }}>
              {data.map((group: any, index: number) => (
                <Card key={index} size="small" style={{ marginBottom: 8 }}>
                  <p><strong>{group.name || `分组 ${index + 1}`}</strong></p>
                  <p>规则数: {group.rules?.length || 0}</p>
                </Card>
              ))}
            </div>
          );
        }
      }

      if (dataType === 'ai_config' || dataType === 'random_word_ai_config') {
        // AI配置
        const maskApiKey = (key: string) => {
          if (!key || key.length <= 8) return '********';
          return key.slice(0, 4) + '****' + key.slice(-4);
        };

        return (
          <Descriptions column={1} size="small">
            <Descriptions.Item label="API地址">{data.base_url || '-'}</Descriptions.Item>
            <Descriptions.Item label="模型">{data.model || '-'}</Descriptions.Item>
            <Descriptions.Item label="API Key">
              <Space>
                <span style={{ fontFamily: 'monospace' }}>
                  {showApiKey ? (data.api_key || '-') : maskApiKey(data.api_key)}
                </span>
                <Button
                  type="link"
                  size="small"
                  icon={showApiKey ? <EyeInvisibleOutlined /> : <EyeOutlined />}
                  loading={secretLoading}
                  onClick={toggleApiKeyVisible}
                >
                  {showApiKey ? '隐藏' : '显示'}
                </Button>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="最大Token">{data.max_token || '-'}</Descriptions.Item>
          </Descriptions>
        );
      }

      // 默认JSON显示
      return <pre style={{ maxHeight: 400, overflow: 'auto' }}>{JSON.stringify(data, null, 2)}</pre>;
    } catch {
      return <pre style={{ maxHeight: 400, overflow: 'auto' }}>{dataJSON}</pre>;
    }
  };

  if (pageLoading) {
    return (
      <div style={{ textAlign: 'center', padding: 50 }}>
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div>
      <Card>
        <Space style={{ marginBottom: 16 }}>
          <span>选择应用：</span>
          <Select
            value={selectedApp}
            onChange={setSelectedApp}
            style={{ width: 200 }}
            placeholder="选择应用"
          >
            {apps.map((app: any) => (
              <Option key={app.id} value={app.id}>{app.name}</Option>
            ))}
          </Select>
        </Space>

        {selectedUser ? (
          <div>
            <Button onClick={() => setSelectedUser(null)} style={{ marginBottom: 16 }}>
              ← 返回用户列表
            </Button>
            <Card title={`用户: ${selectedUser.email}`}>
              <Tabs>
                {Object.entries(backups).map(([dataType, backupList]) => (
                  <TabPane
                    tab={
                      <span>
                        <DatabaseOutlined />
                        {dataTypeLabels[dataType] || dataType} ({backupList.length})
                      </span>
                    }
                    key={dataType}
                  >
                    {renderBackupList(dataType, backupList)}
                  </TabPane>
                ))}
              </Tabs>
              {Object.keys(backups).length === 0 && (
                <Empty description="暂无备份数据" />
              )}
            </Card>
          </div>
        ) : (
          <Table
            loading={loading}
            dataSource={users}
            columns={userColumns}
            rowKey="user_id"
            locale={{ emptyText: '暂无备份数据' }}
          />
        )}
      </Card>

      <Modal
        title="备份详情"
        open={detailVisible}
        onCancel={() => {
          setDetailVisible(false);
          setShowApiKey(false);
          setBackupDetail(null);
        }}
        footer={null}
        width={700}
      >
        {backupDetail && (
          <div>
            <Descriptions column={2} size="small" style={{ marginBottom: 16 }}>
              <Descriptions.Item label="数据类型">
                {dataTypeLabels[backupDetail.data_type] || backupDetail.data_type}
              </Descriptions.Item>
              <Descriptions.Item label="版本">v{backupDetail.version}</Descriptions.Item>
              <Descriptions.Item label="设备">{backupDetail.device_name || '-'}</Descriptions.Item>
              <Descriptions.Item label="设备ID">{backupDetail.machine_id}</Descriptions.Item>
              <Descriptions.Item label="条目数">{backupDetail.item_count}</Descriptions.Item>
              <Descriptions.Item label="数据大小">{backupDetail.data_size} 字节</Descriptions.Item>
              <Descriptions.Item label="备份时间">{backupDetail.created_at}</Descriptions.Item>
              <Descriptions.Item label="状态">
                {backupDetail.is_current ? <Tag color="green">当前版本</Tag> : <Tag>历史版本</Tag>}
              </Descriptions.Item>
            </Descriptions>

            <Card title="数据内容" size="small">
              {renderDataJSON(backupDetail.data_json, backupDetail.data_type)}
            </Card>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default DataBackups;
