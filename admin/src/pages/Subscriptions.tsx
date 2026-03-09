import React, { useEffect, useRef, useState } from 'react';
import { Table, Button, Space, Modal, Form, Select, message, Tag, InputNumber, Descriptions, Input, Checkbox, App, Tooltip, Typography } from 'antd';
import { PlusOutlined, DeleteOutlined, EditOutlined } from '@ant-design/icons';
import { subscriptionApi, appApi, customerApi } from '../api';
import dayjs from 'dayjs';
import { useAuthStore } from '../store';

const { Option } = Select;
const CUSTOMER_PAGE_SIZE = 20;
const ACCOUNT_DETAIL_PAGE_SIZE = 100;
const { Text } = Typography;

type AccountSummary = {
  customer_id: string;
  customer_email: string;
  customer_name?: string;
  subscription_count: number;
  app_names?: string[];
  active_count?: number;
  expired_count?: number;
  cancelled_count?: number;
  suspended_count?: number;
  permanent_count?: number;
  nearest_expire_at?: string;
  latest_created_at?: string;
};

const Subscriptions: React.FC = () => {
  const { modal } = App.useApp();
  const { user } = useAuthStore();
  const isViewer = user?.role === 'viewer';

  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<AccountSummary[]>([]);
  const [apps, setApps] = useState<any[]>([]);
  const [customers, setCustomers] = useState<any[]>([]);
  const [customerLoading, setCustomerLoading] = useState(false);
  const [customerPage, setCustomerPage] = useState(1);
  const [customerHasMore, setCustomerHasMore] = useState(true);
  const [customerKeyword, setCustomerKeyword] = useState('');
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [renewModalVisible, setRenewModalVisible] = useState(false);
  const [renewingSubscription, setRenewingSubscription] = useState<any>(null);
  const [currentSubscription, setCurrentSubscription] = useState<any>(null);
  const [expandedRowKeys, setExpandedRowKeys] = useState<React.Key[]>([]);
  const [accountSubscriptions, setAccountSubscriptions] = useState<Record<string, any[]>>({});
  const [accountSubscriptionsLoading, setAccountSubscriptionsLoading] = useState<Record<string, boolean>>({});
  const [form] = Form.useForm();
  const [renewForm] = Form.useForm();
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [filters, setFilters] = useState<any>({});
  const [selectedAppFeatures, setSelectedAppFeatures] = useState<string[]>([]);
  const customerSearchTimerRef = useRef<number | null>(null);
  const latestCustomerRequestRef = useRef(0);
  const expandedScrollTimerRef = useRef<number | null>(null);

  useEffect(() => {
    fetchData();
    fetchApps();
    fetchCustomers('', 1);
  }, []);

  useEffect(() => {
    return () => {
      if (customerSearchTimerRef.current !== null) {
        window.clearTimeout(customerSearchTimerRef.current);
      }
      if (expandedScrollTimerRef.current !== null) {
        window.clearTimeout(expandedScrollTimerRef.current);
      }
    };
  }, []);

  const resetExpandedRows = () => {
    setExpandedRowKeys([]);
    setAccountSubscriptions({});
    setAccountSubscriptionsLoading({});
  };

  const scrollToExpandedPanel = (customerID: string) => {
    if (expandedScrollTimerRef.current !== null) {
      window.clearTimeout(expandedScrollTimerRef.current);
    }

    expandedScrollTimerRef.current = window.setTimeout(() => {
      const panel = document.getElementById(`subscription-account-panel-${customerID}`);
      panel?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
      expandedScrollTimerRef.current = null;
    }, 120);
  };

  const fetchData = async (page = 1, pageSize = 10, filterParams = filters): Promise<AccountSummary[]> => {
    setLoading(true);
    try {
      const result: any = await subscriptionApi.listAccounts({ page, page_size: pageSize, ...filterParams });
      const list = result.list || [];
      setData(list);
      setPagination({ current: page, pageSize, total: result.total || 0 });
      setExpandedRowKeys(prev => prev.filter(key => list.some((item: AccountSummary) => item.customer_id === key)));
      return list;
    } catch (error) {
      console.error(error);
      return [];
    } finally {
      setLoading(false);
    }
  };

  const fetchApps = async () => {
    try {
      const result: any = await appApi.list();
      setApps(result || []);
    } catch (error) {
      console.error(error);
    }
  };

  const collectPinnedCustomers = (baseCustomers: any[]) => {
    const pinned = new Map<string, any>();
    const selectedCustomerID = form.getFieldValue('customer_id');

    const addCustomer = (customer?: any) => {
      if (customer?.id) {
        pinned.set(customer.id, customer);
      }
    };

    const addCustomerByID = (customerID?: string) => {
      if (!customerID || pinned.has(customerID)) {
        return;
      }

      const matchedCustomer = baseCustomers.find(customer => customer.id === customerID);
      if (matchedCustomer) {
        pinned.set(matchedCustomer.id, matchedCustomer);
      }
    };

    addCustomer(currentSubscription?.customer);
    addCustomerByID(currentSubscription?.customer_id);
    addCustomerByID(selectedCustomerID);

    return Array.from(pinned.values());
  };

  const mergeCustomers = (nextCustomers: any[], append = false) => {
    setCustomers(prev => {
      const map = new Map<string, any>();
      const sourceCustomers = append ? [...prev, ...nextCustomers] : nextCustomers;
      const pinnedCustomers = collectPinnedCustomers(prev);

      [...sourceCustomers, ...pinnedCustomers].forEach(customer => {
        if (customer?.id && !map.has(customer.id)) {
          map.set(customer.id, customer);
        }
      });

      return Array.from(map.values());
    });
  };

  const ensureCustomerOption = (customer?: any) => {
    if (!customer?.id) {
      return;
    }

    setCustomers(prev => {
      const map = new Map<string, any>();
      [customer, ...prev].forEach(item => {
        if (item?.id && !map.has(item.id)) {
          map.set(item.id, item);
        }
      });
      return Array.from(map.values());
    });
  };

  const fetchCustomers = async (keyword = '', page = 1, append = false) => {
    const normalizedKeyword = keyword.trim();
    const requestID = latestCustomerRequestRef.current + 1;
    latestCustomerRequestRef.current = requestID;
    setCustomerLoading(true);

    try {
      const result: any = await customerApi.list({
        page,
        page_size: CUSTOMER_PAGE_SIZE,
        keyword: normalizedKeyword || undefined,
      });

      if (requestID === latestCustomerRequestRef.current) {
        const list = result.list || [];
        mergeCustomers(list, append);
        setCustomerKeyword(normalizedKeyword);
        setCustomerPage(page);

        const total = typeof result.total === 'number' ? result.total : 0;
        setCustomerHasMore(total > 0 ? page * CUSTOMER_PAGE_SIZE < total : list.length === CUSTOMER_PAGE_SIZE);
      }
    } catch (error) {
      console.error(error);
    } finally {
      if (requestID === latestCustomerRequestRef.current) {
        setCustomerLoading(false);
      }
    }
  };

  const handleCustomerSearch = (keyword: string) => {
    if (customerSearchTimerRef.current !== null) {
      window.clearTimeout(customerSearchTimerRef.current);
    }

    customerSearchTimerRef.current = window.setTimeout(() => {
      fetchCustomers(keyword, 1);
    }, 300);
  };

  const handleCustomerPopupScroll = (event: React.UIEvent<HTMLDivElement>) => {
    const target = event.target as HTMLDivElement;

    if (customerLoading || !customerHasMore || !!currentSubscription) {
      return;
    }

    if (target.scrollTop + target.clientHeight >= target.scrollHeight - 24) {
      fetchCustomers(customerKeyword, customerPage + 1, true);
    }
  };

  const fetchAccountSubscriptions = async (customerID: string, force = false) => {
    if (!force && accountSubscriptions[customerID]) {
      return;
    }

    setAccountSubscriptionsLoading(prev => ({ ...prev, [customerID]: true }));
    try {
      const result: any = await subscriptionApi.list({
        page: 1,
        page_size: ACCOUNT_DETAIL_PAGE_SIZE,
        customer_id: customerID,
        app_id: filters.app_id,
        status: filters.status,
      });
      setAccountSubscriptions(prev => ({ ...prev, [customerID]: result.list || [] }));
    } catch (error) {
      console.error(error);
    } finally {
      setAccountSubscriptionsLoading(prev => ({ ...prev, [customerID]: false }));
    }
  };

  const refreshAfterMutation = async (customerID?: string) => {
    const nextList = await fetchData(pagination.current, pagination.pageSize);
    if (customerID && expandedRowKeys.includes(customerID) && nextList.some(item => item.customer_id === customerID)) {
      await fetchAccountSubscriptions(customerID, true);
    }
  };

  const handleCreate = () => {
    setCurrentSubscription(null);
    form.resetFields();
    setSelectedAppFeatures([]);
    fetchCustomers('', 1);
    setModalVisible(true);
  };

  const handleAppChange = (appId: string) => {
    const app = apps.find(item => item.id === appId);
    setSelectedAppFeatures(app?.features || []);
    form.setFieldsValue({ features: app?.features || [] });
    if (app?.max_devices_default) {
      form.setFieldsValue({ max_devices: app.max_devices_default });
    }
  };

  const handleView = async (record: any) => {
    try {
      const detail: any = await subscriptionApi.get(record.id);
      ensureCustomerOption(detail.customer);
      setCurrentSubscription(detail);
      setDetailVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const handleEdit = async (record: any) => {
    try {
      const detail: any = await subscriptionApi.get(record.id);
      const app = apps.find(item => item.id === detail.app_id);
      setSelectedAppFeatures(app?.features || []);
      ensureCustomerOption(detail.customer);
      setCurrentSubscription(detail);
      form.setFieldsValue({
        ...detail,
        features: Array.isArray(detail.features) ? detail.features : [],
      });
      setModalVisible(true);
    } catch (error) {
      console.error(error);
    }
  };

  const handleDelete = (record: any) => {
    modal.confirm({
      title: '确认删除',
      content: '确定要删除此订阅吗？',
      onOk: async () => {
        try {
          await subscriptionApi.delete(record.id);
          message.success('删除成功');
          await refreshAfterMutation(record.customer_id);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      const targetCustomerID = currentSubscription?.customer_id || values.customer_id;

      if (currentSubscription) {
        await subscriptionApi.update(currentSubscription.id, {
          max_devices: values.max_devices,
          unbind_limit: values.unbind_limit,
          features: values.features ?? [],
          notes: values.notes ?? '',
        });
        message.success('更新成功');
      } else {
        await subscriptionApi.create({
          app_id: values.app_id,
          customer_id: values.customer_id,
          max_devices: values.max_devices,
          unbind_limit: values.unbind_limit,
          days: values.days,
          features: values.features ?? [],
          notes: values.notes ?? '',
        });
        message.success('创建成功');
      }

      setModalVisible(false);
      await refreshAfterMutation(targetCustomerID);
    } catch (error) {
      console.error(error);
    }
  };

  const handleResetUnbindCount = (record: any) => {
    modal.confirm({
      title: '重置解绑次数',
      content: '确定要重置该订阅的客户端解绑次数吗？',
      onOk: async () => {
        try {
          await subscriptionApi.resetUnbindCount(record.id);
          message.success('解绑次数已重置');
          await refreshAfterMutation(record.customer_id);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleRenew = (record: any) => {
    setRenewingSubscription(record);
    renewForm.setFieldsValue({ days: 30 });
    setRenewModalVisible(true);
  };

  const handleRenewSubmit = async () => {
    if (!renewingSubscription) {
      return;
    }

    try {
      const values = await renewForm.validateFields();
      await subscriptionApi.renew(renewingSubscription.id, { days: values.days });
      message.success('续费成功');
      setRenewModalVisible(false);
      setRenewingSubscription(null);
      renewForm.resetFields();
      await refreshAfterMutation(renewingSubscription.customer_id);
    } catch (error) {
      console.error(error);
    }
  };

  const handleRenewCancel = () => {
    setRenewModalVisible(false);
    setRenewingSubscription(null);
    renewForm.resetFields();
  };

  const handleCancel = (record: any) => {
    modal.confirm({
      title: '取消订阅',
      content: '确定要取消此订阅吗？',
      onOk: async () => {
        try {
          await subscriptionApi.cancel(record.id);
          message.success('订阅已取消');
          await refreshAfterMutation(record.customer_id);
        } catch (error) {
          console.error(error);
        }
      },
    });
  };

  const handleTableChange = (pag: any) => {
    resetExpandedRows();
    fetchData(pag.current, pag.pageSize);
  };

  const handleSearch = (values: any) => {
    const nextFilters = Object.fromEntries(
      Object.entries(values).filter(([, value]) => value !== undefined && value !== null && value !== '')
    );
    setFilters(nextFilters);
    resetExpandedRows();
    fetchData(1, pagination.pageSize, nextFilters);
  };

  const handleExpandToggle = async (customerID: string) => {
    const isExpanded = expandedRowKeys.includes(customerID);
    if (isExpanded) {
      setExpandedRowKeys(prev => prev.filter(key => key !== customerID));
      return;
    }

    setExpandedRowKeys(prev => [...prev, customerID]);
    scrollToExpandedPanel(customerID);
    await fetchAccountSubscriptions(customerID);
  };

  const getStatusTag = (status: string) => {
    const statusMap: Record<string, { color: string; text: string }> = {
      active: { color: 'green', text: '有效' },
      expired: { color: 'red', text: '已过期' },
      cancelled: { color: 'orange', text: '已取消' },
      suspended: { color: 'default', text: '已暂停' },
    };
    const currentStatus = statusMap[status] || { color: 'default', text: status };
    return <Tag color={currentStatus.color}>{currentStatus.text}</Tag>;
  };

  const getCustomerLabel = (customer?: any) => {
    if (!customer) {
      return '-';
    }
    return `${customer.email}${customer.name ? ` (${customer.name})` : ''}`;
  };

  const getFeatureText = (features: any) => {
    if (Array.isArray(features)) {
      return features.length > 0 ? features.join('、') : '-';
    }
    return features && features !== '[]' ? features : '-';
  };

  const renderAppNames = (appNames?: string[]) => {
    if (!appNames || appNames.length === 0) {
      return '-';
    }

    const visibleNames = appNames.slice(0, 2);
    const hiddenCount = appNames.length - visibleNames.length;
    const hiddenNames = appNames.slice(2);

    return (
      <Space size={[4, 4]} wrap>
        {visibleNames.map(name => (
          <Tooltip key={name} title={name}>
            <Tag color="blue" style={{ marginInlineEnd: 0 }}>{name}</Tag>
          </Tooltip>
        ))}
        {hiddenCount > 0 && (
          <Tooltip title={hiddenNames.join('、')}>
            <Tag color="processing" style={{ marginInlineEnd: 0 }}>+{hiddenCount}</Tag>
          </Tooltip>
        )}
      </Space>
    );
  };

  const renderStatusSummary = (record: AccountSummary) => {
    const items = [
      { key: 'active', count: record.active_count || 0, color: 'green', label: '有效' },
      { key: 'expired', count: record.expired_count || 0, color: 'red', label: '过期' },
      { key: 'cancelled', count: record.cancelled_count || 0, color: 'orange', label: '取消' },
      { key: 'suspended', count: record.suspended_count || 0, color: 'default', label: '暂停' },
      { key: 'permanent', count: record.permanent_count || 0, color: 'blue', label: '永久' },
    ].filter(item => item.count > 0);

    if (items.length === 0) {
      return '-';
    }

    return (
      <Space size={[4, 4]} wrap>
        {items.map(item => (
          <Tag key={item.key} color={item.color} style={{ marginInlineEnd: 0 }}>
            {item.label} {item.count}
          </Tag>
        ))}
      </Space>
    );
  };

  const renderNearestExpire = (record: AccountSummary) => {
    if (record.nearest_expire_at) {
      return (
        <div>
          <div>{dayjs(record.nearest_expire_at).format('YYYY-MM-DD')}</div>
          {(record.permanent_count || 0) > 0 && (
            <Text type="secondary" style={{ fontSize: 12 }}>
              另有 {record.permanent_count} 个永久
            </Text>
          )}
        </div>
      );
    }
    if ((record.permanent_count || 0) > 0) {
      return <Tag color="blue">全部永久</Tag>;
    }
    return '-';
  };

  const accountColumns = [
    {
      title: '账号',
      key: 'customer',
      render: (_: any, record: AccountSummary) => (
        <div style={{ lineHeight: 1.5 }}>
          <Tooltip title={record.customer_email || '-'}>
            <Text strong>{record.customer_email || '-'}</Text>
          </Tooltip>
          <div>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {record.customer_name || '客户名称未设置'}
            </Text>
          </div>
        </div>
      ),
    },
    {
      title: '应用数',
      dataIndex: 'subscription_count',
      key: 'subscription_count',
      width: 90,
    },
    {
      title: '应用订阅',
      key: 'app_names',
      render: (_: any, record: AccountSummary) => renderAppNames(record.app_names),
    },
    {
      title: '状态概览',
      key: 'status_summary',
      render: (_: any, record: AccountSummary) => renderStatusSummary(record),
    },
    {
      title: '最近到期',
      key: 'nearest_expire_at',
      width: 150,
      render: (_: any, record: AccountSummary) => renderNearestExpire(record),
    },
    {
      title: '操作',
      key: 'action',
      width: 120,
      render: (_: any, record: AccountSummary) => (
        <Button type="link" size="small" onClick={() => handleExpandToggle(record.customer_id)}>
          {expandedRowKeys.includes(record.customer_id) ? '收起应用' : '查看应用'}
        </Button>
      ),
    },
  ];

  const subscriptionColumns = [
    {
      title: '应用',
      key: 'app_name',
      render: (_: any, record: any) => record.app_name || '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => getStatusTag(status),
    },
    {
      title: '最大设备数',
      dataIndex: 'max_devices',
      key: 'max_devices',
      width: 110,
    },
    {
      title: '解绑剩余',
      key: 'unbind_remaining',
      width: 120,
      render: (_: any, record: any) => `${record.unbind_remaining ?? 0}/${record.unbind_limit ?? 0}`,
    },
    {
      title: '剩余天数',
      dataIndex: 'remaining_days',
      key: 'remaining_days',
      width: 100,
      render: (value: number) => value === -1 ? '永久' : `${value} 天`,
    },
    {
      title: '过期时间',
      dataIndex: 'expire_at',
      key: 'expire_at',
      width: 120,
      render: (value: string) => value ? dayjs(value).format('YYYY-MM-DD') : '永久',
    },
    {
      title: '操作',
      key: 'action',
      width: 260,
      render: (_: any, record: any) => (
        <Space wrap>
          <Button type="link" size="small" onClick={() => handleView(record)}>详情</Button>
          {!isViewer && (
            <>
              <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>编辑</Button>
              <Button type="link" size="small" onClick={() => handleResetUnbindCount(record)}>重置解绑</Button>
              <Button type="link" size="small" onClick={() => handleRenew(record)}>续费</Button>
              {record.status === 'active' && (
                <Button type="link" size="small" danger onClick={() => handleCancel(record)}>取消</Button>
              )}
              <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)}>删除</Button>
            </>
          )}
        </Space>
      ),
    },
  ];

  const renderExpandedSubscriptions = (record: AccountSummary) => {
    return (
      <div
        id={`subscription-account-panel-${record.customer_id}`}
        style={{
          padding: 12,
          borderRadius: 10,
          background: '#fafafa',
          border: '1px solid #f0f0f0',
        }}
      >
        <div style={{ marginBottom: 10, fontSize: 13, color: '#666', fontWeight: 500 }}>
          应用订阅明细
        </div>
        <Table
          columns={subscriptionColumns}
          dataSource={accountSubscriptions[record.customer_id] || []}
          rowKey="id"
          size="small"
          pagination={false}
          loading={!!accountSubscriptionsLoading[record.customer_id]}
          locale={{ emptyText: '该账号下暂无匹配的应用订阅' }}
        />
      </div>
    );
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <h2 style={{ margin: 0 }}>订阅管理</h2>
        {!isViewer && <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>创建订阅</Button>}
      </div>

      <Form layout="inline" onFinish={handleSearch} style={{ marginBottom: 16 }}>
        <Form.Item name="keyword">
          <Input placeholder="搜索账号邮箱 / 名称 / 公司" allowClear style={{ width: 220 }} />
        </Form.Item>
        <Form.Item name="app_id">
          <Select placeholder="选择应用" allowClear style={{ width: 160 }}>
            {apps.map(app => <Option key={app.id} value={app.id}>{app.name}</Option>)}
          </Select>
        </Form.Item>
        <Form.Item name="status">
          <Select placeholder="状态" allowClear style={{ width: 120 }}>
            <Option value="active">有效</Option>
            <Option value="expired">已过期</Option>
            <Option value="cancelled">已取消</Option>
            <Option value="suspended">已暂停</Option>
          </Select>
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit">搜索</Button>
        </Form.Item>
      </Form>

      <Table
        columns={accountColumns}
        dataSource={data}
        rowKey="customer_id"
        loading={loading}
        pagination={pagination}
        onChange={handleTableChange}
        expandable={{
          expandedRowKeys,
          expandedRowRender: renderExpandedSubscriptions,
          showExpandColumn: false,
          rowExpandable: (record: AccountSummary) => record.subscription_count > 0,
        }}
      />

      <Modal
        title={currentSubscription ? '编辑订阅' : '创建订阅'}
        open={modalVisible}
        onOk={handleSubmit}
        onCancel={() => setModalVisible(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="app_id" label="应用" rules={[{ required: true, message: '请选择应用' }]}>
            <Select placeholder="选择应用" disabled={!!currentSubscription} onChange={handleAppChange}>
              {apps.map(app => <Option key={app.id} value={app.id}>{app.name}</Option>)}
            </Select>
          </Form.Item>
          <Form.Item name="customer_id" label="客户" rules={[{ required: true, message: '请选择客户' }]}>
            <Select
              placeholder="搜索邮箱 / 姓名 / 公司"
              showSearch
              filterOption={false}
              allowClear
              disabled={!!currentSubscription}
              loading={customerLoading}
              onSearch={handleCustomerSearch}
              onClear={() => fetchCustomers('', 1)}
              onOpenChange={(open) => {
                if (open) {
                  fetchCustomers(customerKeyword, 1);
                }
              }}
              onPopupScroll={handleCustomerPopupScroll}
              notFoundContent={customerLoading ? '搜索中...' : '暂无客户'}
            >
              {customers.map(customer => <Option key={customer.id} value={customer.id}>{getCustomerLabel(customer)}</Option>)}
            </Select>
          </Form.Item>
          <Form.Item name="max_devices" label="最大设备数" initialValue={1}>
            <InputNumber min={1} max={100} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="unbind_limit"
            label="终身解绑总次数"
            initialValue={5}
            rules={[{ required: true, message: '请输入终身解绑总次数' }]}
            extra="客户端累计解绑总上限，超限后只能管理员后台解绑"
          >
            <InputNumber min={0} max={1000} style={{ width: '100%' }} />
          </Form.Item>
          {!currentSubscription && (
            <Form.Item name="days" label="有效天数" initialValue={365} rules={[{ required: true, message: '请输入有效天数' }]} extra="-1表示永久有效">
              <InputNumber min={-1} style={{ width: '100%' }} />
            </Form.Item>
          )}
          <Form.Item name="features" label="功能权限">
            {selectedAppFeatures.length > 0 ? (
              <Checkbox.Group>
                <Space direction="vertical">
                  {selectedAppFeatures.map(feature => (
                    <Checkbox key={feature} value={feature}>{feature}</Checkbox>
                  ))}
                </Space>
              </Checkbox.Group>
            ) : (
              <span style={{ color: '#999' }}>请先选择应用，或该应用未配置功能列表</span>
            )}
          </Form.Item>
          <Form.Item name="notes" label="备注">
            <Input.TextArea placeholder="备注信息" rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="续费订阅"
        open={renewModalVisible}
        onOk={handleRenewSubmit}
        onCancel={handleRenewCancel}
        width={420}
      >
        <Form form={renewForm} layout="vertical">
          <Form.Item name="days" label="续费天数" initialValue={30} rules={[{ required: true, message: '请输入续费天数' }]}>
            <InputNumber min={1} max={3650} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="订阅详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={600}
      >
        {currentSubscription && (
          <Descriptions column={2} bordered size="small">
            <Descriptions.Item label="应用">{currentSubscription.application?.name || '-'}</Descriptions.Item>
            <Descriptions.Item label="客户">
              {getCustomerLabel(currentSubscription.customer) !== '-'
                ? getCustomerLabel(currentSubscription.customer)
                : getCustomerLabel(customers.find(customer => customer.id === currentSubscription.customer_id))}
            </Descriptions.Item>
            <Descriptions.Item label="状态">{getStatusTag(currentSubscription.status)}</Descriptions.Item>
            <Descriptions.Item label="最大设备数">{currentSubscription.max_devices}</Descriptions.Item>
            <Descriptions.Item label="终身解绑总次数">{currentSubscription.unbind_limit ?? 0}</Descriptions.Item>
            <Descriptions.Item label="已用解绑次数">{currentSubscription.unbind_used ?? 0}</Descriptions.Item>
            <Descriptions.Item label="剩余解绑次数">{currentSubscription.unbind_remaining ?? 0}</Descriptions.Item>
            <Descriptions.Item label="剩余天数">
              {currentSubscription.remaining_days === -1 ? '永久' : `${currentSubscription.remaining_days} 天`}
            </Descriptions.Item>
            <Descriptions.Item label="开始时间">
              {currentSubscription.start_at ? dayjs(currentSubscription.start_at).format('YYYY-MM-DD HH:mm') : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="过期时间">
              {currentSubscription.expire_at ? dayjs(currentSubscription.expire_at).format('YYYY-MM-DD HH:mm') : '永久'}
            </Descriptions.Item>
            <Descriptions.Item label="创建时间" span={2}>
              {dayjs(currentSubscription.created_at).format('YYYY-MM-DD HH:mm')}
            </Descriptions.Item>
            <Descriptions.Item label="功能权限" span={2}>
              {getFeatureText(currentSubscription.features)}
            </Descriptions.Item>
            <Descriptions.Item label="备注" span={2}>{currentSubscription.notes || '-'}</Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

export default Subscriptions;
