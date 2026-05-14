import { fileURLToPath, URL } from 'node:url';

import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import vueJsx from '@vitejs/plugin-vue-jsx';
import vueDevTools from 'vite-plugin-vue-devtools';
import tailwindcss from '@tailwindcss/vite';

const DEFAULT_PROXY_TARGET = 'http://127.0.0.1:3020';
const apiProxyTarget = (
  process.env.VITE_API_PROXY_TARGET ||
  process.env.DEV_PROXY_SERVER ||
  process.env.VITE_API_BASE_URL ||
  DEFAULT_PROXY_TARGET
).trim();

// https://vite.dev/config/
export default defineConfig({
  base: './',
  plugins: [vue(), vueJsx(), tailwindcss(), vueDevTools()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '0.0.0.0',
    proxy: {
      '^/api': {
        target: apiProxyTarget,
        changeOrigin: true,
        xfwd: true,
      },
      '^/openapi.json$': {
        target: apiProxyTarget,
        changeOrigin: true,
        xfwd: true,
      },
      '^/docs(?:/.*)?$': {
        target: apiProxyTarget,
        changeOrigin: true,
        xfwd: true,
      },
      '^/schemas(?:/.*)?$': {
        target: apiProxyTarget,
        changeOrigin: true,
        xfwd: true,
      },
    },
  },
});
