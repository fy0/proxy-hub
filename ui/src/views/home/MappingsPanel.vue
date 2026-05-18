<script setup lang="ts">
import { Check, Copy, Edit3, Gauge, MoreVertical, Plus, Power, Trash2 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import ActionTooltip from '@/components/ActionTooltip.vue';
import { useI18n } from '@/i18n';
import type { HomeViewContext } from './types';

const { t } = useI18n();
const props = defineProps<{
  context: HomeViewContext;
}>();

const {
  mappings,
  portRuntimeState,
  portEnabledLabel,
  toggleMappingEnabled,
  mappingEndpoint,
  outboundProtocolLabels,
  strategyLabels,
  openEditMappingDialog,
  copyPopoverText,
  copyEndpoint,
  openRouteDialog,
  openMappingTestDialog,
  requestRemoveMapping,
  portFailureReason,
  portStatusTitle,
  portStatusLabel,
  mappingNodes,
  isRouteActionMenuOpen,
  toggleRouteActionMenu,
  isActiveRoute,
  switchMappingRoute,
  openNodeTestDialog,
  requestRemoveRoute,
  openEditGroupDialog,
  protocolLabels,
  nodeHealthTitle,
  routeLatencyLabel,
  routeSuccessLabel,
  routeFailureLabel,
  mappingGroups,
  groupRouteTotalLabel,
  groupRouteAvailableLabel,
  groupRouteLatencyLabel,
  groupRouteHealthTitle,
  openNewMappingDialog,
} = props.context;
</script>

<template>
  <section class="panel port-panel">
    <div class="port-grid">
      <article
        v-for="mapping in mappings"
        :key="mapping.id"
        class="port-card"
        :class="`status-${portRuntimeState(mapping)}`"
      >
        <div class="port-card-header">
          <button
            type="button"
            class="switch-button"
            :class="`status-${portRuntimeState(mapping)}`"
            :aria-pressed="mapping.enabled"
            :aria-label="portEnabledLabel(mapping)"
            :title="portEnabledLabel(mapping)"
            @click="toggleMappingEnabled(mapping)"
          >
            <Power class="size-6" aria-hidden="true" />
          </button>

          <div class="port-title">
            <span>{{ t('home.form.listenAddress') }}</span>
            <strong>{{ mappingEndpoint(mapping) }}</strong>
            <div class="port-summary-row">
              <em>{{ outboundProtocolLabels[mapping.outboundProtocol] }}</em>
              <span class="port-tag">{{ strategyLabels[mapping.strategy] }}</span>
              <span class="port-tag">{{
                mapping.username || mapping.password
                  ? t('common.authConfigured')
                  : t('common.noAuth')
              }}</span>
            </div>
          </div>

          <div class="card-actions port-card-actions">
            <div class="port-action-icons">
              <ActionTooltip :label="t('common.editPort')">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="t('common.editPort')"
                  @click="openEditMappingDialog(mapping)"
                >
                  <Edit3 class="size-4" aria-hidden="true" />
                </Button>
              </ActionTooltip>
              <ActionTooltip :label="copyPopoverText(mapping.id)">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="t('common.copyEndpoint')"
                  @click="copyEndpoint(mapping)"
                >
                  <Copy class="size-4" aria-hidden="true" />
                </Button>
              </ActionTooltip>
              <DropdownMenu>
                <ActionTooltip :label="t('home.aria.moreActions')" wrap>
                  <DropdownMenuTrigger as-child>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      :aria-label="t('home.aria.moreActions')"
                    >
                      <MoreVertical class="size-4" aria-hidden="true" />
                    </Button>
                  </DropdownMenuTrigger>
                </ActionTooltip>
                <DropdownMenuContent align="end" :side-offset="8" class="port-actions-menu">
                  <DropdownMenuItem
                    class="port-actions-menu-item"
                    @select="openRouteDialog(mapping)"
                  >
                    <Plus class="size-4" aria-hidden="true" />
                    <span>{{ t('common.addRoute') }}</span>
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    class="port-actions-menu-item"
                    @select="openMappingTestDialog(mapping)"
                  >
                    <Gauge class="size-4" aria-hidden="true" />
                    <span>{{ t('common.test') }}</span>
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    class="port-actions-menu-item"
                    @select="requestRemoveMapping(mapping)"
                  >
                    <Trash2 class="size-4" aria-hidden="true" />
                    <span>{{ t('common.deletePort') }}</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <ActionTooltip
              :label="portFailureReason(mapping)"
              :disabled="portRuntimeState(mapping) !== 'failed'"
              side="bottom"
              align="end"
            >
              <small
                class="port-status-chip"
                :class="`status-${portRuntimeState(mapping)}`"
                :aria-label="portStatusTitle(mapping)"
                :tabindex="portRuntimeState(mapping) === 'failed' ? 0 : -1"
              >
                <i aria-hidden="true"></i>
                {{ portStatusLabel(mapping) }}
              </small>
            </ActionTooltip>
          </div>
        </div>

        <div class="route-card-grid">
          <article
            v-for="node in mappingNodes(mapping)"
            :key="node.id"
            class="inner-route-card"
            :class="{ active: isActiveRoute(mapping, 'node', node.id) }"
          >
            <div class="route-card-actions" @click.stop>
              <ActionTooltip :label="t('home.aria.moreActions')" align="end">
                <button
                  type="button"
                  class="mini-menu-button"
                  aria-haspopup="menu"
                  :aria-expanded="isRouteActionMenuOpen(mapping, 'node', node.id)"
                  :aria-label="t('home.aria.moreActions')"
                  @click.stop="toggleRouteActionMenu(mapping, 'node', node.id)"
                >
                  <MoreVertical class="size-3" aria-hidden="true" />
                </button>
              </ActionTooltip>
              <div
                v-if="isRouteActionMenuOpen(mapping, 'node', node.id)"
                class="route-action-menu"
                role="menu"
              >
                <button
                  v-if="mapping.strategy === 'manual' && !isActiveRoute(mapping, 'node', node.id)"
                  type="button"
                  class="route-action-menu-item"
                  role="menuitem"
                  @click.stop="switchMappingRoute(mapping, 'node', node.id)"
                >
                  <Check class="size-4" aria-hidden="true" />
                  <span>{{ t('common.setActiveRoute') }}</span>
                </button>
                <button
                  type="button"
                  class="route-action-menu-item"
                  role="menuitem"
                  @click.stop="openNodeTestDialog(node)"
                >
                  <Gauge class="size-4" aria-hidden="true" />
                  <span>{{ t('common.test') }}</span>
                </button>
                <button
                  type="button"
                  class="route-action-menu-item danger"
                  role="menuitem"
                  @click.stop="requestRemoveRoute(mapping, node)"
                >
                  <Trash2 class="size-4" aria-hidden="true" />
                  <span>{{ t('common.removeRoute') }}</span>
                </button>
              </div>
            </div>
            <div class="route-main">
              <strong>{{ node.name }}</strong>
            </div>
            <span class="route-card-meta">
              <span class="route-kind-badge">{{ t('home.routeKind.node') }}</span>
              <span class="route-detail">{{ protocolLabels[node.protocol] }}</span>
            </span>
            <span
              class="route-health"
              :class="{
                blacklisted: node.health?.blacklisted,
                probing: node.health?.probeRunning,
              }"
              :title="nodeHealthTitle(node)"
            >
              <small class="latency" :title="t('home.nodeHealth.latency')">
                {{ routeLatencyLabel(node) }}
              </small>
              <small class="success" :title="t('home.nodeHealth.success')">
                <i aria-hidden="true"></i>
                {{ routeSuccessLabel(node) }}
              </small>
              <small class="failure" :title="t('home.nodeHealth.failure')">
                <i aria-hidden="true"></i>
                {{ routeFailureLabel(node) }}
              </small>
            </span>
          </article>

          <article
            v-for="group in mappingGroups(mapping)"
            :key="group.id"
            class="inner-route-card group-route-card"
            :class="{ active: isActiveRoute(mapping, 'group', group.id) }"
          >
            <div class="route-card-actions" @click.stop>
              <ActionTooltip :label="t('home.aria.moreActions')" align="end">
                <button
                  type="button"
                  class="mini-menu-button"
                  aria-haspopup="menu"
                  :aria-expanded="isRouteActionMenuOpen(mapping, 'group', group.id)"
                  :aria-label="t('home.aria.moreActions')"
                  @click.stop="toggleRouteActionMenu(mapping, 'group', group.id)"
                >
                  <MoreVertical class="size-3" aria-hidden="true" />
                </button>
              </ActionTooltip>
              <div
                v-if="isRouteActionMenuOpen(mapping, 'group', group.id)"
                class="route-action-menu"
                role="menu"
              >
                <button
                  v-if="mapping.strategy === 'manual' && !isActiveRoute(mapping, 'group', group.id)"
                  type="button"
                  class="route-action-menu-item"
                  role="menuitem"
                  @click.stop="switchMappingRoute(mapping, 'group', group.id)"
                >
                  <Check class="size-4" aria-hidden="true" />
                  <span>{{ t('common.setActiveRoute') }}</span>
                </button>
                <button
                  v-if="group.type === 'manual'"
                  type="button"
                  class="route-action-menu-item"
                  role="menuitem"
                  @click.stop="openEditGroupDialog(group)"
                >
                  <Edit3 class="size-4" aria-hidden="true" />
                  <span>{{ t('common.editGroup') }}</span>
                </button>
                <button
                  type="button"
                  class="route-action-menu-item danger"
                  role="menuitem"
                  @click.stop="requestRemoveRoute(mapping, group)"
                >
                  <Trash2 class="size-4" aria-hidden="true" />
                  <span>{{ t('common.removeRoute') }}</span>
                </button>
              </div>
            </div>
            <div class="route-main">
              <strong>{{ group.name }}</strong>
            </div>
            <span class="route-card-meta">
              <span class="route-kind-badge group">{{ t('home.routeKind.group') }}</span>
              <span class="route-detail">{{ t(`home.groupStrategy.${group.strategy}`) }}</span>
            </span>
            <span
              class="route-health group-route-health"
              :title="groupRouteHealthTitle(mapping, group)"
            >
              <small class="total" :title="t('home.nodeGroupHealth.totalTitle')">
                {{ groupRouteTotalLabel(mapping, group) }}
              </small>
              <small class="success" :title="t('home.nodeGroupHealth.availableTitle')">
                <i aria-hidden="true"></i>
                {{ groupRouteAvailableLabel(mapping, group) }}
              </small>
              <small class="latency" :title="t('home.nodeGroupHealth.fastestTitle')">
                {{ groupRouteLatencyLabel(mapping, group) }}
              </small>
            </span>
          </article>

          <button type="button" class="inner-add-card" @click="openRouteDialog(mapping)">
            <Plus class="size-5" aria-hidden="true" />
            <span>{{ t('common.addRoute') }}</span>
          </button>
        </div>

        <div class="port-card-footer">
          <span>{{ mapping.remark || t('common.noRemark') }}</span>
          <ActionTooltip :label="t('common.deletePort')" side="left" align="center">
            <Button
              type="button"
              variant="destructive"
              size="icon"
              class="danger-popover"
              :aria-label="t('common.deletePort')"
              @click="requestRemoveMapping(mapping)"
            >
              <Trash2 class="size-4" aria-hidden="true" />
            </Button>
          </ActionTooltip>
        </div>
      </article>

      <button type="button" class="add-port-card" @click="openNewMappingDialog">
        <Plus class="size-8" aria-hidden="true" />
        <span>{{ t('common.addPort') }}</span>
        <small>{{ t('home.sections.addPortHint') }}</small>
      </button>
    </div>
  </section>
</template>
