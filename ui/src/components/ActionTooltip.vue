<script setup lang="ts">
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

interface Props {
  label: string;
  disabled?: boolean;
  side?: 'top' | 'right' | 'bottom' | 'left';
  align?: 'start' | 'center' | 'end';
  wrap?: boolean;
}

withDefaults(defineProps<Props>(), {
  disabled: false,
  side: 'top',
  align: 'center',
  wrap: false,
});

defineOptions({
  name: 'ActionTooltip',
});
</script>

<template>
  <TooltipProvider>
    <Tooltip :disabled="disabled || !label">
      <TooltipTrigger v-if="wrap" as-child>
        <span class="action-tooltip-trigger">
          <slot />
        </span>
      </TooltipTrigger>
      <TooltipTrigger v-else as-child>
        <slot />
      </TooltipTrigger>
      <TooltipContent :side="side" :align="align">
        {{ label }}
      </TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>
