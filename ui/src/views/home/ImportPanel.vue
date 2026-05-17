<script setup lang="ts">
import { Import, Plus } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/i18n';
import type { HomeViewContext } from './types';

const { t } = useI18n();
const props = defineProps<{
  context: HomeViewContext;
}>();

const {
  rawImport,
  rawImportGroupId,
  groups,
  manualNodeForm,
  protocolLabels,
  handleManualNodeSubmit,
  importPreview,
  previewSummary,
  previewTypeLabel,
  previewActionLabel,
  handleImport,
  importMessage,
} = props.context;
</script>

<template>
  <section class="panel simple-panel">
    <div class="import-layout">
      <label>
        <span>{{ t('home.form.shareLink') }}</span>
        <textarea
          v-model="rawImport"
          rows="5"
          :placeholder="t('home.placeholders.shareLinks')"
        ></textarea>
      </label>
      <label>
        <span>{{ t('home.form.nodeGroup') }}</span>
        <select v-model="rawImportGroupId">
          <option value="">{{ t('home.groupMeta.ungrouped') }}</option>
          <option v-for="group in groups" :key="group.id" :value="group.id">
            {{ group.name }}
          </option>
        </select>
      </label>

      <form class="manual-node-form" @submit.prevent="handleManualNodeSubmit">
        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.name') }}</span>
            <input v-model.trim="manualNodeForm.name" type="text" required />
          </label>
          <label>
            <span>{{ t('home.form.protocol') }}</span>
            <select v-model="manualNodeForm.protocol">
              <option value="socks5">{{ protocolLabels.socks5 }}</option>
              <option value="http">{{ protocolLabels.http }}</option>
              <option value="vless">{{ protocolLabels.vless }}</option>
              <option value="vmess">{{ protocolLabels.vmess }}</option>
              <option value="trojan">{{ protocolLabels.trojan }}</option>
              <option value="shadowsocks">{{ protocolLabels.shadowsocks }}</option>
              <option value="tuic">{{ protocolLabels.tuic }}</option>
              <option value="ssh">{{ protocolLabels.ssh }}</option>
            </select>
          </label>
        </div>

        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.server') }}</span>
            <input v-model.trim="manualNodeForm.server" type="text" required />
          </label>
          <label>
            <span>{{ t('home.form.port') }}</span>
            <input
              v-model.number="manualNodeForm.port"
              type="number"
              min="1"
              max="65535"
              required
            />
          </label>
        </div>

        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.username') }}</span>
            <input
              v-model.trim="manualNodeForm.username"
              type="text"
              :placeholder="t('common.optional')"
            />
          </label>
          <label>
            <span>{{ t('home.form.password') }}</span>
            <input
              v-model.trim="manualNodeForm.password"
              type="password"
              :placeholder="t('common.optional')"
            />
          </label>
        </div>

        <label>
          <span>{{ t('home.form.nodeGroup') }}</span>
          <select v-model="manualNodeForm.groupId">
            <option value="">{{ t('home.groupMeta.ungrouped') }}</option>
            <option v-for="group in groups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select>
        </label>

        <label>
          <span>{{ t('home.form.remark') }}</span>
          <input
            v-model.trim="manualNodeForm.remark"
            type="text"
            :placeholder="t('common.optional')"
          />
        </label>

        <Button type="submit">
          <Plus class="size-4" aria-hidden="true" />
          {{ t('common.addNode') }}
        </Button>
      </form>
    </div>

    <div v-if="importPreview" class="import-preview-panel">
      <div class="import-preview-heading">
        <strong>{{ t('home.importPreview.title') }}</strong>
        <span>{{ previewSummary(importPreview) }}</span>
      </div>
      <div class="import-preview-list">
        <article
          v-for="(item, index) in importPreview.items"
          :key="`${item.type}-${item.name}-${index}`"
          class="import-preview-item"
          :class="{ muted: item.action === 'skip' || item.action === 'fail' }"
        >
          <span>{{ previewTypeLabel(item) }}</span>
          <strong>{{ item.name }}</strong>
          <small>{{ item.detail || previewActionLabel(item) }}</small>
        </article>
      </div>
    </div>

    <div class="button-row">
      <Button type="button" @click="handleImport">
        <Import class="size-4" aria-hidden="true" />
        {{ importPreview ? t('home.importPreview.confirmImport') : t('common.importLinks') }}
      </Button>
      <span class="inline-message">{{ importMessage }}</span>
    </div>
  </section>
</template>
