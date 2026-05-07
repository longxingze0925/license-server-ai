import request, { buildUploadConfig } from './request';
import type { AxiosResponse } from 'axios';

// 认证
export const authApi = {
  login: (data: { email: string; password: string }) => request.post('/auth/login', data),
  register: (data: { email: string; password: string; name: string; tenant_name?: string }) => request.post('/auth/register', data),
  getProfile: () => request.get('/auth/profile'),
  changePassword: (data: { old_password: string; new_password: string }) => request.put('/auth/password', data),
  acceptInvite: (data: { token: string; password: string; name: string }) => request.post('/auth/accept-invite', data),
};

// 租户管理
export const tenantApi = {
  get: () => request.get('/tenant'),
  update: (data: { name?: string; logo?: string; email?: string; phone?: string; website?: string; address?: string }) => request.put('/tenant', data),
  delete: () => request.delete('/tenant'),
};

// 团队成员管理
export const teamApi = {
  list: (params?: { page?: number; page_size?: number; status?: string; role?: string }) => request.get('/team/members', { params }),
  get: (id: string) => request.get(`/team/members/${id}`),
  create: (data: { email: string; password: string; name: string; role: string; phone?: string }) => request.post('/team/members', data),
  update: (id: string, data: { email?: string; name?: string; phone?: string }) => request.put(`/team/members/${id}`, data),
  resetPassword: (id: string, data: { password: string }) => request.post(`/team/members/${id}/reset-password`, data),
  updateRole: (id: string, data: { role: string }) => request.put(`/team/members/${id}/role`, data),
  remove: (id: string) => request.delete(`/team/members/${id}`),
};

// 客户管理
export const customerApi = {
  list: (params?: { page?: number; page_size?: number; status?: string; keyword?: string; owner_id?: string }) => request.get('/admin/customers', { params }),
  get: (id: string) => request.get(`/admin/customers/${id}`),
  create: (data: { email: string; password?: string; name?: string; phone?: string; company?: string; remark?: string; metadata?: string }) => request.post('/admin/customers', data),
  update: (id: string, data: { name?: string; phone?: string; company?: string; remark?: string; metadata?: string; status?: string }) => request.put(`/admin/customers/${id}`, data),
  delete: (id: string) => request.delete(`/admin/customers/${id}`),
  disable: (id: string) => request.post(`/admin/customers/${id}/disable`),
  enable: (id: string) => request.post(`/admin/customers/${id}/enable`),
  resetPassword: (id: string, data: { password: string }) => request.post(`/admin/customers/${id}/reset-password`, data),
  getLicenses: (id: string) => request.get(`/admin/customers/${id}/licenses`),
  getSubscriptions: (id: string) => request.get(`/admin/customers/${id}/subscriptions`),
  getDevices: (id: string) => request.get(`/admin/customers/${id}/devices`),
};

// 应用管理
export const appApi = {
  list: () => request.get('/admin/apps'),
  get: (id: string) => request.get(`/admin/apps/${id}`),
  create: (data: any) => request.post('/admin/apps', data),
  update: (id: string, data: any) => request.put(`/admin/apps/${id}`, data),
  delete: (id: string) => request.delete(`/admin/apps/${id}`),
  regenerateKeys: (id: string) => request.post(`/admin/apps/${id}/regenerate-keys`),
  // 脚本
  getScripts: (appId: string) => request.get(`/admin/apps/${appId}/scripts`),
  uploadScript: (appId: string, formData: FormData, onProgress?: (percent: number) => void) =>
    request.post(`/admin/apps/${appId}/scripts`, formData, buildUploadConfig(onProgress)),
  deleteScript: (id: string) => request.delete(`/admin/scripts/${id}`),
  // 版本
  getReleases: (appId: string) => request.get(`/admin/apps/${appId}/releases`),
  uploadRelease: (appId: string, formData: FormData, onProgress?: (percent: number) => void) =>
    request.post(`/admin/apps/${appId}/releases/upload`, formData, buildUploadConfig(onProgress)),
  publishRelease: (id: string) => request.post(`/admin/releases/${id}/publish`),
  deleteRelease: (id: string) => request.delete(`/admin/releases/${id}`),
};

