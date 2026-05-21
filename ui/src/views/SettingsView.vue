<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import {
  ArrowLeft,
  Download,
  FileJson,
  Power,
  RefreshCw,
  Save,
  Server,
  SlidersHorizontal,
  Upload,
} from 'lucide-vue-next';
import { postProxySettingsImport } from '@/api/generated';
import { client } from '@/api/generated/client.gen';
import type { SettingsBackupDto, SettingsBackupDtoWritable, SettingsImportResultDto } from '@/api/generated';
import { Button } from '@/components/ui/button';
import AppVersionBadge from '@/components/AppVersionBadge.vue';
import proxyHubMarkUrl from '@/assets/mark-large.png';
import { useI18n } from '@/i18n';
import { useUiPreferences, type ExtraUiInfoPreference } from '@/composables/useUiPreferences';
import './settings.css';

interface ParsedBackupSummary {
  nodes: number;
  groups: number;
  subscriptions: number;
  mappings: number;
}

interface SystemListenConfig {
  serveAt: string;
  runningServeAt: string;
  listenAddress: string;
  listenPort: number;
  restartRequired: boolean;
}

interface SystemListenUpdateResult {
  message: string;
  item: SystemListenConfig;
}

interface SystemRestartResult {
  message: string;
}

const settingsKind = 'proxyhub.proxy-settings';
const settingsSchemaVersion = 1;

const { t } = useI18n();
const { extraUiInfoPreference, setExtraUiInfoPreference } = useUiPreferences();

const selectedFileName = ref('');
const selectedBackup = ref<SettingsBackupDtoWritable | null>(null);
const selectedZipBackup = ref<File | null>(null);
const parseMessage = ref('');
const operationMessage = ref('');
const systemMessage = ref('');
const isExporting = ref(false);
const isImporting = ref(false);
const isLoadingListen = ref(false);
const isSavingListen = ref(false);
const isRestarting = ref(false);
const confirmOverwrite = ref(false);
const showRestartConfirm = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const listenConfig = ref<SystemListenConfig | null>(null);
const listenAddress = ref('');
const listenPort = ref(3020);
const extraUiInfoPreferenceOptions = computed<Array<{ label: string; value: ExtraUiInfoPreference }>>(
  () => [
    { label: t('settings.extraUiInfo.default'), value: 'default' },
    { label: t('settings.extraUiInfo.off'), value: 'off' },
    { label: t('settings.extraUiInfo.on'), value: 'on' },
  ]
);

const isBusy = computed(
  () => isExporting.value || isImporting.value || isSavingListen.value || isRestarting.value
);
const isListenPortValid = computed(() => {
  const port = Number(listenPort.value);
  return Number.isInteger(port) && port >= 1 && port <= 65535;
});
const canSaveListenConfig = computed(
  () => !isBusy.value && !isLoadingListen.value && isListenPortValid.value
);
const canRestartService = computed(() => !isLoadingListen.value && !isRestarting.value);
const hasSelectedBackup = computed(() => selectedBackup.value !== null || selectedZipBackup.value !== null);
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
const runningServeAtLabel = computed(() => listenConfig.value?.runningServeAt || '-');
const savedServeAtLabel = computed(() => listenConfig.value?.serveAt || '-');
const restartRequiredLabel = computed(() =>
  listenConfig.value?.restartRequired ? t('settings.service.restartYes') : t('settings.service.restartNo')
);

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

function buildExportFileName(extension = 'zip'): string {
  const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\..+$/, '').replace('T', '-');
  return `proxyhub-settings-${stamp}.${extension}`;
}

function downloadBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = fileName;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function fileNameFromContentDisposition(header: string | null): string | null {
  if (!header) return null;
  const encoded = header.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) {
    try {
      return decodeURIComponent(encoded);
    } catch {
      return encoded;
    }
  }
  return header.match(/filename="?([^";]+)"?/i)?.[1] ?? null;
}

