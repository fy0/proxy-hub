<script setup lang="ts">
import type { DropdownMenuSeparatorProps } from 'reka-ui';
import type { HTMLAttributes } from 'vue';
import { computed } from 'vue';
import { DropdownMenuSeparator as DropdownMenuSeparatorPrimitive, useForwardProps } from 'reka-ui';
import { cn } from '@/lib/utils';

interface Props extends DropdownMenuSeparatorProps {
  class?: HTMLAttributes['class'];
}

const props = defineProps<Props>();

const delegatedProps = computed(() => {
  const { class: _class, ...delegated } = props;
  return delegated;
});
const forwarded = useForwardProps(delegatedProps);

defineOptions({
  name: 'DropdownMenuSeparator',
});
</script>

<template>
  <DropdownMenuSeparatorPrimitive
    v-bind="forwarded"
    :class="cn('-mx-1 my-1 h-px bg-border', props.class)"
  />
</template>
