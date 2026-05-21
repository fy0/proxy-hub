<script setup lang="ts">
import { computed } from 'vue';

import { useI18n } from '@/i18n';
import { useAppStore } from '@/stores/app';
import { formatVersionForDisplay } from '@/utils/versionDisplay';

defineOptions({
  name: 'AppVersionBadge',
});

const { locale, t } = useI18n();
const appStore = useAppStore();

const currentVersion = computed(() => formatVersionForDisplay(appStore.appInfo.version));
const updateNotice = computed(() => {
  const localeToken = locale.value;
  const info = appStore.updateInfo;
  if (!info?.hasUpdate || !info.updateUrl || !info.latestVersion) {
    return null;
  }

  const latestVersion = formatVersionForDisplay(info.latestVersion);
  const updateCommand = info.updateCommand?.trim();
  const title = updateCommand
    ? t('common.npmUpdateTitle', {
        command: updateCommand,
        version: `v${latestVersion}`,
      })
    : t('common.updateTitle', {
        version: `v${latestVersion}`,
      });

  return {
    href: info.updateUrl,
    label: t('common.updateBadge'),
    title,
    localeToken,
  };
});
</script>

<template>
  <span class="app-version-badge">
    <span class="app-version-current">v{{ currentVersion }}</span>
    <a
      v-if="updateNotice"
      class="app-version-update"
      :href="updateNotice.href"
      target="_blank"
      rel="noopener noreferrer"
      :title="updateNotice.title"
      :aria-label="updateNotice.title"
    >
      <span>{{ updateNotice.label }}</span>
    </a>
  </span>
</template>

<style scoped>
.app-version-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  flex-wrap: wrap;
  align-self: flex-end;
  margin-bottom: 2px;
}

.app-version-current {
  color: rgb(100 116 139);
  font-size: 12px;
  line-height: 1;
  font-weight: 700;
  white-space: nowrap;
}

.app-version-update {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 18px;
  min-width: 0;
  padding: 0 6px;
  border: 1px solid rgb(191 219 254);
  border-radius: 6px;
  color: rgb(37 99 235);
  background: rgb(239 246 255);
  font-size: 11px;
  line-height: 1;
  font-weight: 700;
  text-decoration: none;
  white-space: nowrap;
  transition:
    color 150ms ease,
    background 150ms ease;
}

.app-version-update:hover {
  color: rgb(30 64 175);
  background: rgb(219 234 254);
}

@media (max-width: 860px) {
  .app-version-badge {
    gap: 4px;
  }

  .app-version-update {
    min-height: 17px;
    padding: 0 5px;
    font-size: 10px;
  }
}
</style>
