import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          const normalizedId = id.replace(/\\/g, '/');
          if (!normalizedId.includes('/node_modules/')) {
            return;
          }

          if (normalizedId.includes('/react/') || normalizedId.includes('/react-dom/') || normalizedId.includes('/react-router-dom/')) {
            return 'vendor-react';
          }
          if (
            normalizedId.includes('/recharts/') ||
            normalizedId.includes('/d3-')
          ) {
            return 'vendor-charts';
          }
          if (normalizedId.includes('/axios/') || normalizedId.includes('/zustand/') || normalizedId.includes('/dayjs/')) {
            return 'vendor-utils';
          }
          if (
            normalizedId.includes('/rc-picker/') ||
            normalizedId.includes('/antd/es/date-picker/') ||
            normalizedId.includes('/antd/es/calendar/')
          ) {
            return 'vendor-antd-date';
          }
          if (
            normalizedId.includes('/rc-table/') ||
            normalizedId.includes('/rc-pagination/') ||
            normalizedId.includes('/rc-virtual-list/') ||
            normalizedId.includes('/antd/es/table/') ||
            normalizedId.includes('/antd/es/pagination/')
          ) {
            return 'vendor-antd-table';
          }
          if (
            normalizedId.includes('/rc-select/') ||
            normalizedId.includes('/rc-tree/') ||
            normalizedId.includes('/rc-cascader/') ||
            normalizedId.includes('/antd/es/select/') ||
            normalizedId.includes('/antd/es/tree-select/')
          ) {
            return 'vendor-antd-select';
          }
          if (
            normalizedId.includes('/antd/') ||
            normalizedId.includes('/@ant-design/') ||
            normalizedId.includes('/@rc-component/') ||
            normalizedId.includes('/rc-')
          ) {
            return 'vendor-antd-core';
          }
        },
      },
    },
    chunkSizeWarningLimit: 1000,
  },
  server: {
    host: '127.0.0.1',
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
      },
    },
  },
})