async function handleExport(): Promise<void> {
  isExporting.value = true;
  operationMessage.value = '';
  try {
    const { data, response } = await client.get<{ 200: Blob }, unknown, true>({
      url: '/api/v1/proxy/settings/export/zip',
      parseAs: 'blob',
      throwOnError: true,
    });
    downloadBlob(data, fileNameFromContentDisposition(response.headers.get('Content-Disposition')) ?? buildExportFileName());
    operationMessage.value = t('settings.messages.exported');
  } catch (error) {
    operationMessage.value = errorToMessage(error);
  } finally {
    isExporting.value = false;
  }
}

function applyListenConfig(config: SystemListenConfig): void {
  listenConfig.value = config;
  listenAddress.value = config.listenAddress;
  listenPort.value = config.listenPort;
}

async function loadListenConfig(): Promise<void> {
  isLoadingListen.value = true;
  systemMessage.value = '';
  try {
    const { data } = await client.get<{ 200: SystemListenConfig }, unknown, true>({
      url: '/api/v1/system/listen',
      throwOnError: true,
    });
    applyListenConfig(data);
  } catch (error) {
    systemMessage.value = errorToMessage(error);
  } finally {
    isLoadingListen.value = false;
  }
}

async function handleSaveListenConfig(): Promise<void> {
  if (!isListenPortValid.value) {
    systemMessage.value = t('settings.messages.invalidListenPort');
    return;
  }

  isSavingListen.value = true;
  systemMessage.value = '';
  try {
    const { data } = await client.put<{ 200: SystemListenUpdateResult }, unknown, true>({
      url: '/api/v1/system/listen',
      body: {
        listenAddress: listenAddress.value.trim(),
        listenPort: Number(listenPort.value),
      },
      parseAs: 'json',
      throwOnError: true,
    });
    applyListenConfig(data.item);
    systemMessage.value = data.message || t('settings.messages.listenSaved');
  } catch (error) {
    systemMessage.value = errorToMessage(error);
  } finally {
    isSavingListen.value = false;
  }
}

function browserHostForListenAddress(address: string): string {
  const trimmed = address.trim();
  if (!trimmed || trimmed === '0.0.0.0' || trimmed === '::') {
    return window.location.hostname || '127.0.0.1';
  }
  return trimmed;
}

function bracketIPv6Host(host: string): string {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
}

function targetServiceBaseUrl(config: SystemListenConfig): string {
  const host = bracketIPv6Host(browserHostForListenAddress(config.listenAddress));
  return `${window.location.protocol}//${host}:${config.listenPort}`;
}

function restartTargetUrl(): string {
  const config = listenConfig.value;
  if (!config || typeof window === 'undefined') return '';

  const path = `${window.location.pathname || '/settings'}${window.location.search || ''}`;
  return new URL(path, targetServiceBaseUrl(config)).toString();
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, ms));
}

async function waitForRestart(targetUrl: string): Promise<void> {
  const healthUrl = new URL('/health', targetUrl).toString();
  const deadline = Date.now() + 30_000;

  await sleep(800);
  while (Date.now() < deadline) {
    try {
      const response = await fetch(healthUrl, { cache: 'no-store' });
      if (response.ok) {
        window.location.assign(targetUrl);
        return;
      }
    } catch {
      // The service is expected to be unavailable during restart.
    }
    await sleep(800);
  }

  systemMessage.value = t('settings.messages.restartTimeout', { url: targetUrl });
  isRestarting.value = false;
}

async function handleRestartService(): Promise<void> {
  isRestarting.value = true;
  systemMessage.value = t('settings.messages.restarting');
  const targetUrl = restartTargetUrl() || window.location.href;

  try {
    const { data } = await client.post<{ 202: SystemRestartResult }, unknown, true>({
      url: '/api/v1/system/restart',
      body: { confirm: true },
      parseAs: 'json',
      throwOnError: true,
    });
    showRestartConfirm.value = false;
    systemMessage.value = data.message || t('settings.messages.restartRequested');
    await waitForRestart(targetUrl);
  } catch (error) {
    systemMessage.value = errorToMessage(error);
    isRestarting.value = false;
  }
}

