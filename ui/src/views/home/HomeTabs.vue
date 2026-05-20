<script setup lang="ts">
import { FolderTree, Link2, Server } from 'lucide-vue-next';
import { useI18n } from '@/i18n';
import type { TabKey } from './types';

const { t } = useI18n();

defineProps<{
  currentTab: TabKey;
  groupsLabel: string;
  nodesLabel: string;
  compactGroupsLabel: string;
  compactNodesLabel: string;
  compactMappingsLabel: string;
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
      <span class="tab-label tab-label-full">{{ t('home.tabs.mappings') }}</span>
      <span class="tab-label tab-label-compact">{{ compactMappingsLabel }}</span>
    </button>
    <button
      :class="{ active: currentTab === 'nodes' }"
      type="button"
      @click="emit('select', 'nodes')"
    >
      <Server class="size-4" aria-hidden="true" />
      <span class="tab-label tab-label-full">{{ nodesLabel }}</span>
      <span class="tab-label tab-label-compact">{{ compactNodesLabel }}</span>
    </button>
    <button
      :class="{ active: currentTab === 'groups' }"
      type="button"
      @click="emit('select', 'groups')"
    >
      <FolderTree class="size-4" aria-hidden="true" />
      <span class="tab-label tab-label-full">{{ groupsLabel }}</span>
      <span class="tab-label tab-label-compact">{{ compactGroupsLabel }}</span>
    </button>
  </nav>
</template>