// 授权管理
export const licenseApi = {
  list: (params?: any) => request.get('/admin/licenses', { params }),
  get: (id: string) => request.get(`/admin/licenses/${id}`),
  create: (data: any) => request.post('/admin/licenses', data),
  update: (id: string, data: any) => request.put(`/admin/licenses/${id}`, data),
  delete: (id: string) => request.delete(`/admin/licenses/${id}`),
  renew: (id: string, data: { days: number }) => request.post(`/admin/licenses/${id}/renew`, data),
  revoke: (id: string, data?: { reason?: string }) => request.post(`/admin/licenses/${id}/revoke`, data),
  suspend: (id: string, data?: { reason?: string }) => request.post(`/admin/licenses/${id}/suspend`, data),
  resume: (id: string) => request.post(`/admin/licenses/${id}/resume`),
  resetDevices: (id: string) => request.post(`/admin/licenses/${id}/reset-devices`),
  resetUnbindCount: (id: string) => request.post(`/admin/licenses/${id}/reset-unbind-count`),
};

// 订阅管理（账号密码模式）
export const subscriptionApi = {
  listAccounts: (params?: any) => request.get('/admin/subscriptions/accounts', { params }),
  list: (params?: any) => request.get('/admin/subscriptions', { params }),
  get: (id: string) => request.get(`/admin/subscriptions/${id}`),
  create: (data: any) => request.post('/admin/subscriptions', data),
  update: (id: string, data: any) => request.put(`/admin/subscriptions/${id}`, data),
  delete: (id: string) => request.delete(`/admin/subscriptions/${id}`),
  renew: (id: string, data: { days: number }) => request.post(`/admin/subscriptions/${id}/renew`, data),
  cancel: (id: string) => request.post(`/admin/subscriptions/${id}/cancel`),
  resetUnbindCount: (id: string) => request.post(`/admin/subscriptions/${id}/reset-unbind-count`),
};

// 设备管理
export const deviceApi = {
  list: (params?: any) => request.get('/admin/devices', { params }),
  get: (id: string) => request.get(`/admin/devices/${id}`),
  unbind: (id: string) => request.delete(`/admin/devices/${id}`),
  blacklist: (id: string, data?: { reason?: string }) => request.post(`/admin/devices/${id}/blacklist`, data),
  unblacklist: (id: string) => request.post(`/admin/devices/${id}/unblacklist`),
  getBlacklist: (params?: any) => request.get('/admin/blacklist', { params }),
  removeFromBlacklist: (machineId: string, params?: any) => request.delete(`/admin/blacklist/${machineId}`, { params }),
};

// 脚本管理
export const scriptApi = {
  get: (id: string) => request.get(`/admin/scripts/${id}`),
  update: (id: string, data: any) => request.put(`/admin/scripts/${id}`, data),
  delete: (id: string) => request.delete(`/admin/scripts/${id}`),
};

// 版本管理
export const releaseApi = {
  get: (id: string) => request.get(`/admin/releases/${id}`),
  update: (id: string, data: any) => request.put(`/admin/releases/${id}`, data),
  publish: (id: string) => request.post(`/admin/releases/${id}/publish`),
  deprecate: (id: string) => request.post(`/admin/releases/${id}/deprecate`),
  delete: (id: string) => request.delete(`/admin/releases/${id}`),
};

// 统计
export const statsApi = {
  dashboard: () => request.get('/admin/statistics/dashboard'),
  appStats: (appId: string) => request.get(`/admin/statistics/apps/${appId}`),
  licenseTrend: (params?: any) => request.get('/admin/statistics/license-trend', { params }),
  deviceTrend: (params?: any) => request.get('/admin/statistics/device-trend', { params }),
  licenseType: (params?: any) => request.get('/admin/statistics/license-type', { params }),
  deviceOS: (params?: any) => request.get('/admin/statistics/device-os', { params }),
};


