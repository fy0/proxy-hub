<script setup lang="ts">
import { FolderTree, Link2, RefreshCw, Server } from 'lucide-vue-next';
import { useI18n } from '@/i18n';
import type { TabKey } from './types';

const { t } = useI18n();

defineProps<{
  currentTab: TabKey;
  nodesLabel: string;
}>();

const emit = defineEmits<{
  select: [tab: TabKey];
}>();
</script>

<template>
  <nav class="tab-bar" :aria-label="t('home.aria.tabs')">
    <button
      :class="{ active: currentTab === 'mappings' }"
      type="button"
      @click="emit('select', 'mappings')"
    >
      <Link2 class="size-4" aria-hidden="true" />
      <span>{{ t('home.tabs.mappings') }}</span>
    </button>
    <button
      :class="{ active: currentTab === 'nodes' }"
      type="button"
      @click="emit('select', 'nodes')"
    >
      <Server class="size-4" aria-hidden="true" />
      <span>{{ nodesLabel }}</span>
    </button>
    <button
      :class="{ active: currentTab === 'groups' }"
      type="button"
      @click="emit('select', 'groups')"
    >
      <FolderTree class="size-4" aria-hidden="true" />
      <span>{{ t('home.tabs.groups') }}</span>
    </button>
    <button
      :class="{ active: currentTab === 'subscriptions' }"
      type="button"
      @click="emit('select', 'subscriptions')"
    >
      <RefreshCw class="size-4" aria-hidden="true" />
      <span>{{ t('home.tabs.subscriptions') }}</span>
    </button>
  </nav>
</template>
