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
  ProxyGroup,
  ProxyNode,
  ProxyProtocol,
  RouteStrategy,
} from '@/types/proxyHub';
import './home.css';

type TabKey = 'mappings' | 'nodes' | 'subscriptions' | 'import';
type NodeGroupFilterKey = 'all' | 'summary' | 'default' | `group:${string}`;
type PortRuntimeState = 'running' | 'failed' | 'closed' | 'notRunning';

interface NodeGroupFilterOption {
  key: NodeGroupFilterKey;
  label: string;
  countLabel: string;
}

interface NodeGroupSummaryItem {
  key: NodeGroupFilterKey;
  title: string;
  typeLabel: string;
  count: number;
  detail: string;
  strategyLabel: string;
  filter: string;
  isSubscription: boolean;
}

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
  groups,
  subscriptions,
  mappings,
  enabledMappings,
  nodeById,
  groupById,
  lastSavedAt,
  runtime,
  isLoading,
  isSaving,
  errorMessage,
  loginRequired,
  refreshState,
  addNode,
  addNodeFromUri,
  importNodes,
  removeNode,
  addGroup,
  removeGroup,
  addSubscription,
  syncSubscription,
  removeSubscription,
  addMapping,
  updateMapping,
  removeMapping,
} = useProxyHubState();

const currentTab = ref<TabKey>('mappings');
const activeNodeGroupFilter = ref<NodeGroupFilterKey>('all');
const hideEmptyNodeGroups = ref(false);
const rawImport = ref('');
const rawImportGroupId = ref('');
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
  existingNodeId: '',
  groupId: '',
  mode: 'uri' as 'uri' | 'node' | 'group',
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
  groupId: '',
  remark: '',
});

const subscriptionForm = reactive({
  name: '',
  url: '',
  groupId: '',
  remark: '',
});

const manualGroupForm = reactive({
  name: '',
  strategy: 'selector' as 'selector' | 'url-test',
  nodeIds: [] as string[],
  groupIds: [] as string[],
  remark: '',
});

function toGroupFilterKey(groupId: string): NodeGroupFilterKey {
  return `group:${groupId}` as NodeGroupFilterKey;
}

function groupIdFromFilterKey(key: NodeGroupFilterKey): string {
  return key.startsWith('group:') ? key.slice('group:'.length) : '';
}

const isMappingDialogOpen = computed(() => editingMappingId.value !== null);
const routeTargetMapping = computed(() =>
  routeTargetMappingId.value
    ? (mappings.value.find(mapping => mapping.id === routeTargetMappingId.value) ?? null)
    : null
);

const runtimeInboundsByMappingId = computed(
  () => new Map((runtime.value?.inbounds ?? []).map(inbound => [inbound.mappingId, inbound]))
);

const runtimeFailuresByMappingId = computed(
  () => new Map((runtime.value?.failures ?? []).map(failure => [failure.mappingId, failure]))
);

