<script setup lang="ts">
import { onMounted } from 'vue';
import { RouterView } from 'vue-router';
import { useAppStore } from '@/stores/app';

const appStore = useAppStore();

onMounted(() => {
  void appStore.loadAppInfo().catch(error => {
    console.error('Failed to load app version:', error);
  });
});
</script>

<template>
  <RouterView />
</template>
