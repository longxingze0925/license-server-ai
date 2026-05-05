import React, { useState } from 'react';
import { Card, Button, DatePicker, Form, message, Row, Col, Divider, Space, Tag } from 'antd';
import { DownloadOutlined, FileExcelOutlined, FileTextOutlined } from '@ant-design/icons';
import { exportApi } from '../api';
import { useAuthStore } from '../store';
import dayjs from 'dayjs';
import type { AxiosResponse } from 'axios';

const { RangePicker } = DatePicker;

type ExportType = 'licenses' | 'devices' | 'users' | 'audit';
type ExportParams = Record<string, string>;
type ExportItem = {
  key: ExportType;
  title: string;
  description: string;
  icon: React.ReactNode;
  fields: string[];
};

const DataExport: React.FC = () => {
  const { user } = useAuthStore();
  const [loading, setLoading] = useState<string | null>(null);
  const [form] = Form.useForm();
  const canExportAuditLogs = user?.role === 'owner' || user?.role === 'admin';

  const handleExport = async (type: ExportType) => {
    try {
      const values = await form.validateFields();
      setLoading(type);

      const params: ExportParams = {};

      if (values.dateRange) {
        params.start_date = values.dateRange[0].format('YYYY-MM-DD');
        params.end_date = values.dateRange[1].format('YYYY-MM-DD');
      }

      let response: AxiosResponse<Blob>;
      switch (type) {
        case 'licenses':
          response = await exportApi.licenses(params);
          break;
        case 'devices':
          response = await exportApi.devices(params);
          break;
        case 'users':
          response = await exportApi.users(params);
          break;
        case 'audit':
          response = await exportApi.auditLogs(params);
          break;
        default:
          message.error('未知导出类型');
          return;
      }

      saveBlob(response.data, getDownloadFilename(response, `${type}_export_${dayjs().format('YYYYMMDD_HHmmss')}.csv`));
      message.success('导出任务已开始，请等待下载');
    } catch (error) {
      console.error(error);
      message.error('导出失败');
    } finally {
      setLoading(null);
    }
  };

  const allExportItems: ExportItem[] = [
    {
      key: 'licenses',
      title: '授权数据',
      description: '导出所有授权许可证数据，包括授权码、状态、有效期、绑定设备等信息',
      icon: <FileExcelOutlined style={{ fontSize: 32, color: '#52c41a' }} />,
      fields: ['授权码', '应用', '类型', '状态', '有效期', '设备数', '创建时间'],
    },
    {
      key: 'devices',
      title: '设备数据',
      description: '导出所有注册设备数据，包括机器码、操作系统、最后活跃时间等信息',
      icon: <FileExcelOutlined style={{ fontSize: 32, color: '#1890ff' }} />,
      fields: ['设备ID', '机器码', '操作系统', '状态', '最后心跳', '绑定授权'],
    },
    {
      key: 'users',
      title: '客户数据',
      description: '导出客户账户数据，包括邮箱、姓名、公司、状态、注册时间等信息',
      icon: <FileExcelOutlined style={{ fontSize: 32, color: '#722ed1' }} />,
      fields: ['客户ID', '邮箱', '姓名', '公司', '状态', '注册时间'],
    },
    {
      key: 'audit',
      title: '审计日志',
      description: '导出系统操作审计日志，包括操作类型、操作人、时间、详情等信息',
      icon: <FileTextOutlined style={{ fontSize: 32, color: '#fa8c16' }} />,
      fields: ['日志ID', '操作类型', '操作人', '目标', 'IP地址', '操作时间', '详情'],
    },
  ];
  const exportItems = allExportItems.filter(item => item.key !== 'audit' || canExportAuditLogs);

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>数据导出</h2>
        <p style={{ color: '#666', marginTop: 8 }}>导出系统数据用于备份、分析或迁移</p>
      </div>

      <Card title="导出设置" style={{ marginBottom: 24 }}>
        <Form form={form} layout="inline">
          <Form.Item name="dateRange" label="时间范围">
            <RangePicker
              placeholder={['开始日期', '结束日期']}
              presets={[
                { label: '最近7天', value: [dayjs().subtract(7, 'day'), dayjs()] },
                { label: '最近30天', value: [dayjs().subtract(30, 'day'), dayjs()] },
                { label: '最近90天', value: [dayjs().subtract(90, 'day'), dayjs()] },
                { label: '今年', value: [dayjs().startOf('year'), dayjs()] },
              ]}
            />
          </Form.Item>
        </Form>
      </Card>

      <Row gutter={[16, 16]}>
        {exportItems.map(item => (
          <Col span={12} key={item.key}>
            <Card
              hoverable
              actions={[
                <Button
                  type="primary"
                  icon={<DownloadOutlined />}
                  loading={loading === item.key}
                  onClick={() => handleExport(item.key)}
                >
                  导出
                </Button>
              ]}
            >
              <Card.Meta
                avatar={item.icon}
                title={item.title}
                description={
                  <div>
                    <p style={{ marginBottom: 12 }}>{item.description}</p>
                    <Divider style={{ margin: '12px 0' }} />
                    <div>
                      <strong>包含字段：</strong>
                      <div style={{ marginTop: 8 }}>
                        <Space wrap>
                          {item.fields.map(field => (
                            <Tag key={field}>{field}</Tag>
                          ))}
                        </Space>
                      </div>
                    </div>
                  </div>
                }
              />
            </Card>
          </Col>
        ))}
      </Row>

      <Card title="导出说明" style={{ marginTop: 24 }}>
        <ul style={{ paddingLeft: 20, margin: 0 }}>
          <li>CSV 格式：通用格式，可用 Excel 或其他表格软件打开</li>
          <li>如不选择时间范围，将导出全部数据</li>
          <li>大数据量导出可能需要较长时间，请耐心等待</li>
          <li>导出的数据包含敏感信息，请妥善保管</li>
        </ul>
      </Card>
    </div>
  );
};

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function getDownloadFilename(response: AxiosResponse<Blob>, fallback: string) {
  const disposition = response.headers['content-disposition'];
  if (typeof disposition !== 'string') {
    return fallback;
  }

  const utf8Name = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (utf8Name) {
    return decodeURIComponent(utf8Name);
  }

  return disposition.match(/filename="?([^"]+)"?/i)?.[1] || fallback;
}

export default DataExport;
