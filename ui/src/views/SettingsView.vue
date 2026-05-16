<script setup lang="ts">
import { computed, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { ArrowLeft, Download, FileJson, RefreshCw, Upload } from 'lucide-vue-next';
import { getProxySettingsExport, postProxySettingsImport } from '@/api/generated';
import type { SettingsBackupDto, SettingsBackupDtoWritable } from '@/api/generated';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/i18n';
import { useAppStore } from '@/stores/app';
import { formatVersionForDisplay } from '@/utils/versionDisplay';
import './settings.css';

interface ParsedBackupSummary {
  nodes: number;
  groups: number;
  subscriptions: number;
  mappings: number;
}

const settingsKind = 'proxyhub.proxy-settings';
const settingsSchemaVersion = 1;

const { t } = useI18n();
const appStore = useAppStore();

const selectedFileName = ref('');
const selectedBackup = ref<SettingsBackupDtoWritable | null>(null);
const parseMessage = ref('');
const operationMessage = ref('');
const isExporting = ref(false);
const isImporting = ref(false);
const confirmOverwrite = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);

const displayAppVersion = computed(() => formatVersionForDisplay(appStore.appInfo.version));
const isBusy = computed(() => isExporting.value || isImporting.value);
const parsedSummary = computed<ParsedBackupSummary | null>(() => {
  const backup = selectedBackup.value;
  if (!backup) return null;
  return {
    nodes: backup.data.nodes?.length ?? 0,
    groups: backup.data.groups?.length ?? 0,
    subscriptions: backup.data.subscriptions?.length ?? 0,
    mappings: backup.data.mappings?.length ?? 0,
  };
});

function backupToWritable(value: unknown): SettingsBackupDtoWritable {
  if (!isPlainObject(value)) {
    throw new Error(t('settings.messages.invalidJsonShape'));
  }
  const backup = value as Record<string, unknown>;
  const data = backup.data;
  if (
    backup.kind !== settingsKind ||
    backup.schemaVersion !== settingsSchemaVersion ||
    typeof backup.exportedAt !== 'string' ||
    !isPlainObject(data)
  ) {
    throw new Error(t('settings.messages.invalidBackup'));
  }

  const typedData = data as Record<string, unknown>;
  for (const key of ['nodes', 'groups', 'subscriptions', 'mappings']) {
    if (!Array.isArray(typedData[key])) {
      throw new Error(t('settings.messages.invalidBackup'));
    }
  }

  return {
    kind: settingsKind,
    schemaVersion: settingsSchemaVersion,
    exportedAt: backup.exportedAt,
    data: {
      nodes: typedData.nodes as SettingsBackupDto['data']['nodes'],
      groups: typedData.groups as SettingsBackupDto['data']['groups'],
      subscriptions: typedData.subscriptions as SettingsBackupDto['data']['subscriptions'],
      mappings: typedData.mappings as SettingsBackupDto['data']['mappings'],
    },
  };
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function errorToMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  if (isPlainObject(error)) {
    for (const key of ['message', 'detail', 'title']) {
      const value = error[key];
      if (typeof value === 'string' && value.trim() !== '') return value;
    }
  }
  return t('settings.messages.requestFailed');
}

function buildExportFileName(): string {
  const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\..+$/, '').replace('T', '-');
  return `proxyhub-settings-${stamp}.json`;
}

