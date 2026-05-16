<script setup lang="ts">
import type { DropdownMenuRootEmits, DropdownMenuRootProps } from 'reka-ui';
import { DropdownMenuRoot, useForwardPropsEmits } from 'reka-ui';

const props = withDefaults(defineProps<DropdownMenuRootProps>(), {
  modal: true,
});
const emits = defineEmits<DropdownMenuRootEmits>();

const forwarded = useForwardPropsEmits(props, emits);

defineOptions({
  name: 'DropdownMenu',
});
</script>

<template>
  <DropdownMenuRoot v-bind="forwarded">
    <slot />
  </DropdownMenuRoot>
</template>
