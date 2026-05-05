import React, { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider, App as AntdApp, Spin } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';
import { useAuthStore } from './store';

const Login = lazy(() => import('./pages/Login'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const Analytics = lazy(() => import('./pages/Analytics'));
const Apps = lazy(() => import('./pages/Apps'));
const AppDetail = lazy(() => import('./pages/AppDetail'));
const Instructions = lazy(() => import('./pages/Instructions'));
const Licenses = lazy(() => import('./pages/Licenses'));
const Subscriptions = lazy(() => import('./pages/Subscriptions'));
const Devices = lazy(() => import('./pages/Devices'));
const TeamMembers = lazy(() => import('./pages/TeamMembers'));
const Customers = lazy(() => import('./pages/Customers'));
const AuditLogs = lazy(() => import('./pages/AuditLogs'));
const Profile = lazy(() => import('./pages/Profile'));
const Blacklist = lazy(() => import('./pages/Blacklist'));
const DataExport = lazy(() => import('./pages/DataExport'));
const DataBackups = lazy(() => import('./pages/DataBackups'));
const SecureScripts = lazy(() => import('./pages/SecureScripts'));
const ProviderCredentials = lazy(() => import('./pages/ProviderCredentials'));
const PricingRules = lazy(() => import('./pages/PricingRules'));
const UserCredits = lazy(() => import('./pages/UserCredits'));

// 路由守卫组件
const PrivateRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated } = useAuthStore();
  return isAuthenticated ? <>{children}</> : <Navigate to="/login" replace />;
};

const RoleRoute: React.FC<{ children: React.ReactNode; roles: string[] }> = ({ children, roles }) => {
  const { user } = useAuthStore();
  return user && roles.includes(user.role) ? <>{children}</> : <Navigate to="/" replace />;
};

const PageFallback = () => (
  <div style={{ minHeight: 240, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
    <Spin />
  </div>
);

const App: React.FC = () => {
  return (
    <ConfigProvider locale={zhCN}>
      <AntdApp>
        <BrowserRouter>
          <Suspense fallback={<PageFallback />}>
            <Routes>
              <Route path="/login" element={<Login />} />
              <Route
                path="/"
                element={
                  <PrivateRoute>
                    <MainLayout />
                  </PrivateRoute>
                }
              >
                <Route index element={<Dashboard />} />
                <Route path="analytics" element={<Analytics />} />
                <Route path="apps" element={<Apps />} />
                <Route path="apps/:id" element={<AppDetail />} />
                <Route path="instructions" element={<RoleRoute roles={['owner', 'admin', 'developer']}><Instructions /></RoleRoute>} />
                <Route path="secure-scripts" element={<RoleRoute roles={['owner', 'admin', 'developer']}><SecureScripts /></RoleRoute>} />
                <Route path="team" element={<RoleRoute roles={['owner', 'admin', 'developer']}><TeamMembers /></RoleRoute>} />
                <Route path="customers" element={<Customers />} />
                <Route path="licenses" element={<Licenses />} />
                <Route path="subscriptions" element={<Subscriptions />} />
                <Route path="devices" element={<Devices />} />
                <Route path="blacklist" element={<Blacklist />} />
                <Route path="audit" element={<RoleRoute roles={['owner', 'admin']}><AuditLogs /></RoleRoute>} />
                <Route path="export" element={<RoleRoute roles={['owner', 'admin', 'developer']}><DataExport /></RoleRoute>} />
                <Route path="backups" element={<RoleRoute roles={['owner', 'admin']}><DataBackups /></RoleRoute>} />
                <Route path="provider-credentials" element={<RoleRoute roles={['owner', 'admin']}><ProviderCredentials /></RoleRoute>} />
                <Route path="pricing-rules" element={<RoleRoute roles={['owner', 'admin']}><PricingRules /></RoleRoute>} />
                <Route path="user-credits" element={<RoleRoute roles={['owner', 'admin']}><UserCredits /></RoleRoute>} />
                <Route path="profile" element={<Profile />} />
              </Route>
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Suspense>
        </BrowserRouter>
      </AntdApp>
    </ConfigProvider>
  );
};

export default App;