async function handleFileChange(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  selectedBackup.value = null;
  selectedZipBackup.value = null;
  selectedFileName.value = file?.name ?? '';
  parseMessage.value = '';
  operationMessage.value = '';
  confirmOverwrite.value = false;

  if (!file) return;
  const fileName = file.name.toLowerCase();
  const isJson = fileName.endsWith('.json');
  const isZip = fileName.endsWith('.zip');
  if (!isJson && !isZip) {
    parseMessage.value = t('settings.messages.fileType');
    return;
  }

  if (isZip) {
    selectedZipBackup.value = file;
    parseMessage.value = t('settings.messages.zipFileReady');
    return;
  }

  try {
    selectedBackup.value = backupToWritable(JSON.parse(await file.text()));
    parseMessage.value = t('settings.messages.fileReady');
  } catch (error) {
    parseMessage.value = errorToMessage(error);
  }
}

function formatImportResult(data: SettingsImportResultDto): string {
  return data.runtimeReloadWarning
    ? t('settings.messages.importedWithWarning', { warning: data.runtimeReloadWarning })
    : t('settings.messages.imported', {
        nodes: data.nodes,
        groups: data.groups,
        subscriptions: data.subscriptions,
        mappings: data.mappings,
      });
}

function clearSelectedImport(): void {
  selectedBackup.value = null;
  selectedZipBackup.value = null;
  selectedFileName.value = '';
  confirmOverwrite.value = false;
  if (fileInput.value) fileInput.value.value = '';
}

async function handleImport(): Promise<void> {
  if (!hasSelectedBackup.value) {
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
    const result = selectedZipBackup.value
      ? await client.post<{ 200: SettingsImportResultDto }, unknown, true>({
          url: '/api/v1/proxy/settings/import/zip',
          body: selectedZipBackup.value,
          bodySerializer: null,
          headers: { 'Content-Type': 'application/zip' },
          parseAs: 'json',
          throwOnError: true,
        })
      : await postProxySettingsImport({
          body: selectedBackup.value as SettingsBackupDtoWritable,
          throwOnError: true,
        });
    operationMessage.value = formatImportResult(result.data);
    clearSelectedImport();
  } catch (error) {
    operationMessage.value = errorToMessage(error);
  } finally {
    isImporting.value = false;
  }
}

onMounted(() => {
  void loadListenConfig();
});
</script>

