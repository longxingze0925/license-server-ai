import axios, { type AxiosProgressEvent } from 'axios';
import { message } from 'antd';

// 支持环境变量配置 API 地址
// 开发环境默认走 Vite proxy，生产环境默认走 nginx proxy。
const getBaseURL = () => {
  // 优先使用环境变量
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL;
  }
  // 开发环境默认使用相对路径，避免绕过 Vite proxy 触发 CORS。
  if (import.meta.env.DEV) {
    return '/api';
  }
  // 生产环境默认使用相对路径
  return '/api';
};

const request = axios.create({
  baseURL: getBaseURL(),
  timeout: 30000,
});

export const UPLOAD_TIMEOUT = 10 * 60 * 1000;

let csrfToken: string | null = null;
let csrfTokenPromise: Promise<string | null> | null = null;
let csrfTokenAuth: string | null = null;

export const clearCSRFToken = () => {
  csrfToken = null;
  csrfTokenPromise = null;
  csrfTokenAuth = null;
};

const needsCSRFToken = (method?: string) => {
  const normalized = (method || 'get').toLowerCase();
  return !['get', 'head', 'options'].includes(normalized);
};

const fetchCSRFToken = async (authToken: string) => {
  if (csrfToken && csrfTokenAuth === authToken) {
    return csrfToken;
  }
  if (csrfTokenAuth !== authToken) {
    clearCSRFToken();
  }
  if (!csrfTokenPromise) {
    csrfTokenPromise = axios
      .get(`${getBaseURL()}/auth/csrf-token`, {
        headers: { Authorization: `Bearer ${authToken}` },
      })
      .then((resp) => {
        csrfToken = resp.data?.data?.csrf_token || null;
        csrfTokenAuth = authToken;
        return csrfToken;
      })
      .finally(() => {
        csrfTokenPromise = null;
      });
  }
  return csrfTokenPromise;
};

export const buildUploadConfig = (onProgress?: (percent: number) => void) => ({
  timeout: UPLOAD_TIMEOUT,
  onUploadProgress: (event: AxiosProgressEvent) => {
    if (!onProgress || !event.total) {
      return;
    }
    onProgress(Math.round((event.loaded / event.total) * 100));
  },
});

// 请求拦截器
request.interceptors.request.use(
  async (config) => {
    const token = localStorage.getItem('token');
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
      if (needsCSRFToken(config.method)) {
        const csrf = await fetchCSRFToken(token);
        if (csrf) {
          config.headers['X-CSRF-Token'] = csrf;
        }
      }
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// 响应拦截器
request.interceptors.response.use(
  (response) => {
    if (response.config.responseType === 'blob') {
      return response;
    }

    const { code, message: msg, data } = response.data;
    if (code === 0) {
      return data;
    }
    message.error(msg || '请求失败');
    return Promise.reject(new Error(msg));
  },
  async (error) => {
    const originalConfig = error.config;
    if (
      error.response?.status === 403 &&
      String(error.response?.data?.message || '').includes('CSRF') &&
      originalConfig &&
      needsCSRFToken(originalConfig.method) &&
      !originalConfig.__csrfRetried
    ) {
      clearCSRFToken();
      const token = localStorage.getItem('token');
      if (token) {
        originalConfig.__csrfRetried = true;
        const csrf = await fetchCSRFToken(token);
        if (csrf) {
          originalConfig.headers = originalConfig.headers || {};
          originalConfig.headers['X-CSRF-Token'] = csrf;
          originalConfig.headers.Authorization = `Bearer ${token}`;
          return request(originalConfig);
        }
      }
    }
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      clearCSRFToken();
      window.location.href = '/login';
    }
    if (error.response?.status === 403 && String(error.response?.data?.message || '').includes('CSRF')) {
      clearCSRFToken();
    }
    message.error(error.response?.data?.message || '网络错误');
    return Promise.reject(error);
  }
);

export default request;
