<script setup lang="ts">
import type { DropdownMenuItemEmits, DropdownMenuItemProps } from 'reka-ui';
import type { HTMLAttributes } from 'vue';
import { computed } from 'vue';
import { DropdownMenuItem as DropdownMenuItemPrimitive, useForwardPropsEmits } from 'reka-ui';
import { cn } from '@/lib/utils';

interface Props extends DropdownMenuItemProps {
  class?: HTMLAttributes['class'];
  inset?: boolean;
  variant?: 'default' | 'destructive';
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'default',
});
const emits = defineEmits<DropdownMenuItemEmits>();

const delegatedProps = computed(() => {
  const { class: _class, inset: _inset, variant: _variant, ...delegated } = props;
  return delegated;
});
const forwarded = useForwardPropsEmits(delegatedProps, emits);

defineOptions({
  name: 'DropdownMenuItem',
});
</script>

<template>
  <DropdownMenuItemPrimitive
    v-bind="forwarded"
    :class="
      cn(
        'relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none transition-colors data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0',
        inset && 'pl-8',
        variant === 'destructive' &&
          'text-destructive data-[highlighted]:bg-destructive/10 data-[highlighted]:text-destructive',
        props.class
      )
    "
  >
    <slot />
  </DropdownMenuItemPrimitive>
</template>
