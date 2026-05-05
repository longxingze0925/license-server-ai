import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Card, Row, Col, Select, DatePicker, Statistic, Spin } from 'antd';
import {
  BarChartOutlined,
  KeyOutlined,
  DesktopOutlined,
  PieChartOutlined,
} from '@ant-design/icons';
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { statsApi, appApi } from '../api';
import dayjs from 'dayjs';

const { RangePicker } = DatePicker;
const CHART_COLORS = ['#1677ff', '#52c41a', '#faad14', '#f5222d', '#722ed1', '#13c2c2', '#eb2f96'];
const DEFAULT_SERIES = 'count';

const toNumber = (value: any) => {
  const n = Number(value ?? 0);
  return Number.isFinite(n) ? n : 0;
};

const normalizeValueRows = (rows: any[], valueKey = 'count') =>
  rows.map(row => ({
    ...row,
    [valueKey]: toNumber(row?.[valueKey]),
  }));

const pivotSeriesData = (rows: any[], xKey: string, seriesKey: string, valueKey: string) => {
  const xOrder: string[] = [];
  const seriesOrder: string[] = [];
  const byX = new Map<string, Record<string, string | number>>();

  rows.forEach(row => {
    const xValue = String(row?.[xKey] ?? '');
    if (!xValue) {
      return;
    }

    const rawSeries = row?.[seriesKey];
    const seriesName = rawSeries === undefined || rawSeries === null || rawSeries === '' ? DEFAULT_SERIES : String(rawSeries);
    if (!byX.has(xValue)) {
      byX.set(xValue, { [xKey]: xValue });
      xOrder.push(xValue);
    }
    if (!seriesOrder.includes(seriesName)) {
      seriesOrder.push(seriesName);
    }
    byX.get(xValue)![seriesName] = toNumber(row?.[valueKey]);
  });

  return {
    data: xOrder.map(xValue => byX.get(xValue)!),
    series: seriesOrder,
  };
};

