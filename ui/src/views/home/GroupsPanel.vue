<script setup lang="ts">
import { computed, ref } from 'vue';
import { Edit3, RefreshCw, Search, Trash2 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import ActionTooltip from '@/components/ActionTooltip.vue';
import { useI18n } from '@/i18n';
import type { HomeViewContext, NodeGroupSummaryItem } from './types';

const { t } = useI18n();
const props = defineProps<{
  context: HomeViewContext;
}>();

const {
  groupSummaryItems,
  selectNodeGroupFilter,
  openEditGroupById,
  requestRemoveGroup,
  importMessage,
  subscriptions,
  subscriptionForm,
  handleSubscriptionSubmit,
  subscriptionPreview,
  previewSummary,
  previewTypeLabel,
  previewActionLabel,
  syncExistingSubscription,
  removeSubscription,
  subscriptionGroupName,
  formatDateTime,
} = props.context;

const groupSearch = ref('');

interface GroupSummarySection {
  key: string;
  title: string;
  count: number;
  items: NodeGroupSummaryItem[];
}

function normalizedSearchValue(value: string): string {
  return value.trim().toLocaleLowerCase();
}

function itemMatchesSearch(item: NodeGroupSummaryItem, keyword: string): boolean {
  if (!keyword) return true;
  return [item.title, item.detail, item.strategyLabel, item.filter, item.typeLabel]
    .join(' ')
    .toLocaleLowerCase()
    .includes(keyword);
}

function sectionCount(items: NodeGroupSummaryItem[]): number {
  return items.reduce((total, item) => total + item.count, 0);
}

const visibleGroupSections = computed<GroupSummarySection[]>(() => {
  const keyword = normalizedSearchValue(groupSearch.value);
  const items = groupSummaryItems.value.filter(item => itemMatchesSearch(item, keyword));
  const manualItems = items.filter(item => !item.isSubscription);
  const subscriptionItems = items.filter(item => item.isSubscription);
  const subscriptionById = new Map(
    subscriptions.value.map(subscription => [subscription.id, subscription])
  );
  const sections: GroupSummarySection[] = [
    {
      key: 'manual',
      title: t('home.groupSections.manual'),
      count: sectionCount(manualItems),
      items: manualItems,
    },
  ];

  for (const subscription of subscriptions.value) {
    const sectionItems = subscriptionItems.filter(item => item.subscriptionId === subscription.id);
    if (sectionItems.length > 0) {
      sections.push({
        key: `subscription:${subscription.id}`,
        title: subscription.name,
        count: sectionCount(sectionItems),
        items: sectionItems,
      });
    }
  }

  const orphanItems = subscriptionItems.filter(
    item => !item.subscriptionId || !subscriptionById.has(item.subscriptionId)
  );
  if (orphanItems.length > 0) {
    sections.push({
      key: 'subscription:unknown',
      title: t('home.groupSections.unknownSubscription'),
      count: sectionCount(orphanItems),
      items: orphanItems,
    });
  }

  return keyword ? sections.filter(section => section.items.length > 0) : sections;
});

const hasVisibleGroups = computed(() =>
  visibleGroupSections.value.some(section => section.items.length > 0)
);
</script>

<template>
  <section class="panel simple-panel">
    <label class="group-card-search">
      <span>{{ t('home.groupSections.searchLabel') }}</span>
      <div class="group-card-search-box">
        <Search class="size-4" aria-hidden="true" />
        <input
          v-model.trim="groupSearch"
          type="search"
          autocomplete="off"
          :placeholder="t('home.placeholders.groupSearch')"
        />
      </div>
    </label>

    <div class="node-group-section-stack">
      <section
        v-for="section in visibleGroupSections"
        :key="section.key"
        class="node-group-card-section"
      >
        <header class="node-group-card-section-header">
          <div>
            <strong>{{ section.title }}</strong>
            <span>{{ t('home.groupSections.cardCount', { count: section.items.length }) }}</span>
          </div>
          <small>{{ t('home.groupMeta.nodeCount', { count: section.count }) }}</small>
        </header>

        <div v-if="section.items.length" class="node-group-summary-grid">
          <article
            v-for="item in section.items"
            :key="item.key"
            class="node-group-summary-card"
            :class="{ unavailable: item.allUnavailable }"
          >
            <button
              type="button"
              class="node-group-summary-main"
              @click="selectNodeGroupFilter(item.key)"
            >
              <span class="node-summary-type" :class="{ subscription: item.isSubscription }">
                {{ item.typeLabel }}
              </span>
              <strong>{{ item.title }}</strong>
              <span class="node-summary-count">{{
                t('home.groupMeta.nodeCount', { count: item.count })
              }}</span>
              <small>{{ item.detail }}</small>
              <span class="node-summary-meta">
                <em>{{ item.strategyLabel }}</em>
                <em v-if="item.filter">{{ item.filter }}</em>
              </span>
            </button>
            <div v-if="item.editable && item.groupId" class="node-group-summary-actions">
              <ActionTooltip :label="t('common.editGroup')">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  class="node-group-summary-action"
                  :aria-label="t('common.editGroup')"
                  @click="openEditGroupById(item.groupId)"
                >
                  <Edit3 class="size-4" aria-hidden="true" />
                </Button>
              </ActionTooltip>
              <ActionTooltip :label="t('common.deleteGroup')">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  class="node-group-summary-action danger"
                  :aria-label="t('common.deleteGroup')"
                  @click="requestRemoveGroup(item.groupId)"
                >
                  <Trash2 class="size-4" aria-hidden="true" />
                </Button>
              </ActionTooltip>
            </div>
          </article>
        </div>

        <p v-else class="node-group-card-section-empty">
          {{ t('home.groupSections.empty') }}
        </p>
      </section>
    </div>

    <p v-if="!hasVisibleGroups" class="node-group-search-empty">
      {{ t('home.groupSections.noMatches') }}
    </p>

    <section class="node-group-subscriptions">
      <header class="node-group-subscriptions-header">
        <div>
          <strong>{{ t('home.sections.subscriptionsTitle') }}</strong>
          <span>{{
            t('home.groupSections.subscriptionCount', { count: subscriptions.length })
          }}</span>
        </div>
        <small>{{ t('home.sections.subscriptionsLead') }}</small>
      </header>

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

      <p v-if="!subscriptions.length" class="node-group-card-section-empty">
        {{ t('home.subscription.empty') }}
      </p>

      <div v-else class="node-table">
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

    <span class="inline-message">{{ importMessage }}</span>
  </section>
</template>
