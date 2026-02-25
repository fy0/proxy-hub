<script setup lang="ts">
import { computed } from 'vue';
import { useQuery, useQueryClient } from '@tanstack/vue-query';
import { getHealthOptions, getHealthQueryKey } from '@/api';

const queryClient = useQueryClient();
const { data, isLoading, isError, error, isFetching } = useQuery(getHealthOptions());

const message = computed(() => data.value?.message ?? '');

const refresh = () => {
  queryClient.invalidateQueries({ queryKey: getHealthQueryKey() });
};
</script>

<template>
  <main class="home">
    <h1>Health Check</h1>

    <p v-if="isLoading">Loading...</p>
    <p v-else-if="isError">
      Error: {{ error?.message ?? 'Request failed' }}
    </p>
    <p v-else>API: {{ message || 'ok' }}</p>

    <button type="button" @click="refresh" :disabled="isFetching">
      {{ isFetching ? 'Refreshing…' : 'Refresh' }}
    </button>
  </main>
</template>

<style scoped>
.home {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 24px;
}

button {
  width: fit-content;
  padding: 6px 12px;
  border-radius: 6px;
  border: 1px solid #d1d5db;
  background: #ffffff;
  cursor: pointer;
}

button[disabled] {
  opacity: 0.6;
  cursor: not-allowed;
}
</style>