function downloadJson(backup: SettingsBackupDto): void {
  const blob = new Blob([JSON.stringify(backup, null, 2)], {
    type: 'application/json;charset=utf-8',
  });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = buildExportFileName();
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

async function handleExport(): Promise<void> {
  isExporting.value = true;
  operationMessage.value = '';
  try {
    const { data } = await getProxySettingsExport({ throwOnError: true });
    downloadJson(data);
    operationMessage.value = t('settings.messages.exported');
  } catch (error) {
    operationMessage.value = errorToMessage(error);
  } finally {
    isExporting.value = false;
  }
}

async function handleFileChange(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  selectedBackup.value = null;
  selectedFileName.value = file?.name ?? '';
  parseMessage.value = '';
  operationMessage.value = '';
  confirmOverwrite.value = false;

  if (!file) return;
  if (!file.name.toLowerCase().endsWith('.json')) {
    parseMessage.value = t('settings.messages.fileType');
    return;
  }

  try {
    selectedBackup.value = backupToWritable(JSON.parse(await file.text()));
    parseMessage.value = t('settings.messages.fileReady');
  } catch (error) {
    parseMessage.value = errorToMessage(error);
  }
}

async function handleImport(): Promise<void> {
  if (!selectedBackup.value) {
    parseMessage.value = t('settings.messages.selectFile');
    return;
  }
  if (!confirmOverwrite.value) {
    parseMessage.value = t('settings.messages.confirmRequired');
    return;
  }

  isImporting.value = true;
  operationMessage.value = '';
  parseMessage.value = '';
  try {
    const { data } = await postProxySettingsImport({
      body: selectedBackup.value,
      throwOnError: true,
    });
    operationMessage.value = data.runtimeReloadWarning
      ? t('settings.messages.importedWithWarning', { warning: data.runtimeReloadWarning })
      : t('settings.messages.imported', {
          nodes: data.nodes,
          groups: data.groups,
          subscriptions: data.subscriptions,
          mappings: data.mappings,
        });
    selectedBackup.value = null;
    selectedFileName.value = '';
    confirmOverwrite.value = false;
    if (fileInput.value) fileInput.value.value = '';
  } catch (error) {
    operationMessage.value = errorToMessage(error);
  } finally {
    isImporting.value = false;
  }
}
</script>

<template>
  <main class="settings-shell">
    <section class="settings-header">
      <header class="settings-brand-bar">
        <RouterLink class="settings-brand-lockup" :to="{ name: 'home' }">
          <span class="settings-brand-logo" aria-hidden="true">
            <span class="settings-brand-logo-core"></span>
          </span>
          <span class="settings-brand-name">{{ t('app.name') }}</span>
          <span class="settings-brand-version">v{{ displayAppVersion }}</span>
        </RouterLink>

        <RouterLink class="settings-back-link" :to="{ name: 'home' }">
          <ArrowLeft class="size-4" aria-hidden="true" />
          <span>{{ t('common.goHome') }}</span>
        </RouterLink>
      </header>
    </section>

    <section class="settings-panel">
      <header class="settings-page-heading">
        <div>
          <p>{{ t('settings.eyebrow') }}</p>
          <h1>{{ t('settings.title') }}</h1>
        </div>
      </header>

      <section class="settings-actions-grid">
        <article class="settings-action-card">
          <div class="settings-card-heading">
            <span class="settings-card-icon" aria-hidden="true">
              <Download class="size-5" />
            </span>
            <div>
              <h2>{{ t('settings.export.title') }}</h2>
              <p>{{ t('settings.export.lead') }}</p>
            </div>
          </div>
          <dl class="settings-summary-list">
            <div>
              <dt>{{ t('settings.scopeLabel') }}</dt>
              <dd>{{ t('settings.scopeValue') }}</dd>
            </div>
            <div>
              <dt>{{ t('settings.formatLabel') }}</dt>
              <dd>{{ t('settings.formatValue') }}</dd>
            </div>
          </dl>
          <Button type="button" class="settings-primary-action" :disabled="isBusy" @click="handleExport">
            <RefreshCw v-if="isExporting" class="size-4 spin-icon" aria-hidden="true" />
            <Download v-else class="size-4" aria-hidden="true" />
            {{ t('settings.export.button') }}
          </Button>
        </article>

        <article class="settings-action-card">
          <div class="settings-card-heading">
            <span class="settings-card-icon" aria-hidden="true">
              <Upload class="size-5" />
            </span>
            <div>
              <h2>{{ t('settings.import.title') }}</h2>
              <p>{{ t('settings.import.lead') }}</p>
            </div>
          </div>

          <label class="settings-file-picker">
            <FileJson class="size-5" aria-hidden="true" />
            <span>{{ selectedFileName || t('settings.import.selectFile') }}</span>
            <input ref="fileInput" type="file" accept="application/json,.json" @change="handleFileChange" />
          </label>

          <div v-if="parsedSummary" class="settings-import-summary">
            <span>{{ t('settings.counts.nodes', { count: parsedSummary.nodes }) }}</span>
            <span>{{ t('settings.counts.groups', { count: parsedSummary.groups }) }}</span>
            <span>{{ t('settings.counts.subscriptions', { count: parsedSummary.subscriptions }) }}</span>
            <span>{{ t('settings.counts.mappings', { count: parsedSummary.mappings }) }}</span>
          </div>

          <label class="settings-confirm">
            <input v-model="confirmOverwrite" type="checkbox" :disabled="!selectedBackup || isBusy" />
            <span>{{ t('settings.import.confirmOverwrite') }}</span>
          </label>

          <Button
            type="button"
            class="settings-primary-action"
            :disabled="!selectedBackup || !confirmOverwrite || isBusy"
            @click="handleImport"
          >
            <RefreshCw v-if="isImporting" class="size-4 spin-icon" aria-hidden="true" />
            <Upload v-else class="size-4" aria-hidden="true" />
            {{ t('settings.import.button') }}
          </Button>
        </article>
      </section>

      <p v-if="parseMessage" class="settings-message" role="status">{{ parseMessage }}</p>
      <p v-if="operationMessage" class="settings-message" role="status">{{ operationMessage }}</p>
    </section>
  </main>
</template>
