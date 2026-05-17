<script setup lang="ts">
import { RefreshCw, Trash2 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/i18n';
import type { HomeViewContext } from './types';

const { t } = useI18n();
const props = defineProps<{
  context: HomeViewContext;
}>();

const {
  subscriptionForm,
  manualGroups,
  handleSubscriptionSubmit,
  subscriptionPreview,
  previewSummary,
  previewTypeLabel,
  previewActionLabel,
  subscriptions,
  syncExistingSubscription,
  removeSubscription,
  subscriptionGroupName,
  formatDateTime,
} = props.context;
</script>

<template>
  <section class="panel simple-panel">
    <div class="import-layout">
      <form class="manual-node-form" @submit.prevent="handleSubscriptionSubmit">
        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.subscriptionName') }}</span>
            <input
              v-model.trim="subscriptionForm.name"
              type="text"
              :placeholder="t('common.optional')"
            />
          </label>
          <label>
            <span>{{ t('home.form.subscriptionUrl') }}</span>
            <input v-model.trim="subscriptionForm.url" type="url" required />
          </label>
        </div>
        <label>
          <span>{{ t('home.form.subscriptionGroup') }}</span>
          <select v-model="subscriptionForm.groupId">
            <option value="">{{ t('home.subscription.createGroup') }}</option>
            <option v-for="group in manualGroups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select>
        </label>
        <label>
          <span>{{ t('home.form.remark') }}</span>
          <input
            v-model.trim="subscriptionForm.remark"
            type="text"
            :placeholder="t('common.optional')"
          />
        </label>
        <Button type="submit">
          <RefreshCw class="size-4" aria-hidden="true" />
          {{
            subscriptionPreview
              ? t('home.importPreview.confirmSubscription')
              : t('home.importPreview.previewSubscription')
          }}
        </Button>
      </form>
    </div>

    <div v-if="subscriptionPreview" class="import-preview-panel">
      <div class="import-preview-heading">
        <strong>{{ t('home.importPreview.title') }}</strong>
        <span>{{ previewSummary(subscriptionPreview) }}</span>
      </div>
      <div class="import-preview-list">
        <article
          v-for="(item, index) in subscriptionPreview.items"
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

    <div class="node-table">
      <article v-for="subscription in subscriptions" :key="subscription.id" class="node-row">
        <div class="node-protocol">{{ t('home.subscription.source') }}</div>
        <div class="node-main">
          <strong>{{ subscription.name }}</strong>
          <span>{{ subscription.url }}</span>
        </div>
        <div class="card-actions">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            :title="t('common.syncSubscription')"
            @click="syncExistingSubscription(subscription.id)"
          >
            <RefreshCw class="size-4" aria-hidden="true" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            :title="t('common.deleteSubscription')"
            @click="removeSubscription(subscription.id).catch(() => undefined)"
          >
            <Trash2 class="size-4" aria-hidden="true" />
          </Button>
        </div>
        <div class="node-meta">
          <small>{{ subscriptionGroupName(subscription.groupId) }}</small>
          <small>{{ subscription.lastSyncStatus || t('home.subscription.notSynced') }}</small>
          <small v-if="subscription.lastSyncedAt">{{
            formatDateTime(subscription.lastSyncedAt)
          }}</small>
        </div>
      </article>
    </div>
  </section>
</template>
