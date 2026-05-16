<script setup lang="ts">
import type { TooltipTriggerProps } from 'reka-ui';
import { TooltipTrigger as TooltipTriggerPrimitive, useForwardProps } from 'reka-ui';

const props = defineProps<TooltipTriggerProps>();
const forwarded = useForwardProps(props);

defineOptions({
  name: 'TooltipTrigger',
});
</script>

<template>
  <TooltipTriggerPrimitive v-bind="forwarded">
    <slot />
  </TooltipTriggerPrimitive>
</template>