const portStripItems = computed(() =>
  [
    ...(runtime.value?.inbounds ?? []).map(inbound => ({
      key: `running-${inbound.mappingId}`,
      label: inbound.listen,
      state: 'running' as const,
      title: t('home.portState.running'),
    })),
    ...(runtime.value?.failures ?? []).map(failure => ({
      key: `failed-${failure.mappingId}`,
      label: failure.listen,
      state: 'failed' as const,
      title: failure.error,
    })),
  ].sort((a, b) => a.label.localeCompare(b.label))
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

const runtimeInboundCount = computed(() => runtime.value?.inbounds?.length ?? 0);
const runtimeFailureCount = computed(() => runtime.value?.failures?.length ?? 0);
const hasNoticeError = computed(
  () =>
    Boolean(errorMessage.value) || Boolean(runtime.value?.error) || runtimeFailureCount.value > 0
);

const backendNotice = computed(() => {
  if (errorMessage.value) return errorMessage.value;
  if (isLoading.value) return t('home.messages.loadingBackend');
  if (isSaving.value) return t('home.messages.savingBackend');
  if (runtime.value?.failures?.length) {
    return t('home.messages.runtimeFailures', {
      count: runtime.value.failures.length,
      ports: runtime.value.failures.map(failure => failure.listen).join(', '),
    });
  }
  if (runtime.value?.error)
    return t('home.messages.runtimeError', { message: runtime.value.error });
  if (runtime.value?.running) {
    return t('home.messages.runtimeRunning', { count: runtimeInboundCount.value });
  }
  if (runtime.value) return t('home.messages.runtimeStopped');

  return t('home.notice');
});

const loginRoute = computed(() => ({
  name: 'login',
  query: {
    redirect: '/',
  },
}));

const overviewCards = computed(() => [
  { label: t('home.status.enabledPorts'), value: enabledMappings.value.length, icon: Power },
  { label: t('home.status.nodeCount'), value: nodes.value.length, icon: Server },
  { label: t('home.status.groupCount'), value: groups.value.length, icon: Users },
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

function mappingGroups(mapping: PortMapping): ProxyGroup[] {
  return mapping.groupIds
    .map(id => groupById.value.get(id))
    .filter((group): group is ProxyGroup => Boolean(group));
}

const groupedNodeIds = computed(() => {
  const ids = new Set<string>();
  for (const group of groups.value) {
    for (const nodeId of group.nodeIds) ids.add(nodeId);
  }
  return ids;
});

const defaultNodes = computed(() =>
  nodes.value.filter(node => !node.groupId && !groupedNodeIds.value.has(node.id))
);
const manualGroups = computed(() => groups.value.filter(group => group.type === 'manual'));
const visibleGroups = computed(() =>
  hideEmptyNodeGroups.value
    ? groups.value.filter(group => groupNodes(group).length > 0)
    : groups.value
);

const nodeGroupFilterOptions = computed<NodeGroupFilterOption[]>(() => [
  {
    key: 'all',
    label: t('home.groupFilters.all'),
    countLabel: t('home.groupMeta.nodeCount', { count: nodes.value.length }),
  },
  {
    key: 'summary',
    label: t('home.groupFilters.summary'),
    countLabel: t('home.groupMeta.groupCount', { count: visibleGroups.value.length + 1 }),
  },
  {
    key: 'default',
    label: t('home.groupFilters.default'),
    countLabel: t('home.groupMeta.nodeCount', { count: defaultNodes.value.length }),
  },
  ...visibleGroups.value.map(group => ({
    key: toGroupFilterKey(group.id),
    label:
      group.type === 'subscription'
        ? t('home.groupFilters.subscriptionLabel', { name: group.name })
        : group.name,
    countLabel: t('home.groupMeta.nodeCount', { count: groupNodes(group).length }),
  })),
]);

const selectedGroup = computed(() => {
  const groupId = groupIdFromFilterKey(activeNodeGroupFilter.value);
  return groupId ? (groupById.value.get(groupId) ?? null) : null;
});

const selectedNodeGroupTitle = computed(() => {
  if (activeNodeGroupFilter.value === 'default') return t('home.groupFilters.default');
  return selectedGroup.value?.name ?? t('home.groupFilters.all');
});

const selectedNodeGroupNodes = computed(() => {
  if (activeNodeGroupFilter.value === 'all') return nodes.value;
  if (activeNodeGroupFilter.value === 'default') return defaultNodes.value;
  if (selectedGroup.value) return groupNodes(selectedGroup.value);

  return [];
});

const groupSummaryItems = computed<NodeGroupSummaryItem[]>(() => [
  {
    key: 'default',
    title: t('home.groupFilters.default'),
    typeLabel: t('home.groupFilters.virtual'),
    count: defaultNodes.value.length,
    detail: t('home.groupFilters.defaultDetail'),
    strategyLabel: t('home.groupFilters.defaultStrategy'),
    filter: '',
    isSubscription: false,
  },
  ...visibleGroups.value.map(group => ({
    key: toGroupFilterKey(group.id),
    title: group.name,
    typeLabel: t(`home.groupType.${group.type}`),
    count: groupNodes(group).length,
    detail: groupSummary(group),
    strategyLabel: t(`home.groupStrategy.${group.strategy}`),
    filter: group.filter,
    isSubscription: group.type === 'subscription',
  })),
]);

watch(
  () => nodeGroupFilterOptions.value.map(option => option.key).join('|'),
  () => {
    if (!nodeGroupFilterOptions.value.some(option => option.key === activeNodeGroupFilter.value)) {
      activeNodeGroupFilter.value = 'all';
    }
  }
);

function groupNodes(group: ProxyGroup): ProxyNode[] {
  return nodes.value.filter(node => node.groupId === group.id || group.nodeIds.includes(node.id));
}

function selectNodeGroupFilter(key: NodeGroupFilterKey): void {
  activeNodeGroupFilter.value = key;
}

function groupSummary(group: ProxyGroup): string {
  const nodeCount = group.nodeIds.length;
  const groupCount = group.groupIds.length;
  const builtins = group.builtinTags.length;
  return t('home.groupMeta.summary', { nodeCount, groupCount, builtins });
}

function subscriptionGroupName(groupId: string): string {
  return groupById.value.get(groupId)?.name || t('home.subscription.noGroup');
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

async function saveMappingDialog(): Promise<void> {
  try {
    if (editingMappingId.value === 'new') {
      const mapping = await addMapping({
        listenAddress: mappingForm.listenAddress,
        listenPort: mappingForm.listenPort,
        outboundProtocol: mappingForm.outboundProtocol,
        username: mappingForm.username,
        password: mappingForm.password,
        strategy: mappingForm.strategy,
        nodeIds: [],
        activeNodeId: null,
        groupIds: [],
        activeGroupId: null,
        enabled: true,
        remark: mappingForm.remark,
      });

      closeMappingDialog();
      openRouteDialog(mapping);
      return;
    }

    if (editingMappingId.value) {
      await updateMapping(editingMappingId.value, {
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
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

function openRouteDialog(mapping: PortMapping): void {
  routeNodeForm.name = '';
  routeNodeForm.uri = '';
  routeNodeForm.existingNodeId = nodes.value[0]?.id ?? '';
  routeNodeForm.groupId = groups.value[0]?.id ?? '';
  routeNodeForm.mode = groups.value.length > 0 ? 'group' : nodes.value.length > 0 ? 'node' : 'uri';
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
  routeNodeForm.existingNodeId = '';
  routeNodeForm.groupId = '';
  routeNodeForm.mode = 'uri';
  routeNodeNameEdited.value = false;
  routeNodeError.value = '';
}

async function saveRouteDialog(): Promise<void> {
  const mapping = routeTargetMapping.value;
  if (!mapping) return;

  if (routeNodeForm.mode === 'uri' && !routeNodeForm.uri.trim()) {
    routeNodeError.value = t('home.messages.routeNodeRequired');
    return;
  }
  if (routeNodeForm.mode === 'node' && !routeNodeForm.existingNodeId) {
    routeNodeError.value = t('home.messages.routeExistingRequired');
    return;
  }
  if (routeNodeForm.mode === 'group' && !routeNodeForm.groupId) {
    routeNodeError.value = t('home.messages.routeGroupRequired');
    return;
  }

  try {
    if (routeNodeForm.mode === 'uri') {
      const node = await addNodeFromUri(routeNodeForm.uri, routeNodeForm.name);
      const nodeIds = Array.from(new Set([...mapping.nodeIds, node.id]));

      await updateMapping(mapping.id, {
        nodeIds,
        activeNodeId: mapping.activeNodeId || node.id,
      });
    } else if (routeNodeForm.mode === 'node') {
      const nodeIds = Array.from(new Set([...mapping.nodeIds, routeNodeForm.existingNodeId]));
      await updateMapping(mapping.id, {
        nodeIds,
        activeNodeId: mapping.activeNodeId || routeNodeForm.existingNodeId,
      });
    } else {
      const groupIds = Array.from(new Set([...mapping.groupIds, routeNodeForm.groupId]));
      await updateMapping(mapping.id, {
        groupIds,
        activeGroupId: mapping.activeGroupId || routeNodeForm.groupId,
      });
    }
    closeRouteDialog();
  } catch (error) {
    routeNodeError.value =
      error instanceof Error ? error.message : t('home.messages.requestFailed');
  }
}

function handleRouteNodeNameInput(): void {
  routeNodeNameEdited.value = true;
}

async function removeNodeFromMapping(mapping: PortMapping, nodeId: string): Promise<void> {
  const nodeIds = mapping.nodeIds.filter(id => id !== nodeId);
  const activeNodeId = mapping.activeNodeId === nodeId ? nodeIds[0] || null : mapping.activeNodeId;

  try {
    await updateMapping(mapping.id, { nodeIds, activeNodeId });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function removeGroupFromMapping(mapping: PortMapping, groupId: string): Promise<void> {
  const groupIds = mapping.groupIds.filter(id => id !== groupId);
  const activeGroupId =
    mapping.activeGroupId === groupId ? groupIds[0] || null : mapping.activeGroupId;

  try {
    await updateMapping(mapping.id, { groupIds, activeGroupId });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

function copyPopoverText(mappingId: string): string {
  return copiedMappingId.value === mappingId
    ? t('common.copiedEndpoint')
    : t('common.copyEndpoint');
}

async function handleImport(): Promise<void> {
  const lines = rawImport.value
    .split(/\r?\n/)
    .map(line => line.trim())
    .filter(Boolean);

  if (!lines.length) {
    importMessage.value = t('home.messages.importEmpty');
    return;
  }

  try {
    const added = await importNodes(lines.join('\n'), rawImportGroupId.value);
    if (added.length > 0) rawImport.value = '';
    importMessage.value = t('home.messages.imported', { count: added.length });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function handleManualNodeSubmit(): Promise<void> {
  try {
    const node = await addNode({
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
      groupId: manualNodeForm.groupId,
      remark: manualNodeForm.remark,
    });

    manualNodeForm.name = '';
    manualNodeForm.server = '';
    manualNodeForm.port = 1080;
    manualNodeForm.username = '';
    manualNodeForm.password = '';
    manualNodeForm.tags = '';
    manualNodeForm.groupId = '';
    manualNodeForm.remark = '';
    importMessage.value = t('home.messages.nodeAdded', { name: node.name });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function handleSubscriptionSubmit(): Promise<void> {
  if (!subscriptionForm.url.trim()) {
    importMessage.value = t('home.messages.subscriptionUrlRequired');
    return;
  }

  try {
    const subscription = await addSubscription({
      name: subscriptionForm.name,
      url: subscriptionForm.url,
      groupId: subscriptionForm.groupId,
      remark: subscriptionForm.remark,
    });
    await syncSubscription(subscription.id);
    subscriptionForm.name = '';
    subscriptionForm.url = '';
    subscriptionForm.groupId = '';
    subscriptionForm.remark = '';
    importMessage.value = t('home.messages.subscriptionSynced');
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function syncExistingSubscription(id: string): Promise<void> {
  try {
    await syncSubscription(id);
    importMessage.value = t('home.messages.subscriptionSynced');
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function handleManualGroupSubmit(): Promise<void> {
  if (!manualGroupForm.name.trim()) {
    importMessage.value = t('home.messages.groupNameRequired');
    return;
  }

  try {
    const group = await addGroup({
      name: manualGroupForm.name,
      strategy: manualGroupForm.strategy,
      nodeIds: manualGroupForm.nodeIds,
      groupIds: manualGroupForm.groupIds,
      remark: manualGroupForm.remark,
    });
    manualGroupForm.name = '';
    manualGroupForm.strategy = 'selector';
    manualGroupForm.nodeIds = [];
    manualGroupForm.groupIds = [];
    manualGroupForm.remark = '';
    importMessage.value = t('home.messages.groupAdded', { name: group.name });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function toggleMappingEnabled(mapping: PortMapping): Promise<void> {
  try {
    await updateMapping(mapping.id, { enabled: !mapping.enabled });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
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

async function handleReset(): Promise<void> {
  try {
    await refreshState();
    importMessage.value = t('home.messages.backendRefreshed');
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

function openTab(tab: TabKey): void {
  currentTab.value = tab;
}

function portEnabledLabel(mapping: PortMapping): string {
  return mapping.enabled ? t('home.aria.disablePort') : t('home.aria.enablePort');
}

function portRuntimeState(mapping: PortMapping): PortRuntimeState {
  if (!mapping.enabled) return 'closed';
  if (runtimeFailuresByMappingId.value.has(mapping.id)) return 'failed';
  if (runtimeInboundsByMappingId.value.has(mapping.id)) return 'running';
  return 'notRunning';
}

function portStatusLabel(mapping: PortMapping): string {
  return t(`home.portState.${portRuntimeState(mapping)}`);
}

function portStatusTitle(mapping: PortMapping): string {
  const failure = runtimeFailuresByMappingId.value.get(mapping.id);
  return failure?.error || portStatusLabel(mapping);
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
        <Button
          type="button"
          variant="outline"
          class="restore-button"
          :disabled="isLoading || isSaving"
          @click="handleReset"
        >
          <RefreshCw
            class="size-4"
            :class="{ 'spin-icon': isLoading || isSaving }"
            aria-hidden="true"
          />
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

    <section class="notice-bar" :class="{ error: hasNoticeError }" role="status">
      <Info class="size-4" aria-hidden="true" />
      <span class="notice-message">{{ backendNotice }}</span>
      <RouterLink v-if="loginRequired" class="notice-link" :to="loginRoute">
        {{ t('common.goLogin') }}
      </RouterLink>
    </section>

    <nav class="tab-bar" :aria-label="t('home.aria.tabs')">
      <button
        :class="{ active: currentTab === 'mappings' }"
        type="button"
        @click="openTab('mappings')"
      >
        {{ t('home.tabs.mappings') }}
      </button>
      <button :class="{ active: currentTab === 'nodes' }" type="button" @click="openTab('nodes')">
        {{ t('home.tabs.nodes') }}
      </button>
      <button
        :class="{ active: currentTab === 'subscriptions' }"
        type="button"
        @click="openTab('subscriptions')"
      >
        {{ t('home.tabs.subscriptions') }}
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
          <span
            v-for="endpoint in portStripItems"
            :key="endpoint.key"
            :class="`status-${endpoint.state}`"
            :title="endpoint.title"
          >
            <i aria-hidden="true"></i>
            {{ endpoint.label }}
          </span>
        </div>
      </div>

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
              <Power class="size-4" aria-hidden="true" />
            </button>

            <div class="port-title">
              <strong>{{ mapping.listenPort }}</strong>
              <span
                >{{ mapping.listenAddress }} ·
                {{ outboundProtocolLabels[mapping.outboundProtocol] }}</span
              >
              <small
                class="port-status-chip"
                :class="`status-${portRuntimeState(mapping)}`"
                :title="portStatusTitle(mapping)"
              >
                <i aria-hidden="true"></i>
                {{ portStatusLabel(mapping) }}
              </small>
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
              <span>{{ protocolLabels[node.protocol] }}</span>
            </article>

            <article
              v-for="group in mappingGroups(mapping)"
              :key="group.id"
              class="inner-route-card group-route-card"
            >
              <span class="action-popover mini-action" :data-popover="t('common.removeRoute')">
                <button
                  type="button"
                  class="mini-remove"
                  :aria-label="t('home.aria.removeFromPort')"
                  @click="removeGroupFromMapping(mapping, group.id)"
                >
                  <MoreVertical class="size-3" aria-hidden="true" />
                </button>
              </span>
              <strong>{{ group.name }}</strong>
              <span>{{ t(`home.groupStrategy.${group.strategy}`) }}</span>
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
                @click="removeMapping(mapping.id).catch(() => undefined)"
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

    <section v-else-if="currentTab === 'nodes'" class="panel simple-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('home.sections.nodesTitle') }}</h2>
          <p>{{ t('home.sections.nodesLead') }}</p>
        </div>
        <Users class="panel-icon" aria-hidden="true" />
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
          <span>{{ t('home.groupMeta.nodeCount', { count: selectedNodeGroupNodes.length }) }}</span>
        </div>
        <div v-if="selectedNodeGroupNodes.length" class="node-table">
          <article v-for="node in selectedNodeGroupNodes" :key="node.id" class="node-row">
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
              @click="removeNode(node.id).catch(() => undefined)"
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
        <p v-else class="empty-node-state">
          {{ t('home.groupFilters.emptyNodes', { name: selectedNodeGroupTitle }) }}
        </p>
      </section>

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
            <select v-model="manualGroupForm.nodeIds" multiple>
              <option v-for="node in nodes" :key="node.id" :value="node.id">
                {{ node.name }}
              </option>
            </select>
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

    <section v-else-if="currentTab === 'subscriptions'" class="panel simple-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('home.sections.subscriptionsTitle') }}</h2>
          <p>{{ t('home.sections.subscriptionsLead') }}</p>
        </div>
        <RefreshCw class="panel-icon" aria-hidden="true" />
      </div>

      <div class="import-layout">
        <form class="manual-node-form" @submit.prevent="handleSubscriptionSubmit">
          <div class="field-grid two">
            <label>
              <span>{{ t('home.form.subscriptionName') }}</span>
              <input
                v-model.trim="subscriptionForm.name"
                type="text"
                :placeholder="t('common.optional')"
              />
            </label>
            <label>
              <span>{{ t('home.form.subscriptionUrl') }}</span>
              <input v-model.trim="subscriptionForm.url" type="url" required />
            </label>
          </div>
          <label>
            <span>{{ t('home.form.subscriptionGroup') }}</span>
            <select v-model="subscriptionForm.groupId">
              <option value="">{{ t('home.subscription.createGroup') }}</option>
              <option v-for="group in manualGroups" :key="group.id" :value="group.id">
                {{ group.name }}
              </option>
            </select>
          </label>
          <label>
            <span>{{ t('home.form.remark') }}</span>
            <input
              v-model.trim="subscriptionForm.remark"
              type="text"
              :placeholder="t('common.optional')"
            />
          </label>
          <Button type="submit">
            <RefreshCw class="size-4" aria-hidden="true" />
            {{ t('common.addSubscription') }}
          </Button>
        </form>
      </div>

      <div class="node-table">
        <article v-for="subscription in subscriptions" :key="subscription.id" class="node-row">
          <div class="node-protocol">{{ t('home.subscription.source') }}</div>
          <div class="node-main">
            <strong>{{ subscription.name }}</strong>
            <span>{{ subscription.url }}</span>
          </div>
          <div class="card-actions">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :title="t('common.syncSubscription')"
              @click="syncExistingSubscription(subscription.id)"
            >
              <RefreshCw class="size-4" aria-hidden="true" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :title="t('common.deleteSubscription')"
              @click="removeSubscription(subscription.id).catch(() => undefined)"
            >
              <Trash2 class="size-4" aria-hidden="true" />
            </Button>
          </div>
          <div class="node-meta">
            <small>{{ subscriptionGroupName(subscription.groupId) }}</small>
            <small>{{ subscription.lastSyncStatus || t('home.subscription.notSynced') }}</small>
            <small v-if="subscription.lastSyncedAt">{{
              formatDateTime(subscription.lastSyncedAt)
            }}</small>
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
        <label>
          <span>{{ t('home.form.nodeGroup') }}</span>
          <select v-model="rawImportGroupId">
            <option value="">{{ t('home.groupMeta.ungrouped') }}</option>
            <option v-for="group in groups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select>
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
            <span>{{ t('home.form.nodeGroup') }}</span>
            <select v-model="manualNodeForm.groupId">
              <option value="">{{ t('home.groupMeta.ungrouped') }}</option>
              <option v-for="group in groups" :key="group.id" :value="group.id">
                {{ group.name }}
              </option>
            </select>
          </label>

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
            <span>{{ t('home.form.routeSource') }}</span>
            <select v-model="routeNodeForm.mode">
              <option value="uri">{{ t('home.routeSource.uri') }}</option>
              <option value="node">{{ t('home.routeSource.node') }}</option>
              <option value="group">{{ t('home.routeSource.group') }}</option>
            </select>
          </label>

          <label>
            <span>{{ t('home.form.nodeName') }}</span>
            <input
              v-model.trim="routeNodeForm.name"
              type="text"
              autocomplete="off"
              :placeholder="t('home.placeholders.nodeName')"
              :disabled="routeNodeForm.mode !== 'uri'"
              @input="handleRouteNodeNameInput"
            />
          </label>

          <label v-if="routeNodeForm.mode === 'uri'">
            <span>{{ t('home.form.nodeUri') }} <em class="required-mark">*</em></span>
            <input
              v-model.trim="routeNodeForm.uri"
              type="text"
              required
              autocomplete="off"
              :placeholder="t('home.placeholders.nodeUri')"
            />
          </label>

          <label v-else-if="routeNodeForm.mode === 'node'">
            <span>{{ t('home.form.existingNode') }} <em class="required-mark">*</em></span>
            <select v-model="routeNodeForm.existingNodeId" required>
              <option v-for="node in nodes" :key="node.id" :value="node.id">
                {{ node.name }}
              </option>
            </select>
          </label>

          <label v-else>
            <span>{{ t('home.form.existingGroup') }} <em class="required-mark">*</em></span>
            <select v-model="routeNodeForm.groupId" required>
              <option v-for="group in groups" :key="group.id" :value="group.id">
                {{ group.name }}
              </option>
            </select>
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
