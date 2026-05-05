import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Card, Row, Col, Select, DatePicker, Statistic, Spin } from 'antd';
import {
  KeyOutlined,
  DesktopOutlined,
  UserOutlined,
  AppstoreOutlined,
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
  const [dashboardData, setDashboardData] = useState<any>(null);
  const [licenseTrend, setLicenseTrend] = useState<any[]>([]);
  const [deviceTrend, setDeviceTrend] = useState<any[]>([]);
  const [licenseTypeData, setLicenseTypeData] = useState<any[]>([]);
  const [deviceOSData, setDeviceOSData] = useState<any[]>([]);
  const selectedAppInfo = selectedApp ? apps.find(app => app.id === selectedApp) : null;

  const totals = {
    licenses: dashboardData?.licenses?.total ?? 0,
    activeLicenses: dashboardData?.licenses?.active ?? 0,
    devices: dashboardData?.devices?.total ?? 0,
    activeDevices: dashboardData?.devices?.active ?? 0,
    customers: dashboardData?.customers?.total ?? 0,
    todayCustomers: dashboardData?.customers?.today_new ?? 0,
    apps: selectedApp ? 1 : (dashboardData?.applications?.total ?? apps.length),
    activeApps: selectedApp ? (selectedAppInfo?.status === 'active' ? 1 : 0) : apps.filter(a => a.status === 'active').length,
  };

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

  const fetchDashboard = useCallback(async () => {
    try {
      const result: any = selectedApp ? await statsApi.appStats(selectedApp) : await statsApi.dashboard();
      setDashboardData(result);
    } catch (error) {
      console.error(error);
      setDashboardData(null);
    }
  }, [selectedApp]);

  useEffect(() => {
    fetchDashboard();
  }, [fetchDashboard]);

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

  if (pageLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%', minHeight: 300 }}>
        <Spin size="large" tip="加载中..." />
      </div>
    );
  }

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>数据分析</h2>
        <div style={{ display: 'flex', gap: 16 }}>
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

      {/* 概览统计 */}
      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={6}>
          <Card>
            <Statistic
              title="总授权数"
              value={totals.licenses}
              prefix={<KeyOutlined />}
              valueStyle={{ color: '#1890ff' }}
            />
            <div style={{ marginTop: 8, fontSize: 12, color: '#666' }}>
              活跃: {totals.activeLicenses}
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="总设备数"
              value={totals.devices}
              prefix={<DesktopOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
            <div style={{ marginTop: 8, fontSize: 12, color: '#666' }}>
              活跃: {totals.activeDevices}
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="总用户数"
              value={totals.customers}
              prefix={<UserOutlined />}
              valueStyle={{ color: '#722ed1' }}
            />
            <div style={{ marginTop: 8, fontSize: 12, color: '#666' }}>
              今日新增: {totals.todayCustomers}
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="应用数量"
              value={totals.apps}
              prefix={<AppstoreOutlined />}
              valueStyle={{ color: '#fa8c16' }}
            />
            <div style={{ marginTop: 8, fontSize: 12, color: '#666' }}>
              活跃: {totals.activeApps}
            </div>
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
