import { defineStore } from 'pinia';
import { ref } from 'vue';
import type { CheckUpdateResponseBody, VersionResponseBody } from '@/api/generated';
import { getSystemVersion } from '@/api/generated';
import { client } from '@/api/generated/client.gen';

export interface AppInfo {
  name: string;
  version: string;
  channel: string;
}

export interface AppUpdateInfo extends CheckUpdateResponseBody {
  channel?: string;
  distTag?: string;
  updateCommand?: string;
}

const defaultAppInfo: AppInfo = {
  name: 'ProxyHub',
  version: '1.0.1',
  channel: 'stable',
};

export const useAppStore = defineStore('app', () => {
  const appInfo = ref<AppInfo>({ ...defaultAppInfo });
  const appInfoLoaded = ref(false);
  const updateInfo = ref<AppUpdateInfo | null>(null);
  const updateInfoLoaded = ref(false);

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

  async function loadUpdateInfo(): Promise<void> {
    if (updateInfoLoaded.value) {
      return;
    }

    const { data } = await client.get<{ 200: AppUpdateInfo }, unknown, true>({
      url: '/api/v1/system/check-update',
      throwOnError: true,
    });
    updateInfo.value = data;
    updateInfoLoaded.value = true;
  }

  return {
    appInfo,
    appInfoLoaded,
    updateInfo,
    updateInfoLoaded,
    loadAppInfo,
    loadUpdateInfo,
    setAppInfo,
  };
});
