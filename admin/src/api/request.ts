import axios, { type AxiosProgressEvent } from 'axios';
import { message } from 'antd';

// 支持环境变量配置 API 地址
// 开发环境: http://localhost:8081/api
// 生产环境: /api (通过 nginx 代理)
const getBaseURL = () => {
  // 优先使用环境变量
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL;
  }
  // 开发环境默认值
  if (import.meta.env.DEV) {
    return 'http://localhost:8081/api';
  }
  // 生产环境默认使用相对路径
  return '/api';
};

const request = axios.create({
  baseURL: getBaseURL(),
  timeout: 30000,
});

export const UPLOAD_TIMEOUT = 10 * 60 * 1000;

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
  (config) => {
    const token = localStorage.getItem('token');
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
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
    const { code, message: msg, data } = response.data;
    if (code === 0) {
      return data;
    }
    message.error(msg || '请求失败');
    return Promise.reject(new Error(msg));
  },
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      window.location.href = '/login';
    }
    message.error(error.response?.data?.message || '网络错误');
    return Promise.reject(error);
  }
);

export default request;
