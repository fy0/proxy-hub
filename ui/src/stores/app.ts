import { defineStore } from 'pinia';
import { ref } from 'vue';
import type { VersionResponseBody } from '@/api/generated';
import { getSystemVersion } from '@/api/generated';

export interface AppInfo {
  name: string;
  version: string;
  channel: string;
}

const defaultAppInfo: AppInfo = {
  name: 'ProxyHub',
  version: '0.1.0-alpha',
  channel: 'dev',
};

export const useAppStore = defineStore('app', () => {
  const appInfo = ref<AppInfo>({ ...defaultAppInfo });
  const appInfoLoaded = ref(false);

  function setAppInfo(info: VersionResponseBody): void {
    appInfo.value = {
      name: info.name || defaultAppInfo.name,
      version: info.version || defaultAppInfo.version,
      channel: info.channel || defaultAppInfo.channel,
    };
    appInfoLoaded.value = true;
  }

  async function loadAppInfo(): Promise<void> {
    if (appInfoLoaded.value) {
      return;
    }

    const { data } = await getSystemVersion({ throwOnError: true });
    setAppInfo(data);
  }

  return {
    appInfo,
    appInfoLoaded,
    loadAppInfo,
    setAppInfo,
  };
});
