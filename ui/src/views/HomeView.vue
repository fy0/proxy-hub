<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import {
  Check,
  Copy,
  Database,
  Edit3,
  FileInput,
  Import,
  Info,
  Layers3,
  Link2,
  MoreVertical,
  Plus,
  Power,
  RefreshCw,
  Server,
  Trash2,
  Users,
  X,
} from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { inferNodeNameFromUri, useProxyHubState } from '@/composables/useProxyHubState';
import { useI18n } from '@/i18n';
import type {
  OutboundProtocol,
  PortMapping,
  ProxyNode,
  ProxyProtocol,
  RouteStrategy,
} from '@/types/proxyHub';
import './home.css';

type TabKey = 'overview' | 'mappings' | 'nodes' | 'import';

const { formatDateTime, t } = useI18n();

const protocolLabels = computed<Record<ProxyProtocol, string>>(() => ({
  vless: t('home.protocol.vless'),
  vmess: t('home.protocol.vmess'),
  trojan: t('home.protocol.trojan'),
  socks5: t('home.protocol.socks5'),
  http: t('home.protocol.http'),
  unknown: t('home.protocol.unknown'),
}));

const outboundProtocolLabels = computed<Record<OutboundProtocol, string>>(() => ({
  mixed: t('home.outbound.mixed'),
  socks5: t('home.outbound.socks5'),
  http: t('home.outbound.http'),
}));

const strategyLabels = computed<Record<RouteStrategy, string>>(() => ({
  failover: t('home.strategy.failover'),
  'load-balance': t('home.strategy.loadBalance'),
  manual: t('home.strategy.manual'),
}));

const strategyOptions = computed<Array<{ label: string; value: RouteStrategy }>>(() => [
  { label: strategyLabels.value.failover, value: 'failover' },
  { label: strategyLabels.value['load-balance'], value: 'load-balance' },
  { label: strategyLabels.value.manual, value: 'manual' },
]);

const {
  nodes,
  mappings,
  enabledMappings,
  nodeById,
  lastSavedAt,
  addNode,
  addNodeFromUri,
  removeNode,
  addMapping,
  updateMapping,
  removeMapping,
  resetDemoData,
} = useProxyHubState();

const currentTab = ref<TabKey>('mappings');
const rawImport = ref('');
const importMessage = ref('');
const copyMessage = ref('');
const copyMessageTimer = ref<number | null>(null);
const copiedMappingId = ref<string | null>(null);
const editingMappingId = ref<string | null>(null);
const routeTargetMappingId = ref<string | null>(null);

const emptyMappingForm = () => ({
  listenAddress: '0.0.0.0',
  listenPort: 1080,
  outboundProtocol: 'mixed' as OutboundProtocol,
  username: '',
  password: '',
  strategy: 'failover' as RouteStrategy,
  remark: '',
});

const mappingForm = reactive(emptyMappingForm());
const routeNodeForm = reactive({
  name: '',
  uri: '',
});
const routeNodeNameEdited = ref(false);
const routeNodeError = ref('');

const manualNodeForm = reactive({
  name: '',
  protocol: 'socks5' as ProxyProtocol,
  server: '',
  port: 1080,
  username: '',
  password: '',
  tags: '',
  remark: '',
});

const isMappingDialogOpen = computed(() => editingMappingId.value !== null);
const routeTargetMapping = computed(() =>
  routeTargetMappingId.value
    ? (mappings.value.find(mapping => mapping.id === routeTargetMappingId.value) ?? null)
    : null
);

const activePorts = computed(() =>
  enabledMappings.value
    .map(mapping => `${mapping.listenAddress}:${mapping.listenPort}`)
    .sort((a, b) => a.localeCompare(b))
);

