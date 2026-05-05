import React, { useCallback, useEffect, useState } from 'react';
import {
  Table,
  Button,
  Space,
  Modal,
  Form,
  Input,
  InputNumber,
  message,
  Tag,
  Drawer,
  Statistic,
  Card,
} from 'antd';
import {
  EditOutlined,
  ReloadOutlined,
  DollarOutlined,
  HistoryOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { userCreditApi } from '../api';

interface UserCreditRow {
  user_id: string;
  user_type?: 'customer' | 'team_member' | string;
  email: string;
  name: string;
  balance: number;
  total_topup: number;
  total_consumed: number;
  concurrent_limit: number;
  updated_at: string;
}

interface TxRow {
  id: number;
  type: string;
  amount: number;
  balance_after: number;
  task_id: string;
  rule_id: number;
  operator_id: string;
  note: string;
  created_at: string;
}

const TYPE_TAG: Record<string, { color: string; label: string }> = {
  adjust: { color: 'blue', label: '调整' },
  consume: { color: 'orange', label: '消费' },
  refund: { color: 'green', label: '退款' },
};

const USER_TYPE_TAG: Record<string, { color: string; label: string }> = {
  customer: { color: 'green', label: '客户' },
  team_member: { color: 'blue', label: '团队成员' },
};

const UserCredits: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<UserCreditRow[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [keyword, setKeyword] = useState('');

  const [adjustOpen, setAdjustOpen] = useState(false);
  const [adjustTarget, setAdjustTarget] = useState<UserCreditRow | null>(null);
  const [adjustForm] = Form.useForm();

  const [limitOpen, setLimitOpen] = useState(false);
  const [limitTarget, setLimitTarget] = useState<UserCreditRow | null>(null);
  const [limitForm] = Form.useForm();

  const [txOpen, setTxOpen] = useState(false);
  const [txTarget, setTxTarget] = useState<UserCreditRow | null>(null);
  const [txList, setTxList] = useState<TxRow[]>([]);
  const [txLoading, setTxLoading] = useState(false);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const result: any = await userCreditApi.list({ keyword, page, page_size: pageSize });
      setData(result?.list || []);
      setTotal(result?.total || 0);
    } finally { setLoading(false); }
  }, [keyword, page, pageSize]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const openAdjust = (row: UserCreditRow) => {
    setAdjustTarget(row);
    adjustForm.resetFields();
    setAdjustOpen(true);
  };

  const submitAdjust = async () => {
    const v = await adjustForm.validateFields();
    if (!adjustTarget) return;
    try {
      await userCreditApi.adjust(adjustTarget.user_id, { amount: Number(v.amount), note: v.note || '' });
      message.success(`已调整：${v.amount > 0 ? '+' : ''}${v.amount}`);
      setAdjustOpen(false);
      fetchData();
    } catch (e: any) {
      // 后端 402 表示扣减后会变负
      message.error(e?.message || '调整失败');
    }
  };

  const openLimit = (row: UserCreditRow) => {
    setLimitTarget(row);
    limitForm.setFieldsValue({ concurrent_limit: row.concurrent_limit });
    setLimitOpen(true);
  };

  const submitLimit = async () => {
    const v = await limitForm.validateFields();
    if (!limitTarget) return;
    await userCreditApi.setLimits(limitTarget.user_id, { concurrent_limit: Number(v.concurrent_limit) });
    message.success('已更新');
    setLimitOpen(false);
    fetchData();
  };

  const openTx = async (row: UserCreditRow) => {
    setTxTarget(row);
    setTxOpen(true);
    setTxLoading(true);
    try {
      const result: any = await userCreditApi.transactions(row.user_id, { page: 1, page_size: 100 });
      setTxList(result?.list || []);
    } finally { setTxLoading(false); }
  };

  const columns = [
    {
      title: '用户', key: 'user', ellipsis: true,
      render: (_: any, r: UserCreditRow) => (
        <div>
          <div>{r.name || '(未命名)'}</div>
          <div style={{ fontSize: 11, color: '#888' }}>{r.email || r.user_id.slice(0, 8) + '…'}</div>
        </div>
      ),
    },
    {
      title: '类型', dataIndex: 'user_type', key: 'user_type', width: 100,
      render: (v: string) => {
        const meta = USER_TYPE_TAG[v] || { color: 'default', label: v || '-' };
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: '余额', dataIndex: 'balance', key: 'balance', width: 120,
      render: (v: number) => <span style={{ fontWeight: 600, color: v > 0 ? '#1677ff' : '#aaa' }}>{v}</span>,
    },
    { title: '累计充值', dataIndex: 'total_topup', key: 'total_topup', width: 100 },
    { title: '累计消费', dataIndex: 'total_consumed', key: 'total_consumed', width: 100 },
    {
      title: '并发上限', dataIndex: 'concurrent_limit', key: 'concurrent_limit', width: 90,
      render: (v: number) => v === 0 ? <Tag>不限</Tag> : <span>{v}</span>,
    },
    {
      title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 160,
      render: (v: string) => v ? v.replace('T', ' ').slice(0, 19) : '-',
    },
    {
      title: '操作', key: 'action', width: 240,
      render: (_: any, r: UserCreditRow) => (
        <Space>
          <Button size="small" type="primary" icon={<DollarOutlined />} onClick={() => openAdjust(r)}>调余额</Button>
          <Button size="small" icon={<EditOutlined />} onClick={() => openLimit(r)}>并发</Button>
          <Button size="small" type="link" icon={<HistoryOutlined />} onClick={() => openTx(r)}>流水</Button>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>用户额度</h2>
        <Space>
          <Input
            allowClear placeholder="按邮箱/姓名搜索" style={{ width: 220 }}
            prefix={<SearchOutlined />} value={keyword}
            onChange={e => setKeyword(e.target.value)}
            onPressEnter={() => { setPage(1); fetchData(); }}
          />
          <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
        </Space>
      </div>

      <Table
        columns={columns} dataSource={data} rowKey={r => `${r.user_type || 'user'}:${r.user_id}`} loading={loading}
        pagination={{
          current: page, pageSize, total, showSizeChanger: true,
          showTotal: t => `共 ${t} 个用户`,
          onChange: (p, ps) => { setPage(p); setPageSize(ps); },
        }}
      />

      {/* 调余额 */}
      <Modal
        title={`调整余额 — ${adjustTarget?.email || ''}`}
        open={adjustOpen} onOk={submitAdjust} onCancel={() => setAdjustOpen(false)}
        destroyOnClose
      >
        {adjustTarget && (
          <Card style={{ marginBottom: 12 }}>
            <Statistic title="当前余额" value={adjustTarget.balance} />
          </Card>
        )}
        <Form form={adjustForm} layout="vertical" preserve={false}>
          <Form.Item
            name="amount" label="调整数量（正数=充值；负数=扣减）"
            rules={[{ required: true, message: '请输入数量' }]}
            extra="例：100 表示加 100 点；-50 表示扣 50 点"
          >
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="note" label="备注（建议必填）">
            <Input.TextArea rows={2} maxLength={256} placeholder="如：补偿订单 #1234" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 并发上限 */}
      <Modal
        title={`并发上限 — ${limitTarget?.email || ''}`}
        open={limitOpen} onOk={submitLimit} onCancel={() => setLimitOpen(false)}
        destroyOnClose
      >
        <Form form={limitForm} layout="vertical" preserve={false}>
          <Form.Item
            name="concurrent_limit" label="同时跑中任务数上限"
            rules={[{ required: true }]}
            extra="0 表示不限制；建议默认 1-3"
          >
            <InputNumber min={0} max={100} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      {/* 流水 */}
      <Drawer
        title={`流水 — ${txTarget?.email || ''}`}
        open={txOpen} onClose={() => setTxOpen(false)}
        width={720} destroyOnClose
      >
        {txTarget && (
          <Card style={{ marginBottom: 16 }}>
            <Space size="large">
              <Statistic title="当前余额" value={txTarget.balance} />
              <Statistic title="累计充值" value={txTarget.total_topup} />
              <Statistic title="累计消费" value={txTarget.total_consumed} />
            </Space>
          </Card>
        )}
        <Table
          loading={txLoading}
          dataSource={txList} rowKey="id" pagination={false} size="small"
          columns={[
            {
              title: '时间', dataIndex: 'created_at', width: 160,
              render: (v: string) => v.replace('T', ' ').slice(0, 19),
            },
            {
              title: '类型', dataIndex: 'type', width: 80,
              render: (v: string) => {
                const meta = TYPE_TAG[v] || { color: 'default', label: v };
                return <Tag color={meta.color}>{meta.label}</Tag>;
              },
            },
            {
              title: '金额', dataIndex: 'amount', width: 100,
              render: (v: number) => (
                <span style={{ color: v > 0 ? '#52c41a' : '#ff4d4f', fontWeight: 600 }}>
                  {v > 0 ? `+${v}` : v}
                </span>
              ),
            },
            { title: '余额', dataIndex: 'balance_after', width: 80 },
            { title: 'task / 备注', key: 'detail', ellipsis: true,
              render: (_: any, r: TxRow) => r.note || (r.task_id ? <code style={{ fontSize: 10 }}>{r.task_id}</code> : '-') },
          ]}
        />
      </Drawer>
    </div>
  );
};

export default UserCredits;
