<script setup lang="ts">
import { Copy, Edit3, Gauge, Plus, Route, Trash2, X } from 'lucide-vue-next';
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
  groupSummaryItems,
  selectedNodeGroupTitle,
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
  nodeBlacklistLabel,
  isLoadingNodes,
  loadNextNodePage,
  chainNodeForm,
  groups,
  chainNodeSearch,
  chainNodeGroupId,
  groupFilterOptions,
  chainNodeOptions,
  toggleChainNodeSelection,
  optionProtocolLabel,
  optionNameLabel,
  optionEndpointLabel,
  chainNodeTotal,
  isLoadingChainNodes,
  loadMoreChainOptions,
  chainNodeFormPreview,
  selectedChainNodes,
  removeChainNodeSelection,
  handleChainNodeSubmit,
  importMessage,
  manualGroupForm,
  manualGroupNodeSearch,
  manualGroupNodeGroupId,
  manualGroupNodeOptions,
  toggleManualGroupNode,
  manualGroupNodeTotal,
  isLoadingManualGroupNodes,
  loadMoreManualGroupNodeOptions,
  selectedManualGroupNodes,
  manualGroups,
  handleManualGroupSubmit,
  groupSummary,
  removeGroup,
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

    <div v-if="activeNodeGroupFilter === 'summary'" class="node-group-summary-grid">
      <button
        v-for="item in groupSummaryItems"
        :key="item.key"
        type="button"
        class="node-group-summary-card"
        @click="selectNodeGroupFilter(item.key)"
      >
        <span class="node-summary-type" :class="{ subscription: item.isSubscription }">
          {{ item.typeLabel }}
        </span>
        <strong>{{ item.title }}</strong>
        <span class="node-summary-count">{{
          t('home.groupMeta.nodeCount', { count: item.count })
        }}</span>
        <small>{{ item.detail }}</small>
        <span class="node-summary-meta">
          <em>{{ item.strategyLabel }}</em>
          <em v-if="item.filter">{{ item.filter }}</em>
        </span>
      </button>
    </div>

    <section v-else class="node-group-section active-node-group">
      <div class="node-group-heading">
        <strong>{{ selectedNodeGroupTitle }}</strong>
        <span>{{ t('home.groupMeta.nodeCount', { count: currentNodeTotal }) }}</span>
      </div>
      <div v-if="selectedNodeGroupNodes.length" class="node-table virtual-node-table">
        <div v-bind="nodeListContainerProps" class="node-virtual-scroll">
          <div v-bind="nodeListWrapperProps">
            <article
              v-for="row in virtualNodeRows"
              :key="row.data.id"
              class="node-row"
              :class="{ blacklisted: row.data.health?.blacklisted }"
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
                  :class="{ blacklisted: row.data.health?.blacklisted }"
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

    <div class="panel-heading sub-heading">
      <div>
        <h2>{{ t('home.sections.chainTitle') }}</h2>
        <p>{{ t('home.sections.chainLead') }}</p>
      </div>
      <Route class="panel-icon" aria-hidden="true" />
    </div>

    <form class="manual-node-form chain-node-form" @submit.prevent="handleChainNodeSubmit">
      <div class="field-grid two">
        <label>
          <span>{{ t('home.form.chainName') }}</span>
          <input
            v-model.trim="chainNodeForm.name"
            type="text"
            :placeholder="t('home.placeholders.chainName')"
            required
          />
        </label>
        <label>
          <span>{{ t('home.form.nodeGroup') }}</span>
          <select v-model="chainNodeForm.groupId">
            <option value="">{{ t('home.groupMeta.ungrouped') }}</option>
            <option v-for="group in groups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select>
        </label>
      </div>

      <div class="chain-node-builder">
        <fieldset>
          <span>{{ t('home.form.chainNodes') }}</span>
          <div class="node-option-toolbar">
            <input
              v-model.trim="chainNodeSearch"
              type="search"
              autocomplete="off"
              :placeholder="t('home.placeholders.nodeSearch')"
            />
            <select v-model="chainNodeGroupId">
              <option v-for="group in groupFilterOptions()" :key="group.id" :value="group.id">
                {{ group.label }}
              </option>
            </select>
          </div>
          <div class="chain-node-options">
            <label
              v-for="node in chainNodeOptions"
              :key="node.id"
              :class="{ selected: chainNodeForm.chainNodeIds.includes(node.id) }"
            >
              <input
                type="checkbox"
                :checked="chainNodeForm.chainNodeIds.includes(node.id)"
                @change="toggleChainNodeSelection(node.id)"
              />
              <span class="node-option-card">
                <em>{{ optionProtocolLabel(node) }}</em>
                <strong>{{ optionNameLabel(node) }}</strong>
                <small>{{ optionEndpointLabel(node) }}</small>
              </span>
            </label>
            <Button
              v-if="chainNodeOptions.length < chainNodeTotal"
              type="button"
              variant="outline"
              :disabled="isLoadingChainNodes"
              @click="loadMoreChainOptions"
            >
              {{
                isLoadingChainNodes ? t('home.messages.loadingNodes') : t('home.actions.loadMore')
              }}
            </Button>
          </div>
        </fieldset>
        <div class="chain-node-preview">
          <strong>{{ t('home.form.chainPreview') }}</strong>
          <span v-if="chainNodeForm.chainNodeIds.length">{{ chainNodeFormPreview() }}</span>
          <span v-else>{{ t('home.nodeMeta.chainEmpty') }}</span>
          <div v-if="chainNodeForm.chainNodeIds.length" class="chain-node-order">
            <button
              v-for="(node, index) in selectedChainNodes()"
              :key="node.id"
              type="button"
              @click="removeChainNodeSelection(node.id)"
            >
              <em>{{ index + 1 }}</em>
              <span>{{ node.name }}</span>
              <X class="size-3" aria-hidden="true" />
            </button>
          </div>
        </div>
      </div>

      <label>
        <span>{{ t('home.form.remark') }}</span>
        <input
          v-model.trim="chainNodeForm.remark"
          type="text"
          :placeholder="t('common.optional')"
        />
      </label>

      <Button type="submit">
        <Route class="size-4" aria-hidden="true" />
        {{ t('common.addChainNode') }}
      </Button>
    </form>
    <span class="inline-message">{{ importMessage }}</span>

    <div class="panel-heading sub-heading">
      <div>
        <h2>{{ t('home.sections.groupsTitle') }}</h2>
        <p>{{ t('home.sections.groupsLead') }}</p>
      </div>
    </div>

    <form class="manual-node-form" @submit.prevent="handleManualGroupSubmit">
      <div class="field-grid two">
        <label>
          <span>{{ t('home.form.groupName') }}</span>
          <input v-model.trim="manualGroupForm.name" type="text" required />
        </label>
        <label>
          <span>{{ t('home.form.groupStrategy') }}</span>
          <select v-model="manualGroupForm.strategy">
            <option value="selector">{{ t('home.groupStrategy.selector') }}</option>
            <option value="url-test">{{ t('home.groupStrategy.url-test') }}</option>
          </select>
        </label>
      </div>
      <div class="field-grid two">
        <label>
          <span>{{ t('home.form.groupNodes') }}</span>
          <div class="node-picker-box">
            <div class="node-option-toolbar">
              <input
                v-model.trim="manualGroupNodeSearch"
                type="search"
                autocomplete="off"
                :placeholder="t('home.placeholders.nodeSearch')"
              />
              <select v-model="manualGroupNodeGroupId">
                <option v-for="group in groupFilterOptions()" :key="group.id" :value="group.id">
                  {{ group.label }}
                </option>
              </select>
            </div>
            <div class="chain-node-options compact">
              <label
                v-for="node in manualGroupNodeOptions"
                :key="node.id"
                :class="{ selected: manualGroupForm.nodeIds.includes(node.id) }"
              >
                <input
                  type="checkbox"
                  :checked="manualGroupForm.nodeIds.includes(node.id)"
                  @change="toggleManualGroupNode(node.id)"
                />
                <span class="node-option-card">
                  <em>{{ optionProtocolLabel(node) }}</em>
                  <strong>{{ optionNameLabel(node) }}</strong>
                  <small>{{ optionEndpointLabel(node) }}</small>
                </span>
              </label>
              <Button
                v-if="manualGroupNodeOptions.length < manualGroupNodeTotal"
                type="button"
                variant="outline"
                :disabled="isLoadingManualGroupNodes"
                @click="loadMoreManualGroupNodeOptions"
              >
                {{
                  isLoadingManualGroupNodes
                    ? t('home.messages.loadingNodes')
                    : t('home.actions.loadMore')
                }}
              </Button>
            </div>
            <div v-if="manualGroupForm.nodeIds.length" class="chain-node-order">
              <button
                v-for="node in selectedManualGroupNodes()"
                :key="node.id"
                type="button"
                @click="toggleManualGroupNode(node.id)"
              >
                <span>{{ node.name }}</span>
                <X class="size-3" aria-hidden="true" />
              </button>
            </div>
          </div>
        </label>
        <label>
          <span>{{ t('home.form.groupGroups') }}</span>
          <select v-model="manualGroupForm.groupIds" multiple>
            <option v-for="group in manualGroups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select>
        </label>
      </div>
      <label>
        <span>{{ t('home.form.remark') }}</span>
        <input
          v-model.trim="manualGroupForm.remark"
          type="text"
          :placeholder="t('common.optional')"
        />
      </label>
      <Button type="submit">
        <Plus class="size-4" aria-hidden="true" />
        {{ t('common.addGroup') }}
      </Button>
    </form>

    <div class="node-table">
      <article v-for="group in groups" :key="group.id" class="node-row group-row">
        <div class="node-protocol">{{ t(`home.groupType.${group.type}`) }}</div>
        <div class="node-main">
          <strong>{{ group.name }}</strong>
          <span>{{ groupSummary(group) }}</span>
        </div>
        <Button
          v-if="group.type === 'manual'"
          type="button"
          variant="ghost"
          size="icon-sm"
          :title="t('common.deleteGroup')"
          @click="removeGroup(group.id).catch(() => undefined)"
        >
          <Trash2 class="size-4" aria-hidden="true" />
        </Button>
        <div class="node-meta">
          <small>{{ t(`home.groupStrategy.${group.strategy}`) }}</small>
          <small v-if="group.filter">{{ group.filter }}</small>
        </div>
      </article>
    </div>
  </section>
</template>