const formattedLastSaved = computed(() => {
  if (!lastSavedAt.value) return t('common.notSaved');

  return formatDateTime(lastSavedAt.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
});

const overviewCards = computed(() => [
  { label: t('home.status.enabledPorts'), value: enabledMappings.value.length, icon: Power },
  { label: t('home.status.nodeCount'), value: nodes.value.length, icon: Server },
  { label: t('home.status.mappingCount'), value: mappings.value.length, icon: Layers3 },
  { label: t('home.status.lastSaved'), value: formattedLastSaved.value, icon: Database },
]);

function resetMappingForm(): void {
  Object.assign(mappingForm, emptyMappingForm());
}

function mappingNodes(mapping: PortMapping): ProxyNode[] {
  return mapping.nodeIds
    .map(id => nodeById.value.get(id))
    .filter((node): node is ProxyNode => Boolean(node));
}

function openNewMappingDialog(): void {
  resetMappingForm();
  const usedPorts = new Set(mappings.value.map(mapping => mapping.listenPort));
  let nextPort = 1080;
  while (usedPorts.has(nextPort)) nextPort += 1;
  mappingForm.listenPort = nextPort;
  editingMappingId.value = 'new';
}

function openEditMappingDialog(mapping: PortMapping): void {
  Object.assign(mappingForm, {
    listenAddress: mapping.listenAddress,
    listenPort: mapping.listenPort,
    outboundProtocol: mapping.outboundProtocol,
    username: mapping.username,
    password: mapping.password,
    strategy: mapping.strategy,
    remark: mapping.remark,
  });
  editingMappingId.value = mapping.id;
}

function closeMappingDialog(): void {
  editingMappingId.value = null;
  resetMappingForm();
}

function saveMappingDialog(): void {
  if (editingMappingId.value === 'new') {
    const mapping = addMapping({
      listenAddress: mappingForm.listenAddress,
      listenPort: mappingForm.listenPort,
      outboundProtocol: mappingForm.outboundProtocol,
      username: mappingForm.username,
      password: mappingForm.password,
      strategy: mappingForm.strategy,
      nodeIds: [],
      activeNodeId: null,
      enabled: true,
      remark: mappingForm.remark,
    });

    closeMappingDialog();
    openRouteDialog(mapping);
    return;
  }

  if (editingMappingId.value) {
    updateMapping(editingMappingId.value, {
      listenAddress: mappingForm.listenAddress,
      listenPort: mappingForm.listenPort,
      outboundProtocol: mappingForm.outboundProtocol,
      username: mappingForm.username,
      password: mappingForm.password,
      strategy: mappingForm.strategy,
      remark: mappingForm.remark,
    });
  }

  closeMappingDialog();
}

function openRouteDialog(mapping: PortMapping): void {
  routeNodeForm.name = '';
  routeNodeForm.uri = '';
  routeNodeNameEdited.value = false;
  routeNodeError.value = '';
  routeTargetMappingId.value = mapping.id;
}

watch(
  () => routeNodeForm.uri,
  uri => {
    routeNodeError.value = '';
    if (routeNodeNameEdited.value) return;

    routeNodeForm.name = inferNodeNameFromUri(uri);
  }
);

function closeRouteDialog(): void {
  routeTargetMappingId.value = null;
  routeNodeForm.name = '';
  routeNodeForm.uri = '';
  routeNodeNameEdited.value = false;
  routeNodeError.value = '';
}

function saveRouteDialog(): void {
  const mapping = routeTargetMapping.value;
  if (!mapping) return;

  if (!routeNodeForm.uri.trim()) {
    routeNodeError.value = t('home.messages.routeNodeRequired');
    return;
  }

  const node = addNodeFromUri(routeNodeForm.uri, routeNodeForm.name);
  const nodeIds = Array.from(new Set([...mapping.nodeIds, node.id]));

  updateMapping(mapping.id, {
    nodeIds,
    activeNodeId: mapping.activeNodeId || node.id,
  });
  closeRouteDialog();
}

function handleRouteNodeNameInput(): void {
  routeNodeNameEdited.value = true;
}

function removeNodeFromMapping(mapping: PortMapping, nodeId: string): void {
  const nodeIds = mapping.nodeIds.filter(id => id !== nodeId);
  const activeNodeId = mapping.activeNodeId === nodeId ? nodeIds[0] || null : mapping.activeNodeId;
  updateMapping(mapping.id, { nodeIds, activeNodeId });
}

function copyPopoverText(mappingId: string): string {
  return copiedMappingId.value === mappingId
    ? t('common.copiedEndpoint')
    : t('common.copyEndpoint');
}

function handleImport(): void {
  const lines = rawImport.value
    .split(/\r?\n/)
    .map(line => line.trim())
    .filter(Boolean);

  if (!lines.length) {
    importMessage.value = t('home.messages.importEmpty');
    return;
  }

  const added = lines.map(line => addNodeFromUri(line));
  rawImport.value = '';
  importMessage.value = t('home.messages.imported', { count: added.length });
}

function handleManualNodeSubmit(): void {
  const node = addNode({
    name: manualNodeForm.name,
    protocol: manualNodeForm.protocol,
    server: manualNodeForm.server,
    port: manualNodeForm.port,
    username: manualNodeForm.username,
    password: manualNodeForm.password,
    rawUri: '',
    tags: manualNodeForm.tags
      .split(',')
      .map(tag => tag.trim())
      .filter(Boolean),
    remark: manualNodeForm.remark,
  });

  manualNodeForm.name = '';
  manualNodeForm.server = '';
  manualNodeForm.port = 1080;
  manualNodeForm.username = '';
  manualNodeForm.password = '';
  manualNodeForm.tags = '';
  manualNodeForm.remark = '';
  importMessage.value = t('home.messages.nodeAdded', { name: node.name });
}

function toggleMappingEnabled(mapping: PortMapping): void {
  updateMapping(mapping.id, { enabled: !mapping.enabled });
}

async function copyEndpoint(mapping: PortMapping): Promise<void> {
  const endpoint =
    mapping.outboundProtocol === 'mixed'
      ? `mixed://${mapping.listenAddress}:${mapping.listenPort}`
      : `${mapping.outboundProtocol}://${mapping.listenAddress}:${mapping.listenPort}`;

  try {
    await navigator.clipboard.writeText(endpoint);
    copyMessage.value = t('home.messages.endpointCopied', { endpoint });
    copiedMappingId.value = mapping.id;
  } catch {
    copyMessage.value = endpoint;
    copiedMappingId.value = mapping.id;
  }

  if (copyMessageTimer.value !== null) {
    window.clearTimeout(copyMessageTimer.value);
  }

  copyMessageTimer.value = window.setTimeout(() => {
    copyMessage.value = '';
    copiedMappingId.value = null;
    copyMessageTimer.value = null;
  }, 2200);
}

function handleReset(): void {
  resetDemoData();
  importMessage.value = t('home.messages.demoReset');
}

function openTab(tab: TabKey): void {
  currentTab.value = tab;
}

function portEnabledLabel(mapping: PortMapping): string {
  return mapping.enabled ? t('home.aria.disablePort') : t('home.aria.enablePort');
}
</script>

<template>
  <main class="console-shell">
    <section class="hero-strip">
      <div>
        <div class="eyebrow">
          <Link2 class="icon-sm" aria-hidden="true" />
          {{ t('app.name') }}
        </div>
        <h1>{{ t('home.hero.title') }}</h1>
        <p>{{ t('home.hero.lead') }}</p>
      </div>

      <div class="hero-actions">
        <Button type="button" variant="outline" class="restore-button" @click="handleReset">
          <RefreshCw class="size-4" aria-hidden="true" />
          {{ t('common.restoreDemo') }}
        </Button>
      </div>
    </section>

    <section class="status-grid" :aria-label="t('home.aria.statusOverview')">
      <article v-for="card in overviewCards" :key="card.label" class="metric-card">
        <div class="metric-icon">
          <component :is="card.icon" class="size-4" aria-hidden="true" />
        </div>
        <div>
          <span>{{ card.label }}</span>
          <strong>{{ card.value }}</strong>
        </div>
      </article>
    </section>

    <section class="notice-bar" role="status">
      <Info class="size-4" aria-hidden="true" />
      {{ t('home.notice') }}
    </section>

    <nav class="tab-bar" :aria-label="t('home.aria.tabs')">
      <button
        :class="{ active: currentTab === 'mappings' }"
        type="button"
        @click="openTab('mappings')"
      >
        {{ t('home.tabs.mappings') }}
      </button>
      <button
        :class="{ active: currentTab === 'overview' }"
        type="button"
        @click="openTab('overview')"
      >
        {{ t('home.tabs.overview') }}
      </button>
      <button :class="{ active: currentTab === 'nodes' }" type="button" @click="openTab('nodes')">
        {{ t('home.tabs.nodes') }}
      </button>
      <button :class="{ active: currentTab === 'import' }" type="button" @click="openTab('import')">
        {{ t('home.tabs.import') }}
      </button>
    </nav>

    <section v-if="currentTab === 'mappings'" class="panel port-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('home.sections.mappingsTitle') }}</h2>
          <p>{{ t('home.sections.mappingsLead') }}</p>
        </div>
        <div class="port-strip" :aria-label="t('home.aria.activePorts')">
          <span v-for="endpoint in activePorts" :key="endpoint">
            <i aria-hidden="true"></i>
            {{ endpoint }}
          </span>
        </div>
      </div>

      <div class="port-grid">
        <article
          v-for="mapping in mappings"
          :key="mapping.id"
          class="port-card"
          :class="{ disabled: !mapping.enabled }"
        >
          <div class="port-card-header">
            <button
              type="button"
              class="switch-button"
              :aria-pressed="mapping.enabled"
              :aria-label="portEnabledLabel(mapping)"
              :title="portEnabledLabel(mapping)"
              @click="toggleMappingEnabled(mapping)"
            >
              <Power class="size-4" aria-hidden="true" />
            </button>

            <div class="port-title">
              <strong>{{ mapping.listenPort }}</strong>
              <span
                >{{ mapping.listenAddress }} ·
                {{ outboundProtocolLabels[mapping.outboundProtocol] }}</span
              >
            </div>

            <div class="card-actions">
              <span class="action-popover" :data-popover="t('common.editPort')">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="t('common.editPort')"
                  @click="openEditMappingDialog(mapping)"
                >
                  <Edit3 class="size-4" aria-hidden="true" />
                </Button>
              </span>
              <span class="action-popover" :data-popover="copyPopoverText(mapping.id)">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="t('common.copyEndpoint')"
                  @click="copyEndpoint(mapping)"
                >
                  <Copy class="size-4" aria-hidden="true" />
                </Button>
              </span>
              <span class="action-popover" :data-popover="t('home.aria.moreActions')">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="t('home.aria.moreActions')"
                >
                  <MoreVertical class="size-4" aria-hidden="true" />
                </Button>
              </span>
            </div>
          </div>

          <div class="port-meta">
            <span>{{ strategyLabels[mapping.strategy] }}</span>
            <span>{{
              mapping.username || mapping.password ? t('common.authConfigured') : t('common.noAuth')
            }}</span>
          </div>

          <div class="route-card-grid">
            <article v-for="node in mappingNodes(mapping)" :key="node.id" class="inner-route-card">
              <span class="action-popover mini-action" :data-popover="t('common.removeRoute')">
                <button
                  type="button"
                  class="mini-remove"
                  :aria-label="t('home.aria.removeFromPort')"
                  @click="removeNodeFromMapping(mapping, node.id)"
                >
                  <MoreVertical class="size-3" aria-hidden="true" />
                </button>
              </span>
              <strong>{{ node.name }}</strong>
              <span
                >{{ protocolLabels[node.protocol] }} · {{ node.server }}:{{
                  node.port ?? '-'
                }}</span
              >
            </article>

            <button type="button" class="inner-add-card" @click="openRouteDialog(mapping)">
              <Plus class="size-5" aria-hidden="true" />
              <span>{{ t('common.addRoute') }}</span>
            </button>
          </div>

          <div class="port-card-footer">
            <span>{{ mapping.remark || t('common.noRemark') }}</span>
            <span class="action-popover danger-popover" :data-popover="t('common.deletePort')">
              <Button
                type="button"
                variant="destructive"
                size="icon"
                :aria-label="t('common.deletePort')"
                @click="removeMapping(mapping.id)"
              >
                <Trash2 class="size-4" aria-hidden="true" />
              </Button>
            </span>
          </div>
        </article>

        <button type="button" class="add-port-card" @click="openNewMappingDialog">
          <Plus class="size-8" aria-hidden="true" />
          <span>{{ t('common.addPort') }}</span>
          <small>{{ t('home.sections.addPortHint') }}</small>
        </button>
      </div>
    </section>

    <section v-else-if="currentTab === 'overview'" class="panel simple-panel">
      <div class="simple-grid">
        <article class="quick-card">
          <h2>{{ t('home.overview.firstTitle') }}</h2>
          <p>{{ t('home.overview.firstBody') }}</p>
        </article>
        <article class="quick-card">
          <h2>{{ t('home.overview.secondTitle') }}</h2>
          <p>{{ t('home.overview.secondBody') }}</p>
        </article>
        <article class="quick-card">
          <h2>{{ t('home.overview.authTitle') }}</h2>
          <p>{{ t('home.overview.authBody') }}</p>
        </article>
      </div>
    </section>

    <section v-else-if="currentTab === 'nodes'" class="panel simple-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('home.sections.nodesTitle') }}</h2>
          <p>{{ t('home.sections.nodesLead') }}</p>
        </div>
        <Users class="panel-icon" aria-hidden="true" />
      </div>

      <div class="node-table">
        <article v-for="node in nodes" :key="node.id" class="node-row">
          <div class="node-protocol">{{ protocolLabels[node.protocol] }}</div>
          <div class="node-main">
            <strong>{{ node.name }}</strong>
            <span>{{ node.server }}:{{ node.port ?? '-' }}</span>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            :title="t('common.deleteNode')"
            @click="removeNode(node.id)"
          >
            <Trash2 class="size-4" aria-hidden="true" />
          </Button>
          <div class="node-meta">
            <small v-if="node.username">{{
              t('home.nodeMeta.username', { value: node.username })
            }}</small>
            <small v-if="node.password">{{ t('home.nodeMeta.passwordConfigured') }}</small>
            <small v-if="!node.username && !node.password">{{ t('common.noAccount') }}</small>
          </div>
        </article>
      </div>
    </section>

    <section v-else class="panel simple-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('home.sections.importTitle') }}</h2>
          <p>{{ t('home.sections.importLead') }}</p>
        </div>
        <FileInput class="panel-icon" aria-hidden="true" />
      </div>

      <div class="import-layout">
        <label>
          <span>{{ t('home.form.shareLink') }}</span>
          <textarea
            v-model="rawImport"
            rows="5"
            :placeholder="t('home.placeholders.shareLinks')"
          ></textarea>
        </label>

        <form class="manual-node-form" @submit.prevent="handleManualNodeSubmit">
          <div class="field-grid two">
            <label>
              <span>{{ t('home.form.name') }}</span>
              <input v-model.trim="manualNodeForm.name" type="text" required />
            </label>
            <label>
              <span>{{ t('home.form.protocol') }}</span>
              <select v-model="manualNodeForm.protocol">
                <option value="socks5">{{ protocolLabels.socks5 }}</option>
                <option value="http">{{ protocolLabels.http }}</option>
                <option value="vless">{{ protocolLabels.vless }}</option>
                <option value="vmess">{{ protocolLabels.vmess }}</option>
                <option value="trojan">{{ protocolLabels.trojan }}</option>
              </select>
            </label>
          </div>

          <div class="field-grid two">
            <label>
              <span>{{ t('home.form.server') }}</span>
              <input v-model.trim="manualNodeForm.server" type="text" required />
            </label>
            <label>
              <span>{{ t('home.form.port') }}</span>
              <input
                v-model.number="manualNodeForm.port"
                type="number"
                min="1"
                max="65535"
                required
              />
            </label>
          </div>

          <div class="field-grid two">
            <label>
              <span>{{ t('home.form.username') }}</span>
              <input
                v-model.trim="manualNodeForm.username"
                type="text"
                :placeholder="t('common.optional')"
              />
            </label>
            <label>
              <span>{{ t('home.form.password') }}</span>
              <input
                v-model.trim="manualNodeForm.password"
                type="password"
                :placeholder="t('common.optional')"
              />
            </label>
          </div>

          <label>
            <span>{{ t('home.form.remark') }}</span>
            <input
              v-model.trim="manualNodeForm.remark"
              type="text"
              :placeholder="t('common.optional')"
            />
          </label>

          <Button type="submit">
            <Plus class="size-4" aria-hidden="true" />
            {{ t('common.addNode') }}
          </Button>
        </form>
      </div>

      <div class="button-row">
        <Button type="button" @click="handleImport">
          <Import class="size-4" aria-hidden="true" />
          {{ t('common.importLinks') }}
        </Button>
        <span class="inline-message">{{ importMessage }}</span>
      </div>
    </section>

    <div
      v-if="isMappingDialogOpen"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeMappingDialog"
    >
      <form
        class="modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="mapping-dialog-title"
        @submit.prevent="saveMappingDialog"
      >
        <div class="modal-heading">
          <div>
            <h2 id="mapping-dialog-title">
              {{
                editingMappingId === 'new'
                  ? t('home.dialogs.addPortTitle')
                  : t('home.dialogs.editPortTitle')
              }}
            </h2>
            <p>{{ t('home.dialogs.mappingLead') }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeMappingDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.listenAddress') }}</span>
            <input v-model.trim="mappingForm.listenAddress" type="text" autocomplete="off" />
          </label>
          <label>
            <span>{{ t('home.form.port') }}</span>
            <input
              v-model.number="mappingForm.listenPort"
              type="number"
              min="1"
              max="65535"
              required
            />
          </label>
        </div>

        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.outboundType') }}</span>
            <select v-model="mappingForm.outboundProtocol">
              <option value="mixed">{{ outboundProtocolLabels.mixed }}</option>
              <option value="socks5">{{ outboundProtocolLabels.socks5 }}</option>
              <option value="http">{{ outboundProtocolLabels.http }}</option>
            </select>
          </label>
          <label>
            <span>{{ t('home.form.strategy') }}</span>
            <select v-model="mappingForm.strategy">
              <option v-for="option in strategyOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>
        </div>

        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.username') }}</span>
            <input
              v-model.trim="mappingForm.username"
              type="text"
              :placeholder="t('common.optional')"
            />
          </label>
          <label>
            <span>{{ t('home.form.password') }}</span>
            <input
              v-model.trim="mappingForm.password"
              type="password"
              :placeholder="t('common.optional')"
            />
          </label>
        </div>

        <label>
          <span>{{ t('home.form.remark') }}</span>
          <input
            v-model.trim="mappingForm.remark"
            type="text"
            :placeholder="t('common.optional')"
          />
        </label>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeMappingDialog">{{
            t('common.cancel')
          }}</Button>
          <Button type="submit">
            <Check class="size-4" aria-hidden="true" />
            {{ t('common.save') }}
          </Button>
        </div>
      </form>
    </div>

    <div
      v-if="routeTargetMapping"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeRouteDialog"
    >
      <form
        class="modal-card route-node-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="route-dialog-title"
        @submit.prevent="saveRouteDialog"
      >
        <div class="modal-heading">
          <div>
            <h2 id="route-dialog-title">{{ t('home.dialogs.addNodeTitle') }}</h2>
            <p>{{ t('home.dialogs.addNodeLead') }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeRouteDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <div class="route-node-fields">
          <label>
            <span>{{ t('home.form.nodeName') }}</span>
            <input
              v-model.trim="routeNodeForm.name"
              type="text"
              autocomplete="off"
              :placeholder="t('home.placeholders.nodeName')"
              @input="handleRouteNodeNameInput"
            />
          </label>

          <label>
            <span>{{ t('home.form.nodeUri') }} <em class="required-mark">*</em></span>
            <input
              v-model.trim="routeNodeForm.uri"
              type="text"
              required
              autocomplete="off"
              :placeholder="t('home.placeholders.nodeUri')"
            />
          </label>
        </div>

        <p v-if="routeNodeError" class="route-node-error">{{ routeNodeError }}</p>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeRouteDialog">{{
            t('common.cancel')
          }}</Button>
          <Button type="submit">
            <Plus class="size-4" aria-hidden="true" />
            {{ t('common.add') }}
          </Button>
        </div>
      </form>
    </div>

    <p v-if="copyMessage" class="toast-message" role="status">{{ copyMessage }}</p>
  </main>
</template>
