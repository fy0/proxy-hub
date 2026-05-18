<script setup lang="ts">
import { Copy, Edit3, Gauge, Trash2 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import ActionTooltip from '@/components/ActionTooltip.vue';
import { useI18n } from '@/i18n';
import type { HomeViewContext } from './types';

const { t } = useI18n();
const props = defineProps<{
  context: HomeViewContext;
}>();

const {
  nodeSearch,
  hideEmptyNodeGroups,
  nodeGroupFilterOptions,
  activeNodeGroupFilter,
  selectNodeGroupFilter,
  selectedGroup,
  selectedNodeGroupTitle,
  selectedNodeGroupHealthSummary,
  currentNodeTotal,
  selectedNodeGroupNodes,
  nodeListContainerProps,
  nodeListWrapperProps,
  virtualNodeRows,
  protocolLabels,
  nodeEndpointLabel,
  nodeUriPopoverText,
  nodeExportUri,
  copyNodeUri,
  openEditNodeDialog,
  openNodeTestDialog,
  requestRemoveNode,
  routeLatencyLabel,
  routeSuccessLabel,
  routeFailureLabel,
  nodeHealthTitle,
  isProbeUnavailableNode,
  nodeBlacklistLabel,
  isLoadingNodes,
  loadNextNodePage,
  openEditGroupDialog,
  importMessage,
} = props.context;
</script>

<template>
  <section class="panel simple-panel">
    <div class="node-search-row">
      <label class="node-search-field">
        <span>{{ t('home.form.searchNodes') }}</span>
        <input
          v-model.trim="nodeSearch"
          type="search"
          autocomplete="off"
          :placeholder="t('home.placeholders.nodeSearch')"
        />
      </label>
    </div>
    <div class="node-filter-bar" :aria-label="t('home.aria.nodeGroupFilters')">
      <label class="node-empty-toggle">
        <input v-model="hideEmptyNodeGroups" type="checkbox" />
        <span>{{ t('home.groupFilters.hideEmpty') }}</span>
      </label>
      <button
        v-for="option in nodeGroupFilterOptions"
        :key="option.key"
        type="button"
        :class="{ active: activeNodeGroupFilter === option.key }"
        @click="selectNodeGroupFilter(option.key)"
      >
        <span>{{ option.label }}</span>
        <small>{{ option.countLabel }}</small>
      </button>
    </div>

    <section class="node-group-section active-node-group">
      <div class="node-group-heading">
        <div class="node-group-title">
          <strong>{{ selectedNodeGroupTitle }}</strong>
          <span>{{ t('home.groupMeta.nodeCount', { count: currentNodeTotal }) }}</span>
        </div>
        <div class="node-group-health-summary">
          <span class="available">
            {{
              t('home.nodeGroupHealth.available', {
                count: selectedNodeGroupHealthSummary.available,
              })
            }}
          </span>
          <span class="fastest">
            {{
              t('home.nodeGroupHealth.fastest', {
                latency: selectedNodeGroupHealthSummary.fastestLatencyMs
                  ? `${selectedNodeGroupHealthSummary.fastestLatencyMs}ms`
                  : '-',
              })
            }}
          </span>
          <span
            class="probing"
            :class="{
              active:
                selectedNodeGroupHealthSummary.probing > 0 ||
                selectedNodeGroupHealthSummary.autoProbeRunning,
            }"
          >
            {{
              t('home.nodeGroupHealth.probing', { count: selectedNodeGroupHealthSummary.probing })
            }}
          </span>
          <span
            class="probe-state"
            :class="{
              active: selectedNodeGroupHealthSummary.autoProbeEnabled,
              running: selectedNodeGroupHealthSummary.autoProbeRunning,
            }"
          >
            {{
              selectedNodeGroupHealthSummary.autoProbeRunning
                ? t('home.nodeGroupHealth.autoProbeRunning')
                : selectedNodeGroupHealthSummary.autoProbeEnabled
                  ? t('home.nodeGroupHealth.autoProbeEnabled')
                  : t('home.nodeGroupHealth.autoProbeDisabled')
            }}
          </span>
          <span v-if="selectedNodeGroupHealthSummary.autoProbeEnabled" class="needs-probe">
            {{
              t('home.nodeGroupHealth.needsProbe', {
                count: selectedNodeGroupHealthSummary.needsProbe,
              })
            }}
          </span>
          <span class="unavailable">
            {{
              t('home.nodeGroupHealth.unavailable', {
                count: selectedNodeGroupHealthSummary.unavailable,
              })
            }}
          </span>
          <ActionTooltip
            v-if="selectedGroup?.type === 'manual'"
            :label="t('common.editGroup')"
            align="end"
          >
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :aria-label="t('common.editGroup')"
              @click="openEditGroupDialog(selectedGroup)"
            >
              <Edit3 class="size-4" aria-hidden="true" />
            </Button>
          </ActionTooltip>
        </div>
      </div>
      <div v-if="selectedNodeGroupNodes.length" class="node-table virtual-node-table">
        <div v-bind="nodeListContainerProps" class="node-virtual-scroll">
          <div v-bind="nodeListWrapperProps">
            <article
              v-for="row in virtualNodeRows"
              :key="row.data.id"
              class="node-row"
              :class="{
                blacklisted: row.data.health?.blacklisted,
                'probe-unavailable': isProbeUnavailableNode(row.data),
              }"
              :style="{ height: '116px' }"
            >
              <div class="node-protocol" :class="{ chain: row.data.protocol === 'chain' }">
                {{ protocolLabels[row.data.protocol] }}
              </div>
              <div class="node-main">
                <strong>{{ row.data.name }}</strong>
                <span>{{ nodeEndpointLabel(row.data) }}</span>
              </div>
              <div class="node-row-actions">
                <ActionTooltip :label="nodeUriPopoverText(row.data)">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    :aria-label="t('common.exportNodeUri')"
                    :disabled="!nodeExportUri(row.data)"
                    @click="copyNodeUri(row.data)"
                  >
                    <Copy class="size-4" aria-hidden="true" />
                  </Button>
                </ActionTooltip>
                <ActionTooltip :label="t('common.editNode')">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    :aria-label="t('common.editNode')"
                    @click="openEditNodeDialog(row.data)"
                  >
                    <Edit3 class="size-4" aria-hidden="true" />
                  </Button>
                </ActionTooltip>
                <ActionTooltip :label="t('common.test')">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    :aria-label="t('common.test')"
                    @click="openNodeTestDialog(row.data)"
                  >
                    <Gauge class="size-4" aria-hidden="true" />
                  </Button>
                </ActionTooltip>
                <ActionTooltip :label="t('common.deleteNode')">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    :aria-label="t('common.deleteNode')"
                    @click="requestRemoveNode(row.data)"
                  >
                    <Trash2 class="size-4" aria-hidden="true" />
                  </Button>
                </ActionTooltip>
              </div>
              <div class="node-meta">
                <small v-if="row.data.protocol === 'chain'">{{
                  t('home.nodeMeta.chainCount', { count: row.data.chainNodeIds.length })
                }}</small>
                <small v-if="row.data.username">{{
                  t('home.nodeMeta.username', { value: row.data.username })
                }}</small>
                <small v-if="row.data.password">{{ t('home.nodeMeta.passwordConfigured') }}</small>
                <small
                  v-if="row.data.protocol !== 'chain' && !row.data.username && !row.data.password"
                  >{{ t('common.noAccount') }}</small
                >
                <span
                  class="node-health-strip"
                  :class="{
                    blacklisted: row.data.health?.blacklisted,
                    'probe-unavailable': isProbeUnavailableNode(row.data),
                    probing: row.data.health?.probeRunning,
                  }"
                >
                  <small class="latency" :title="t('home.nodeHealth.latency')">
                    {{ routeLatencyLabel(row.data) }}
                  </small>
                  <small class="success" :title="t('home.nodeHealth.success')">
                    <i aria-hidden="true"></i>
                    {{ routeSuccessLabel(row.data) }}
                  </small>
                  <small class="failure" :title="t('home.nodeHealth.failure')">
                    <i aria-hidden="true"></i>
                    {{ routeFailureLabel(row.data) }}
                  </small>
                </span>
                <ActionTooltip
                  v-if="row.data.health?.blacklisted"
                  :label="nodeHealthTitle(row.data)"
                  side="bottom"
                  align="start"
                  wrap
                >
                  <small class="blacklisted" tabindex="0">{{ nodeBlacklistLabel(row.data) }}</small>
                </ActionTooltip>
                <ActionTooltip
                  v-else-if="isProbeUnavailableNode(row.data)"
                  :label="nodeHealthTitle(row.data)"
                  side="bottom"
                  align="start"
                  wrap
                >
                  <small class="probe-unavailable" tabindex="0">{{
                    t('home.nodeHealth.unavailable')
                  }}</small>
                </ActionTooltip>
              </div>
            </article>
          </div>
        </div>
        <Button
          v-if="selectedNodeGroupNodes.length < currentNodeTotal"
          type="button"
          variant="outline"
          :disabled="isLoadingNodes"
          @click="loadNextNodePage"
        >
          {{ isLoadingNodes ? t('home.messages.loadingNodes') : t('home.actions.loadMore') }}
        </Button>
      </div>
      <p v-else class="empty-node-state">
        {{
          isLoadingNodes
            ? t('home.messages.loadingNodes')
            : t('home.groupFilters.emptyNodes', { name: selectedNodeGroupTitle })
        }}
      </p>
    </section>

    <span class="inline-message">{{ importMessage }}</span>
  </section>
</template>