// 审计日志
export const auditApi = {
  list: (params?: any) => request.get('/admin/audit', { params }),
  get: (id: string) => request.get(`/admin/audit/${id}`),
  getStats: (params?: any) => request.get('/admin/audit/stats', { params }),
};

// 数据导出
type ExportParams = Record<string, string | number | boolean | null | undefined>;
const downloadExport = (resource: string, params?: ExportParams) =>
  request.get(`/admin/export/${resource}`, { params, responseType: 'blob' }) as unknown as Promise<AxiosResponse<Blob>>;

export const exportApi = {
  getFormats: () => request.get('/admin/export/formats'),
  licenses: (params?: ExportParams) => downloadExport('licenses', params),
  devices: (params?: ExportParams) => downloadExport('devices', params),
  customers: (params?: ExportParams) => downloadExport('customers', params),
  users: (params?: ExportParams) => downloadExport('customers', params),
  auditLogs: (params?: ExportParams) => downloadExport('audit-logs', params),
};

// 热更新管理
export const hotUpdateApi = {
  list: (appId: string, params?: any) => request.get(`/admin/apps/${appId}/hotupdate`, { params }),
  get: (id: string) => request.get(`/admin/hotupdate/${id}`),
  create: (appId: string, formData: FormData, onProgress?: (percent: number) => void) =>
    request.post(`/admin/apps/${appId}/hotupdate`, formData, buildUploadConfig(onProgress)),
  update: (id: string, data: any) => request.put(`/admin/hotupdate/${id}`, data),
  delete: (id: string) => request.delete(`/admin/hotupdate/${id}`),
  publish: (id: string) => request.post(`/admin/hotupdate/${id}/publish`),
  deprecate: (id: string) => request.post(`/admin/hotupdate/${id}/deprecate`),
  rollback: (id: string) => request.post(`/admin/hotupdate/${id}/rollback`),
  getLogs: (id: string, params?: any) => request.get(`/admin/hotupdate/${id}/logs`, { params }),
};

// 发布异步任务
export const publishTaskApi = {
  createHotUpdateTask: (id: string, action: 'publish' | 'deprecate' | 'rollback') =>
    request.post(`/admin/hotupdate/${id}/tasks`, { action }),
  createReleaseTask: (id: string, action: 'publish' | 'deprecate') =>
    request.post(`/admin/releases/${id}/tasks`, { action }),
  get: (id: string) => request.get(`/admin/tasks/${id}`),
};

// 安全脚本管理
export const secureScriptApi = {
  list: (appId: string, params?: any) => request.get(`/admin/apps/${appId}/secure-scripts`, { params }),
  get: (id: string) => request.get(`/admin/secure-scripts/${id}`),
  create: (appId: string, data: FormData, onProgress?: (percent: number) => void) =>
    request.post(`/admin/apps/${appId}/secure-scripts`, data, buildUploadConfig(onProgress)),
  update: (id: string, data: any) => request.put(`/admin/secure-scripts/${id}`, data),
  delete: (id: string) => request.delete(`/admin/secure-scripts/${id}`),
  updateContent: (id: string, data: FormData, onProgress?: (percent: number) => void) =>
    request.post(`/admin/secure-scripts/${id}/content`, data, buildUploadConfig(onProgress)),
  publish: (id: string) => request.post(`/admin/secure-scripts/${id}/publish`),
  deprecate: (id: string) => request.post(`/admin/secure-scripts/${id}/deprecate`),
  getDeliveries: (id: string, params?: any) => request.get(`/admin/secure-scripts/${id}/deliveries`, { params }),
};

// 实时指令管理
export const instructionApi = {
  list: (params?: any) => request.get('/admin/instructions', { params }),
  get: (id: string) => request.get(`/admin/instructions/${id}`),
  send: (data: any) => request.post('/admin/instructions/send', data),
  getOnlineDevices: (appId: string) => request.get(`/admin/apps/${appId}/online-devices`),
};

// 黑名单管理
export const blacklistApi = {
  list: (params?: any) => request.get('/admin/blacklist', { params }),
  remove: (machineId: string, params?: any) => request.delete(`/admin/blacklist/${machineId}`, { params }),
};

