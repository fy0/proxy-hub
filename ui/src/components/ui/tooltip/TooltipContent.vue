<script setup lang="ts">
import type { TooltipContentEmits, TooltipContentProps } from 'reka-ui';
import type { HTMLAttributes } from 'vue';
import { computed } from 'vue';
import {
  TooltipArrow,
  TooltipContent as TooltipContentPrimitive,
  TooltipPortal,
  useForwardPropsEmits,
} from 'reka-ui';
import { cn } from '@/lib/utils';

interface Props extends TooltipContentProps {
  class?: HTMLAttributes['class'];
  withArrow?: boolean;
  arrowClass?: HTMLAttributes['class'];
}

const props = withDefaults(defineProps<Props>(), {
  sideOffset: 8,
  align: 'center',
  withArrow: true,
});
const emits = defineEmits<TooltipContentEmits>();

const delegatedProps = computed(() => {
  const { class: _class, arrowClass: _arrowClass, withArrow: _withArrow, ...delegated } = props;
  return delegated;
});
const forwarded = useForwardPropsEmits(delegatedProps, emits);

defineOptions({
  name: 'TooltipContent',
});
</script>

<template>
  <TooltipPortal>
    <TooltipContentPrimitive v-bind="forwarded" :class="cn('action-tooltip-content', props.class)">
      <slot />
      <TooltipArrow
        v-if="withArrow"
        :width="12"
        :height="6"
        :class="cn('action-tooltip-arrow', props.arrowClass)"
      />
    </TooltipContentPrimitive>
  </TooltipPortal>
</template>
