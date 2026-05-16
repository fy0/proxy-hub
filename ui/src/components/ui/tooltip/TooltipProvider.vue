<script setup lang="ts">
import type { TooltipProviderProps } from 'reka-ui';
import { TooltipProvider as TooltipProviderPrimitive, useForwardProps } from 'reka-ui';

const props = withDefaults(defineProps<TooltipProviderProps>(), {
  delayDuration: 250,
  skipDelayDuration: 200,
});

const forwarded = useForwardProps(props);

defineOptions({
  name: 'TooltipProvider',
});
</script>

<template>
  <TooltipProviderPrimitive v-bind="forwarded">
    <slot />
  </TooltipProviderPrimitive>
</template>
