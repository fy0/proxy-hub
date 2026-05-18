<script setup lang="ts">
import { Edit3 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import ActionTooltip from '@/components/ActionTooltip.vue';
import { useI18n } from '@/i18n';
import type { HomeViewContext } from './types';

const { t } = useI18n();
const props = defineProps<{
  context: HomeViewContext;
}>();

const { groupSummaryItems, selectNodeGroupFilter, openEditGroupById, importMessage } =
  props.context;
</script>

<template>
  <section class="panel simple-panel">
    <div class="node-group-summary-grid">
      <article v-for="item in groupSummaryItems" :key="item.key" class="node-group-summary-card">
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
        <ActionTooltip v-if="item.editable && item.groupId" :label="t('common.editGroup')">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="node-group-summary-edit"
            :aria-label="t('common.editGroup')"
            @click="openEditGroupById(item.groupId)"
          >
            <Edit3 class="size-4" aria-hidden="true" />
          </Button>
        </ActionTooltip>
      </article>
    </div>

    <span class="inline-message">{{ importMessage }}</span>
  </section>
</template>
