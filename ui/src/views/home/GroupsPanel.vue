<script setup lang="ts">
import { computed, ref } from 'vue';
import { Edit3, Search, Trash2 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import ActionTooltip from '@/components/ActionTooltip.vue';
import { useI18n } from '@/i18n';
import type { HomeViewContext, NodeGroupSummaryItem } from './types';
import type { ProxySubscription } from '@/types/proxyHub';

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

function subscriptionSectionTitle(subscription: ProxySubscription | undefined): string {
  return subscription?.name || t('home.groupSections.unknownSubscription');
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
      title: subscriptionSectionTitle(undefined),
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

    <span class="inline-message">{{ importMessage }}</span>
  </section>
</template>
