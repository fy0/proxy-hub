<script setup lang="ts">
import type { CSSProperties } from 'vue';
import { computed, nextTick, onBeforeUnmount, ref } from 'vue';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

interface Props {
  label: string;
  disabled?: boolean;
  side?: 'top' | 'right' | 'bottom' | 'left';
  align?: 'start' | 'center' | 'end';
  wrap?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  side: 'top',
  align: 'center',
  wrap: false,
});

defineOptions({
  name: 'ActionTooltip',
});

const manualTrigger = ref<HTMLElement | null>(null);
const manualContent = ref<HTMLElement | null>(null);
const manualOpen = ref(false);
const manualStyle = ref<CSSProperties>({});
const manualArrowStyle = ref<CSSProperties>({});
const suppressFocusOpen = ref(false);
let focusSuppressTimer: number | null = null;

const manualVisible = computed(() => props.wrap && manualOpen.value && !props.disabled && !!props.label);

function clearFocusSuppressTimer(): void {
  if (focusSuppressTimer !== null) {
    window.clearTimeout(focusSuppressTimer);
    focusSuppressTimer = null;
  }
}

function updateManualPosition(): void {
  const trigger = manualTrigger.value;
  const content = manualContent.value;
  if (!trigger || !content) return;

  const triggerRect = trigger.getBoundingClientRect();
  const contentRect = content.getBoundingClientRect();
  const gap = 8;
  const viewportPadding = 12;
  const arrowOffset = 12;
  let top = 0;
  let left = 0;

  if (props.side === 'bottom') {
    top = triggerRect.bottom + gap;
  } else if (props.side === 'left') {
    top = triggerRect.top + (triggerRect.height - contentRect.height) / 2;
    left = triggerRect.left - contentRect.width - gap;
  } else if (props.side === 'right') {
    top = triggerRect.top + (triggerRect.height - contentRect.height) / 2;
    left = triggerRect.right + gap;
  } else {
    top = triggerRect.top - contentRect.height - gap;
  }

  if (props.side === 'top' || props.side === 'bottom') {
    if (props.align === 'start') {
      left = triggerRect.left;
    } else if (props.align === 'end') {
      left = triggerRect.right - contentRect.width;
    } else {
      left = triggerRect.left + (triggerRect.width - contentRect.width) / 2;
    }
  }

  left = Math.min(Math.max(left, viewportPadding), window.innerWidth - contentRect.width - viewportPadding);
  top = Math.min(Math.max(top, viewportPadding), window.innerHeight - contentRect.height - viewportPadding);

  manualStyle.value = {
    left: `${left}px`,
    top: `${top}px`,
  };

  const triggerCenterX = triggerRect.left + triggerRect.width / 2;
  const triggerCenterY = triggerRect.top + triggerRect.height / 2;
  if (props.side === 'bottom') {
    manualArrowStyle.value = {
      left: `${Math.min(Math.max(triggerCenterX - left, arrowOffset), contentRect.width - arrowOffset)}px`,
      top: '-4px',
      transform: 'translateX(-50%) rotate(45deg)',
    };
  } else if (props.side === 'left') {
    manualArrowStyle.value = {
      right: '-4px',
      top: `${Math.min(Math.max(triggerCenterY - top, arrowOffset), contentRect.height - arrowOffset)}px`,
      transform: 'translateY(-50%) rotate(45deg)',
    };
  } else if (props.side === 'right') {
    manualArrowStyle.value = {
      left: '-4px',
      top: `${Math.min(Math.max(triggerCenterY - top, arrowOffset), contentRect.height - arrowOffset)}px`,
      transform: 'translateY(-50%) rotate(45deg)',
    };
  } else {
    manualArrowStyle.value = {
      left: `${Math.min(Math.max(triggerCenterX - left, arrowOffset), contentRect.width - arrowOffset)}px`,
      bottom: '-4px',
      transform: 'translateX(-50%) rotate(45deg)',
    };
  }
}

async function openManualTooltip(): Promise<void> {
  if (!props.wrap || props.disabled || !props.label) return;
  manualOpen.value = true;
  await nextTick();
  updateManualPosition();
}

function closeManualTooltip(): void {
  manualOpen.value = false;
}

function handleManualPointerDown(): void {
  closeManualTooltip();
  suppressFocusOpen.value = true;
  clearFocusSuppressTimer();
  focusSuppressTimer = window.setTimeout(() => {
    suppressFocusOpen.value = false;
    focusSuppressTimer = null;
  }, 120);
}

function handleManualFocusIn(): void {
  if (suppressFocusOpen.value) return;
  void openManualTooltip();
}

onBeforeUnmount(() => {
  clearFocusSuppressTimer();
});
</script>

<template>
  <span
    v-if="wrap"
    ref="manualTrigger"
    class="action-tooltip-trigger"
    @pointerenter="openManualTooltip"
    @pointerleave="closeManualTooltip"
    @pointerdown="handleManualPointerDown"
    @focusin="handleManualFocusIn"
    @focusout="closeManualTooltip"
    @keydown.esc="closeManualTooltip"
  >
    <slot />
  </span>
  <Teleport v-if="wrap" to="body">
    <div
      v-if="manualVisible"
      ref="manualContent"
      class="action-tooltip-content action-tooltip-floating"
      :style="manualStyle"
      role="tooltip"
    >
      {{ label }}
      <span class="action-tooltip-floating-arrow" :style="manualArrowStyle" aria-hidden="true"></span>
    </div>
  </Teleport>

  <TooltipProvider v-if="!wrap">
    <Tooltip :disabled="disabled || !label">
      <TooltipTrigger as-child>
        <slot />
      </TooltipTrigger>
      <TooltipContent :side="side" :align="align">
        {{ label }}
      </TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>
