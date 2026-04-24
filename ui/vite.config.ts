import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import vueDevTools from 'vite-plugin-vue-devtools'
import tailwindcss from '@tailwindcss/vite'
import AutoImport from 'unplugin-auto-import/vite'
import { NaiveUiResolver } from 'unplugin-vue-components/resolvers'
import Components from 'unplugin-vue-components/vite'

const DEFAULT_PROXY_TARGET = 'http://127.0.0.1:9005'
const apiProxyTarget = (
  process.env.VITE_API_PROXY_TARGET ||
  process.env.DEV_PROXY_SERVER ||
  process.env.VITE_API_BASE_URL ||
  DEFAULT_PROXY_TARGET
).trim()

// https://vite.dev/config/
export default defineConfig({
  base: './',
  plugins: [
    vue(),
    vueJsx(),
    tailwindcss(),
    vueDevTools(),
    AutoImport({
      imports: [
        // 'vue', // 感觉vue自动引入有点乱，还是手动吧
        {
          'naive-ui': [
            'useDialog',
            'useMessage',
            'useNotification',
            'useLoadingBar'
          ]
        }
      ]
    }),
    Components({
      resolvers: [NaiveUiResolver()]
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
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
})
