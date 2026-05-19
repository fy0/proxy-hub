<script setup lang="ts">
import { computed } from 'vue';
import { Check, ChevronDown } from 'lucide-vue-next';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

interface NodeGroupFilterSelectOption {
  id: string;
  label: string;
}

const props = defineProps<{
  modelValue: string;
  options: NodeGroupFilterSelectOption[];
  ariaLabel?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();

const selectedLabel = computed(() => {
  return (
    props.options.find(option => option.id === props.modelValue)?.label ??
    props.options[0]?.label ??
    ''
  );
});

function selectOption(value: string): void {
  if (value !== props.modelValue) emit('update:modelValue', value);
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <button
        type="button"
        class="node-group-filter-select"
        :aria-label="ariaLabel ?? selectedLabel"
        :title="selectedLabel"
      >
        <span>{{ selectedLabel }}</span>
        <ChevronDown class="size-4" aria-hidden="true" />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" :side-offset="4" class="node-group-filter-select-menu">
      <DropdownMenuItem
        v-for="option in options"
        :key="option.id"
        class="node-group-filter-select-item"
        :class="{ selected: option.id === modelValue }"
        @select="selectOption(option.id)"
      >
        <span :title="option.label">{{ option.label }}</span>
        <Check v-if="option.id === modelValue" class="size-4" aria-hidden="true" />
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