const EmptyChart = () => (
  <div style={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#999' }}>暂无数据</div>
);

const Analytics: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [pageLoading, setPageLoading] = useState(true);
  const [apps, setApps] = useState<any[]>([]);
  const [selectedApp, setSelectedApp] = useState<string>('');
  const [dateRange, setDateRange] = useState<[dayjs.Dayjs, dayjs.Dayjs]>([
    dayjs().subtract(30, 'day'),
    dayjs(),
  ]);
  const [licenseTrend, setLicenseTrend] = useState<any[]>([]);
  const [deviceTrend, setDeviceTrend] = useState<any[]>([]);
  const [licenseTypeData, setLicenseTypeData] = useState<any[]>([]);
  const [deviceOSData, setDeviceOSData] = useState<any[]>([]);

  useEffect(() => {
    fetchApps();
  }, []);

  const fetchApps = async () => {
    try {
      const result: any = await appApi.list();
      const appList = Array.isArray(result) ? result : (result?.list || []);
      setApps(appList);
    } catch (error) {
      console.error(error);
      setApps([]);
    } finally {
      setPageLoading(false);
    }
  };

  const fetchTrendData = useCallback(async () => {
    setLoading(true);
    try {
      const params: any = {
        start_date: dateRange[0].format('YYYY-MM-DD'),
        end_date: dateRange[1].format('YYYY-MM-DD'),
      };
      if (selectedApp) {
        params.app_id = selectedApp;
      }

      const [licenseResult, deviceResult, typeResult, osResult]: any = await Promise.all([
        statsApi.licenseTrend(params).catch(() => null),
        statsApi.deviceTrend(params).catch(() => null),
        statsApi.licenseType(params).catch(() => null),
        statsApi.deviceOS(params).catch(() => null),
      ]);

      const getList = (r: any) => r?.list ?? (Array.isArray(r) ? r : []);
      setLicenseTrend(getList(licenseResult));
      setDeviceTrend(getList(deviceResult));
      setLicenseTypeData(getList(typeResult));
      setDeviceOSData(getList(osResult));
    } catch (error) {
      console.error(error);
    } finally {
      setLoading(false);
    }
  }, [dateRange, selectedApp]);

  useEffect(() => {
    fetchTrendData();
  }, [fetchTrendData]);

  const licenseTrendChart = useMemo(
    () => pivotSeriesData(licenseTrend, 'date', 'type', 'count'),
    [licenseTrend],
  );
  const deviceTrendChart = useMemo(() => normalizeValueRows(deviceTrend), [deviceTrend]);
  const licenseTypeChart = useMemo(() => normalizeValueRows(licenseTypeData), [licenseTypeData]);
  const deviceOSChart = useMemo(() => normalizeValueRows(deviceOSData), [deviceOSData]);
  const summary = useMemo(() => ({
    licenseEvents: licenseTrend.reduce((sum, row) => sum + toNumber(row?.count), 0),
    deviceEvents: deviceTrend.reduce((sum, row) => sum + toNumber(row?.count), 0),
    licenseTypes: licenseTypeChart.length,
    osTypes: deviceOSChart.length,
  }), [licenseTrend, deviceTrend, licenseTypeChart, deviceOSChart]);

  if (pageLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%', minHeight: 300 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
        <h2 style={{ margin: 0 }}>报表分析</h2>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
          <Select
            style={{ width: 200 }}
            placeholder="全部应用"
            allowClear
            value={selectedApp || undefined}
            onChange={setSelectedApp}
            options={apps.map(app => ({ label: app.name, value: app.id }))}
          />
          <RangePicker
            value={dateRange}
            allowClear={false}
            onChange={(dates) => dates && setDateRange(dates as [dayjs.Dayjs, dayjs.Dayjs])}
            presets={[
              { label: '最近7天', value: [dayjs().subtract(7, 'day'), dayjs()] },
              { label: '最近30天', value: [dayjs().subtract(30, 'day'), dayjs()] },
              { label: '最近90天', value: [dayjs().subtract(90, 'day'), dayjs()] },
            ]}
          />
        </div>
      </div>

      {/* 当前筛选范围摘要 */}
      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="授权趋势合计"
              value={summary.licenseEvents}
              prefix={<KeyOutlined />}
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="设备趋势合计"
              value={summary.deviceEvents}
              prefix={<DesktopOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="授权类型数"
              value={summary.licenseTypes}
              prefix={<PieChartOutlined />}
              valueStyle={{ color: '#722ed1' }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="操作系统数"
              value={summary.osTypes}
              prefix={<BarChartOutlined />}
              valueStyle={{ color: '#fa8c16' }}
            />
          </Card>
        </Col>
      </Row>

      <Spin spinning={loading}>
        {/* 趋势图表 */}
        <Row gutter={16} style={{ marginBottom: 24 }}>
          <Col span={12}>
            <Card title="授权趋势" size="small">
              {licenseTrend.length > 0 ? (
                <ResponsiveContainer width="100%" height={300}>
                  <LineChart data={licenseTrendChart.data}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="date" />
                    <YAxis allowDecimals={false} />
                    <Tooltip />
                    {licenseTrendChart.series.length > 1 && <Legend />}
                    {licenseTrendChart.series.map((series, index) => (
                      <Line
                        key={series}
                        type="monotone"
                        dataKey={series}
                        stroke={CHART_COLORS[index % CHART_COLORS.length]}
                        strokeWidth={2}
                        dot={false}
                      />
                    ))}
                  </LineChart>
                </ResponsiveContainer>
              ) : (
                <EmptyChart />
              )}
            </Card>
          </Col>
          <Col span={12}>
            <Card title="设备趋势" size="small">
              {deviceTrend.length > 0 ? (
                <ResponsiveContainer width="100%" height={300}>
                  <AreaChart data={deviceTrendChart}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="date" />
                    <YAxis allowDecimals={false} />
                    <Tooltip />
                    <Area type="monotone" dataKey="count" stroke="#1677ff" fill="#91caff" fillOpacity={0.45} />
                  </AreaChart>
                </ResponsiveContainer>
              ) : (
                <EmptyChart />
              )}
            </Card>
          </Col>
        </Row>

        {/* 分布图表 */}
        <Row gutter={16}>
          <Col span={12}>
            <Card title="授权类型分布" size="small">
              {licenseTypeData.length > 0 ? (
                <ResponsiveContainer width="100%" height={300}>
                  <PieChart>
                    <Pie
                      data={licenseTypeChart}
                      dataKey="count"
                      nameKey="type"
                      cx="50%"
                      cy="50%"
                      outerRadius={90}
                      label={({ payload }: any) => `${payload?.type}: ${payload?.count}`}
                    >
                      {licenseTypeChart.map((_, index) => (
                        <Cell key={`license-type-${index}`} fill={CHART_COLORS[index % CHART_COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip />
                  </PieChart>
                </ResponsiveContainer>
              ) : (
                <EmptyChart />
              )}
            </Card>
          </Col>
          <Col span={12}>
            <Card title="设备操作系统分布" size="small">
              {deviceOSData.length > 0 ? (
                <ResponsiveContainer width="100%" height={300}>
                  <BarChart data={deviceOSChart}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="os_type" />
                    <YAxis allowDecimals={false} />
                    <Tooltip />
                    <Bar dataKey="count" fill="#1677ff" />
                  </BarChart>
                </ResponsiveContainer>
              ) : (
                <EmptyChart />
              )}
            </Card>
          </Col>
        </Row>
      </Spin>
    </div>
  );
};

export default Analytics;
