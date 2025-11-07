import { createAlova, globalConfig } from 'alova';
import fetchAdapter from 'alova/fetch';
import VueHook from 'alova/vue';
import { createApis, withConfigType, mountApis } from './createApis';
export { useReq, useInit, FORCE } from './composable';

// 自定义 API 错误类
export class ApiError extends Error {
  public status: number;
  public statusText: string;
  public data: any;

  constructor(status: number, statusText: string, data: any) {
    // 优先使用 data 中的错误信息，避免重复显示
    const message =
      typeof data === 'object' && data?.detail
        ? data.detail
        : typeof data === 'string'
          ? data
          : statusText;
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.statusText = statusText;
    this.data = data;
  }
}

// 基础URL配置 - 根据环境动态设置
export let urlBase =
  import.meta.env.MODE === 'development'
    ? '//' + window.location.hostname + ':' + 9005
    : '//' + window.location.hostname;

// 如果是生产环境，本身链接又带端口，那么带上端口
if (import.meta.env.MODE === 'production' && window.location.port) {
  urlBase += ':' + window.location.port;
}

export const alovaInstance = createAlova({
  baseURL: urlBase,
  requestAdapter: fetchAdapter(),
  statesHook: VueHook,
  beforeRequest: method => {
    // 设置携带 cookies
    method.config.credentials = 'include';

    // 如果本地存储中有token，则添加到请求头中
    const token = localStorage.getItem('token');
    if (token) {
      method.config.headers['Authorization'] = token;
    }
  },
  responded: async (response, method) => {
    // 检查 HTTP 状态码
    if (!response.ok) {
      const responseText = await response.text();
      let data;

      try {
        // 尝试解析 JSON 响应
        data = JSON.parse(responseText);
      } catch {
        // 解析失败时使用原始文本
        data = responseText;
      }

      throw new ApiError(response.status, response.statusText, data);
    }
    // 根据响应类型返回正确的数据，避免将非 JSON 数据强制解析为 JSON
    const responseType = (method as any).config?.responseType;
    if (responseType === 'blob') {
      return response.blob();
    }
    if (responseType === 'arraybuffer') {
      return response.arrayBuffer();
    }
    if (responseType === 'text') {
      return response.text();
    }

    // 未显式设置 responseType，则根据 Content-Type 决定
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) {
      // 某些接口可能错误返回非JSON数据但Content-Type写成了json，做兼容兜底
      const clone = response.clone();
      try {
        return await response.json();
      } catch {
        // 如果不是合法JSON，优先尝试blob，再次兜底为text
        try {
          return await clone.blob();
        } catch {
          return await clone.text();
        }
      }
    }
    if (contentType.includes('application/pdf')) {
      return response.blob();
    }
    return response.text();
  },
});

export const $$userConfigMap = withConfigType({});

const Apis = createApis(alovaInstance, $$userConfigMap);

mountApis(Apis);

export default Apis;
export { Apis };

// @ts-ignore
export * from './globals.d.ts';