// 计价规则
export const pricingRuleApi = {
  list: (params?: { provider?: string; scope?: string; page?: number; page_size?: number }) =>
    request.get('/admin/pricing/rules', { params }),
  get: (id: number) => request.get(`/admin/pricing/rules/${id}`),
  create: (data: {
    provider: string;
    scope: string;
    match_json?: string;
    credits?: number;
    formula?: string;
    priority?: number;
    enabled?: boolean;
    note?: string;
  }) => request.post('/admin/pricing/rules', data),
  update: (id: number, data: any) => request.put(`/admin/pricing/rules/${id}`, data),
  delete: (id: number) => request.delete(`/admin/pricing/rules/${id}`),
  preview: (data: { provider: string; scope: string; params?: Record<string, any> }) =>
    request.post('/admin/pricing/preview', data),
};

// 用户额度（admin）
export const userCreditApi = {
  list: (params?: { keyword?: string; page?: number; page_size?: number }) =>
    request.get('/admin/credits/users', { params }),
  get: (id: string) => request.get(`/admin/credits/users/${id}`),
  enable: (id: string) => request.post(`/admin/credits/users/${id}`),
  adjust: (id: string, data: { amount: number; note?: string }) =>
    request.post(`/admin/credits/users/${id}/adjust`, data),
  setLimits: (id: string, data: { concurrent_limit: number }) =>
    request.put(`/admin/credits/users/${id}/limits`, data),
  transactions: (id: string, params?: { page?: number; page_size?: number }) =>
    request.get(`/admin/credits/users/${id}/transactions`, { params }),
};

// AI Provider 凭证管理
export const providerCredentialApi = {
  list: (params?: { provider?: string; mode?: string; enabled?: boolean; page?: number; page_size?: number }) =>
    request.get('/admin/proxy/credentials', { params }),
  get: (id: string) => request.get(`/admin/proxy/credentials/${id}`),
  create: (data: {
    provider: string;
    mode: string;
    channel_name: string;
    upstream_base: string;
    api_key: string;
    default_model?: string;
    custom_headers?: string;
    enabled?: boolean;
    is_default?: boolean;
    priority?: number;
    note?: string;
  }) => request.post('/admin/proxy/credentials', data),
  update: (
    id: string,
    data: {
      mode?: string;
      channel_name?: string;
      upstream_base?: string;
      api_key?: string;
      default_model?: string;
      custom_headers?: string;
      enabled?: boolean;
      is_default?: boolean;
      priority?: number;
      note?: string;
    }
  ) => request.put(`/admin/proxy/credentials/${id}`, data),
  delete: (id: string) => request.delete(`/admin/proxy/credentials/${id}`),
  test: (id: string) => request.post(`/admin/proxy/credentials/${id}/test`),
};

// 客户端模型：客户端展示模型与后台真实渠道路由
export const clientModelApi = {
  list: (params?: { include_disabled?: boolean; page?: number; page_size?: number }) =>
    request.get('/admin/client-models', { params }),
  get: (id: string) => request.get(`/admin/client-models/${id}`),
  create: (data: {
    model_key: string;
    display_name: string;
    provider: string;
    scope: string;
    enabled?: boolean;
    sort_order?: number;
    supported_modes?: string[];
    supported_scopes?: string[];
    aspect_ratios?: string[];
    durations?: string[];
    note?: string;
  }) => request.post('/admin/client-models', data),
  update: (id: string, data: any) => request.put(`/admin/client-models/${id}`, data),
  delete: (id: string) => request.delete(`/admin/client-models/${id}`),
  createRoute: (
    id: string,
    data: {
      credential_id: string;
      upstream_model: string;
      enabled?: boolean;
      is_default?: boolean;
      priority?: number;
      sort_order?: number;
      note?: string;
    }
  ) => request.post(`/admin/client-models/${id}/routes`, data),
  updateRoute: (routeId: string, data: any) => request.put(`/admin/client-model-routes/${routeId}`, data),
  deleteRoute: (routeId: string) => request.delete(`/admin/client-model-routes/${routeId}`),
};
