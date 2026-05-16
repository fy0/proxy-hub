<script setup lang="ts">
import type { DropdownMenuTriggerProps } from 'reka-ui';
import { DropdownMenuTrigger as DropdownMenuTriggerPrimitive, useForwardProps } from 'reka-ui';

const props = defineProps<DropdownMenuTriggerProps>();
const forwarded = useForwardProps(props);

defineOptions({
  name: 'DropdownMenuTrigger',
});
</script>

<template>
  <DropdownMenuTriggerPrimitive v-bind="forwarded">
    <slot />
  </DropdownMenuTriggerPrimitive>
</template>