<template>
  <main class="settings-shell">
    <section class="settings-header">
      <header class="settings-brand-bar">
        <div class="settings-brand-lockup">
          <RouterLink class="settings-brand-home" :to="{ name: 'home' }">
            <img class="settings-brand-logo" :src="proxyHubMarkUrl" alt="" aria-hidden="true" />
            <span class="settings-brand-name">{{ t('app.name') }}</span>
          </RouterLink>
          <AppVersionBadge />
        </div>

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
        <article class="settings-action-card settings-preference-card">
          <div class="settings-card-heading">
            <span class="settings-card-icon" aria-hidden="true">
              <SlidersHorizontal class="size-5" />
            </span>
            <div>
              <h2>{{ t('settings.extraUiInfo.title') }}</h2>
              <p>{{ t('settings.extraUiInfo.lead') }}</p>
            </div>
          </div>

          <div class="settings-preference-control" role="group" :aria-label="t('settings.extraUiInfo.title')">
            <Button
              v-for="option in extraUiInfoPreferenceOptions"
              :key="option.value"
              type="button"
              variant="outline"
              class="settings-preference-button"
              :class="{ active: extraUiInfoPreference === option.value }"
              :aria-pressed="extraUiInfoPreference === option.value"
              @click="setExtraUiInfoPreference(option.value)"
            >
              {{ option.label }}
            </Button>
          </div>
        </article>

        <article class="settings-action-card settings-service-card">
          <div class="settings-card-heading">
            <span class="settings-card-icon" aria-hidden="true">
              <Server class="size-5" />
            </span>
            <div>
              <h2>{{ t('settings.service.title') }}</h2>
              <p>{{ t('settings.service.lead') }}</p>
            </div>
          </div>

          <form class="settings-listen-form" @submit.prevent="handleSaveListenConfig">
            <label class="settings-field">
              <span>{{ t('settings.service.addressLabel') }}</span>
              <input
                v-model="listenAddress"
                type="text"
                inputmode="text"
                autocomplete="off"
                :placeholder="t('settings.service.addressPlaceholder')"
                :disabled="isLoadingListen || isBusy"
              />
            </label>

            <label class="settings-field">
              <span>{{ t('settings.service.portLabel') }}</span>
              <input
                v-model.number="listenPort"
                type="number"
                inputmode="numeric"
                min="1"
                max="65535"
                step="1"
                :disabled="isLoadingListen || isBusy"
              />
            </label>
          </form>

          <dl class="settings-summary-list">
            <div>
              <dt>{{ t('settings.service.runningLabel') }}</dt>
              <dd>{{ runningServeAtLabel }}</dd>
            </div>
            <div>
              <dt>{{ t('settings.service.savedLabel') }}</dt>
              <dd>{{ savedServeAtLabel }}</dd>
            </div>
            <div>
              <dt>{{ t('settings.service.restartRequiredLabel') }}</dt>
              <dd>{{ restartRequiredLabel }}</dd>
            </div>
          </dl>

          <div class="settings-card-actions">
            <Button
              type="button"
              class="settings-primary-action"
              :disabled="!canSaveListenConfig"
              @click="handleSaveListenConfig"
            >
              <RefreshCw v-if="isSavingListen" class="size-4 spin-icon" aria-hidden="true" />
              <Save v-else class="size-4" aria-hidden="true" />
              {{ t('settings.service.saveButton') }}
            </Button>

            <Button
              type="button"
              variant="outline"
              class="settings-secondary-action"
              :disabled="!canRestartService"
              @click="showRestartConfirm = true"
            >
              <RefreshCw v-if="isRestarting" class="size-4 spin-icon" aria-hidden="true" />
              <Power v-else class="size-4" aria-hidden="true" />
              {{ t('settings.service.restartButton') }}
            </Button>
          </div>
        </article>

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
            <input
              ref="fileInput"
              type="file"
              accept="application/json,application/zip,.json,.zip"
              @change="handleFileChange"
            />
          </label>

          <div v-if="parsedSummary" class="settings-import-summary">
            <span>{{ t('settings.counts.nodes', { count: parsedSummary.nodes }) }}</span>
            <span>{{ t('settings.counts.groups', { count: parsedSummary.groups }) }}</span>
            <span>{{ t('settings.counts.subscriptions', { count: parsedSummary.subscriptions }) }}</span>
            <span>{{ t('settings.counts.mappings', { count: parsedSummary.mappings }) }}</span>
          </div>

          <label class="settings-confirm">
            <input v-model="confirmOverwrite" type="checkbox" :disabled="!hasSelectedBackup || isBusy" />
            <span>{{ t('settings.import.confirmOverwrite') }}</span>
          </label>

          <Button
            type="button"
            class="settings-primary-action"
            :disabled="!hasSelectedBackup || !confirmOverwrite || isBusy"
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
      <p v-if="systemMessage" class="settings-message" role="status">{{ systemMessage }}</p>
    </section>

    <Transition name="settings-modal-pop">
      <div
        v-if="showRestartConfirm"
        class="settings-modal-backdrop"
        role="presentation"
        @click.self="showRestartConfirm = false"
      >
        <section
          class="settings-modal-card"
          role="dialog"
          aria-modal="true"
          aria-labelledby="settings-restart-title"
        >
          <div class="settings-card-heading">
            <span class="settings-card-icon" aria-hidden="true">
              <Power class="size-5" />
            </span>
            <div>
              <h2 id="settings-restart-title">{{ t('settings.restartConfirm.title') }}</h2>
              <p>{{ t('settings.restartConfirm.message') }}</p>
            </div>
          </div>

          <div class="settings-modal-actions">
            <Button type="button" variant="outline" :disabled="isRestarting" @click="showRestartConfirm = false">
              {{ t('common.cancel') }}
            </Button>
            <Button type="button" variant="destructive" :disabled="isRestarting" @click="handleRestartService">
              <RefreshCw v-if="isRestarting" class="size-4 spin-icon" aria-hidden="true" />
              <Power v-else class="size-4" aria-hidden="true" />
              {{ t('settings.restartConfirm.confirmButton') }}
            </Button>
          </div>
        </section>
      </div>
    </Transition>
  </main>
</template>
