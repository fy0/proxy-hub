<script setup lang="ts">
import { onMounted } from 'vue';
import { RouterView } from 'vue-router';
import { useAppStore } from '@/stores/app';

const appStore = useAppStore();

onMounted(() => {
  void appStore.loadAppInfo().catch(error => {
    console.error('Failed to load app version:', error);
  });
  void appStore.loadUpdateInfo().catch(error => {
    console.error('Failed to load update info:', error);
  });
});
</script>

<template>
  <RouterView v-slot="{ Component }">
    <Transition name="route-fade" mode="out-in" appear>
      <component :is="Component" />
    </Transition>
  </RouterView>
</template>
