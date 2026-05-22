<script setup lang="ts">
import { useClipboard, useVirtualList } from '@vueuse/core';
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import {
  Check,
  ChevronDown,
  Download,
  Gauge,
  Github,
  Languages,
  Link2,
  Plus,
  RefreshCw,
  Route,
  Server,
  Settings,
  Trash2,
  Users,
  X,
} from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useRoute, useRouter } from 'vue-router';
import HomeTabs from './home/HomeTabs.vue';
import GroupsPanel from './home/GroupsPanel.vue';
import MappingsPanel from './home/MappingsPanel.vue';
import NodeGroupFilterSelect from './home/NodeGroupFilterSelect.vue';
import NodesPanel from './home/NodesPanel.vue';
import AppVersionBadge from '@/components/AppVersionBadge.vue';
import type {
  NodeGroupFilterKey,
  NodeGroupFilterOption,
  NodeGroupSummaryItem,
  PortRuntimeState,
  RouteNodeMode,
  TabKey,
  HomeViewContext,
} from './home/types';
import { inferNodeNameFromUri, useProxyHubState } from '@/composables/useProxyHubState';
import { useUiPreferences } from '@/composables/useUiPreferences';
import { useI18n } from '@/i18n';
import type { LocalePreference } from '@/i18n';
import proxyHubMarkUrl from '@/assets/mark-large.png';
import type {
  ImportPreviewResult,
  MappingSwitchTargetType,
  OutboundProtocol,
  PortMapping,
  ProxyGroup,
  ProxyGroupStrategy,
  ProxyNode,
  ProxyNodeHealth,
  ProxyNodeOption,
  ProxyTestResult,
  ProxyProtocol,
  RouteStrategy,
  RuntimeRoute,
  RuntimeRouteNode,
} from '@/types/proxyHub';
import './home.css';

interface ConfirmationDialog {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => Promise<void>;
}

interface DuplicateRouteNodeDialog {
  node: ProxyNode;
}

interface TestDialogState {
  targetType: 'mapping' | 'node';
  targetId: string;
  title: string;
  subtitle: string;
  result: ProxyTestResult | null;
  error: string;
}

type AddNodeDialogMode = 'uri' | 'chain' | 'import';
type ToastVariant = 'default' | 'success';

const { formatDateTime, locale, localePreference, setLocalePreference, t } = useI18n();
const props = defineProps<{
  tab?: TabKey;
}>();
const route = useRoute();
const router = useRouter();
const { showExtraUiInfo } = useUiPreferences();
const clipboard = useClipboard({
  legacy: true,
});

const languagePreferenceOptions = computed<Array<{ label: string; value: LocalePreference }>>(
  () => [
    { label: t('common.languageSystem'), value: 'system' },
    { label: t('common.languageChinese'), value: 'zh-CN' },
    { label: t('common.languageEnglish'), value: 'en-US' },
  ]
);

const currentLocaleLabel = computed(() =>
  locale.value === 'zh-CN' ? t('common.languageChinese') : t('common.languageEnglish')
);

const currentLanguageTitle = computed(() =>
  localePreference.value === 'system'
    ? `${t('common.language')}: ${t('common.languageSystem')} (${currentLocaleLabel.value})`
    : `${t('common.language')}: ${currentLocaleLabel.value}`
);

const protocolLabels = computed<Record<ProxyProtocol, string>>(() => ({
  vless: t('home.protocol.vless'),
  vmess: t('home.protocol.vmess'),
  trojan: t('home.protocol.trojan'),
  socks5: t('home.protocol.socks5'),
  http: t('home.protocol.http'),
  shadowsocks: t('home.protocol.shadowsocks'),
  hysteria: t('home.protocol.hysteria'),
  hysteria2: t('home.protocol.hysteria2'),
  tuic: t('home.protocol.tuic'),
  ssh: t('home.protocol.ssh'),
  chain: t('home.protocol.chain'),
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
  'least-latency': t('home.strategy.leastLatency'),
}));

const strategyOptions = computed<Array<{ label: string; value: RouteStrategy }>>(() => [
  { label: strategyLabels.value.failover, value: 'failover' },
  { label: strategyLabels.value['load-balance'], value: 'load-balance' },
  { label: strategyLabels.value.manual, value: 'manual' },
  { label: strategyLabels.value['least-latency'], value: 'least-latency' },
]);

const {
  nodes,
  nodeTotal,
  currentNodeTotal,
  defaultNodeTotal,
  isLoadingNodes,
  groups,
  subscriptions,
  mappings,
  enabledMappings,
  nodeById,
  nodeOptionById,
  nodeHealthById,
  groupById,
  runtime,
  runtimeRoutes,
  isLoading,
  isSaving,
  errorMessage,
  loginRequired,
  refreshState,
  refreshRuntimeState,
  startRuntimeStatusPolling,
  stopRuntimeStatusPolling,
  addNode,
  addNodeFromUri,
  previewImportNodes,
  importNodes,
  updateNode,
  removeNode,
  testNode,
  addGroup,
  updateGroup,
  removeGroup,
  previewSubscription,
  addSubscription,
  syncSubscription,
  removeSubscription,
  addMapping,
  updateMapping,
  switchMapping,
  removeMapping,
  testMapping,
  loadNodes,
  loadMoreNodes,
  fetchNodeOptions,
  ensureNodeOptions,
} = useProxyHubState();

const currentTab = computed<TabKey>(() => props.tab ?? 'mappings');
const activeNodeGroupFilter = ref<NodeGroupFilterKey>('all');
const hideEmptyNodeGroups = ref(false);
const nodeSearch = ref('');
const chainNodeSearch = ref('');
const chainNodeGroupId = ref('');
const chainNodeOptions = ref<ProxyNodeOption[]>([]);
const chainNodeTotal = ref(0);
const chainNodePage = ref(1);
const isLoadingChainNodes = ref(false);
const editChainNodeSearch = ref('');
const editChainNodeGroupId = ref('');
const editChainNodeOptions = ref<ProxyNodeOption[]>([]);
const editChainNodeTotal = ref(0);
const editChainNodePage = ref(1);
const isLoadingEditChainNodes = ref(false);
const manualGroupNodeSearch = ref('');
const manualGroupNodeGroupId = ref('');
const manualGroupNodeOptions = ref<ProxyNodeOption[]>([]);
const manualGroupNodeTotal = ref(0);
const manualGroupNodePage = ref(1);
const isLoadingManualGroupNodes = ref(false);
const routeNodeSearch = ref('');
const routeNodeGroupId = ref('');
const routeNodeOptions = ref<ProxyNodeOption[]>([]);
const routeNodeTotal = ref(0);
const routeNodePage = ref(1);
const isLoadingRouteNodes = ref(false);
const rawImport = ref('');
const rawImportGroupId = ref('');
const importPreview = ref<ImportPreviewResult | null>(null);
const importPreviewSignature = ref('');
const importMessage = ref('');
const copyMessage = ref('');
const copyMessageVariant = ref<ToastVariant>('default');
const copyMessageTimer = ref<number | null>(null);
const copiedMappingId = ref<string | null>(null);
const copiedNodeId = ref<string | null>(null);
const editingMappingId = ref<string | null>(null);
const editingNodeId = ref<string | null>(null);
const editingGroupId = ref<string | null>(null);
const addNodeDialogMode = ref<AddNodeDialogMode | null>(null);
const isGroupDialogOpen = ref(false);
const routeTargetMappingId = ref<string | null>(null);
const confirmationDialog = ref<ConfirmationDialog | null>(null);
const duplicateRouteNodeDialog = ref<DuplicateRouteNodeDialog | null>(null);
const testDialog = ref<TestDialogState | null>(null);
const testUrl = ref('https://www.gstatic.com/generate_204');
const isTesting = ref(false);
const healthClock = ref(Date.now());
let currentTestController: AbortController | null = null;
let currentTestRunId = 0;

const emptyMappingForm = () => ({
  listenAddress: '0.0.0.0',
  listenPort: 1080,
  outboundProtocol: 'mixed' as OutboundProtocol,
  username: '',
  password: '',
  strategy: 'least-latency' as RouteStrategy,
  remark: '',
});

const mappingForm = reactive(emptyMappingForm());
const routeNodeForm = reactive({
  name: '',
  uri: '',
  existingNodeId: '',
  groupId: '',
  mode: 'uri' as RouteNodeMode,
});
const routeNodeNameEdited = ref(false);
const routeNodeError = ref('');

const routeSourceOptions = computed(() => [
  { value: 'uri' as const, label: t('home.routeSource.uri'), icon: Link2 },
  { value: 'node' as const, label: t('home.routeSource.node'), icon: Server },
  { value: 'group' as const, label: t('home.routeSource.group'), icon: Users },
]);

const nodeCreateForm = reactive({
  name: '',
  rawUri: '',
  groupIds: [] as string[],
  remark: '',
});
const nodeCreateNameEdited = ref(false);
const nodeCreateError = ref('');
const nodeCreateGroupsExpanded = ref(false);

const nodeEditForm = reactive({
  name: '',
  groupIds: [] as string[],
  chainNodeIds: [] as string[],
  rawUri: '',
  remark: '',
});
const nodeEditError = ref('');
const nodeEditGroupsExpanded = ref(false);

const chainNodeForm = reactive({
  name: '',
  chainNodeIds: [] as string[],
  groupIds: [] as string[],
  remark: '',
});
const chainNodeError = ref('');
const chainNodeGroupsExpanded = ref(false);

const subscriptionForm = reactive({
  name: '',
  url: '',
});
const subscriptionPreview = ref<ImportPreviewResult | null>(null);
const subscriptionPreviewSignature = ref('');

const manualGroupForm = reactive({
  name: '',
  strategy: 'selector' as ProxyGroupStrategy,
  nodeIds: [] as string[],
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

function runtimeFailureReason(error: string | null | undefined): string {
  const reason = error?.trim();
  return reason || t('home.messages.runtimeFailureUnknown');
}

function runtimeFailureDetail(failure: { listen?: string | null; error?: string | null }): string {
  return t('home.messages.runtimeFailureDetail', {
    port: failure.listen?.trim() || t('home.messages.runtimeFailureUnknownPort'),
    reason: runtimeFailureReason(failure.error),
  });
}

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
      title: runtimeFailureDetail(failure),
    })),
  ].sort((a, b) => a.label.localeCompare(b.label))
);

const runtimeInboundCount = computed(() => runtime.value?.inbounds?.length ?? 0);
const hasNoticeError = computed(() => Boolean(errorMessage.value) || Boolean(runtime.value?.error));

const availableNodeNotice = computed(() => {
  const total = nodeTotal.value;
  if (total <= 0) return '';

  const healthItems = Object.values(nodeHealthById.value);
  if (healthItems.length === 0) return '';

  const available = healthItems.filter(isAvailableNodeHealth).length;
  return t('home.messages.availableNodes', { available, total });
});

const backendNotice = computed(() => {
  if (errorMessage.value) return errorMessage.value;
  if (isLoading.value) return t('home.messages.loadingBackend');
  if (isSaving.value) return t('home.messages.savingBackend');
  if (runtime.value?.error)
    return t('home.messages.runtimeError', { message: runtime.value.error });
  if (runtime.value?.running) {
    const runningMessage = t('home.messages.runtimeRunning', { count: runtimeInboundCount.value });
    return availableNodeNotice.value
      ? `${runningMessage} ${availableNodeNotice.value}`
      : runningMessage;
  }
  if (runtime.value) return t('home.messages.runtimeStopped');

  return t('home.notice');
});

const loginRoute = computed(() => ({
  name: 'login',
  query: {
    redirect: currentRoutePath(),
  },
}));

const nodesTabLabel = computed(() => t('home.tabs.nodesWithCount', { count: nodeTotal.value }));
const nodesTabCompactLabel = computed(() => {
  const count = nodeTotal.value;
  return t('home.tabs.nodesCompactWithCount', {
    count: count > 999 ? '999+' : count.toString(),
  });
});
const groupsTabLabel = computed(() =>
  t('home.tabs.groupsWithCount', { count: groups.value.length + 1 })
);
const groupsTabCompactLabel = computed(() => {
  const count = groups.value.length + 1;
  return t('home.tabs.groupsCompactWithCount', {
    count: count > 99 ? '99+' : count.toString(),
  });
});
const mappingsTabCompactLabel = computed(() => t('home.tabs.mappingsCompact'));
const mappingCountLabel = computed(
  () => `${enabledMappings.value.length}/${mappings.value.length}`
);

const workspaceTitle = computed(() => {
  if (currentTab.value === 'nodes') return t('home.sections.nodesTitle');
  if (currentTab.value === 'groups') return t('home.sections.groupsTitle');

  return t('home.sections.mappingsTitle');
});

const workspaceLead = computed(() => {
  if (currentTab.value === 'nodes') return t('home.sections.nodesLead');
  if (currentTab.value === 'groups') return t('home.sections.groupsLead');

  return t('home.sections.mappingsLead');
});

function resetMappingForm(): void {
  Object.assign(mappingForm, emptyMappingForm());
}

function optionToProxyNode(option: ProxyNodeOption): ProxyNode {
  return {
    id: option.id,
    name: option.name,
    protocol: option.protocol,
    server: option.server,
    port: option.port,
    username: '',
    password: '',
    rawUri: '',
    tags: [],
    remark: '',
    chainNodeIds: [],
    subscriptionId: '',
    groupId: option.groupIds[0] ?? '',
    groupIds: option.groupIds,
    sourceKey: '',
    health: null,
    createdAt: '',
    updatedAt: '',
  };
}

function nodeFromCache(id: string): ProxyNode | null {
  const node =
    nodeById.value.get(id) ?? optionToProxyNode(nodeOptionById.value.get(id) ?? nullOption(id));
  const health = nodeHealthById.value[id];
  return health ? { ...node, health } : node;
}

function nullOption(id: string): ProxyNodeOption {
  return {
    id,
    name: id,
    protocol: 'unknown',
    server: '',
    port: null,
    groupIds: [],
  };
}

function mappingNodes(mapping: PortMapping): ProxyNode[] {
  return mapping.nodeIds
    .map(id => nodeFromCache(id))
    .filter((node): node is ProxyNode => Boolean(node));
}

function mappingGroups(mapping: PortMapping): ProxyGroup[] {
  return mapping.groupIds
    .map(id => groupById.value.get(id))
    .filter((group): group is ProxyGroup => Boolean(group));
}

function groupNodeIds(group: ProxyGroup, visited = new Set<string>()): string[] {
  if (visited.has(group.id)) return [];
  visited.add(group.id);

  const ids = [...group.nodeIds];
  for (const childGroupId of group.groupIds) {
    const childGroup = groupById.value.get(childGroupId);
    if (childGroup) {
      ids.push(...groupNodeIds(childGroup, visited));
    }
  }
  return Array.from(new Set(ids.filter(Boolean)));
}

function mappingRuntimeGroupTag(mapping: PortMapping): string {
  return `mapping-out-${mapping.id}`;
}

function proxyGroupRuntimeTag(groupId: string): string {
  return `group-${groupId}`;
}

function mappingRuntimeRoute(mapping: PortMapping): RuntimeRoute | null {
  const rootTag = mappingRuntimeGroupTag(mapping);
  return (
    runtimeRoutes.value.find(
      route => route.mappingId === mapping.id && route.groupTag === rootTag
    ) ?? null
  );
}

function proxyGroupRuntimeRoute(mapping: PortMapping, group: ProxyGroup): RuntimeRoute | null {
  const groupTag = proxyGroupRuntimeTag(group.id);
  return (
    runtimeRoutes.value.find(
      route => route.mappingId === mapping.id && route.groupTag === groupTag
    ) ?? null
  );
}

function anyProxyGroupRuntimeRoute(group: ProxyGroup): RuntimeRoute | null {
  const groupTag = proxyGroupRuntimeTag(group.id);
  return runtimeRoutes.value.find(route => route.groupTag === groupTag) ?? null;
}

function runtimeRouteNodeFor(
  mapping: PortMapping,
  targetType: MappingSwitchTargetType,
  targetId: string
): RuntimeRouteNode | null {
  const route = mappingRuntimeRoute(mapping);
  if (!route) return null;
  const tag = targetType === 'group' ? proxyGroupRuntimeTag(targetId) : `node-${targetId}`;
  return (
    route.nodes.find(
      node =>
        node.nodeId === targetId &&
        (targetType === 'group' ? node.kind === 'group' : node.kind === 'node')
    ) ??
    route.nodes.find(node => node.nodeTag === tag) ??
    null
  );
}

function runtimeRouteNodeAvailable(node: RuntimeRouteNode): boolean {
  if (node.error && !node.probeRunning) return false;
  return (
    node.available ||
    node.latencyCandidate ||
    node.latencyFallback ||
    node.latencyMs > 0 ||
    node.probeRunning
  );
}

function hasKnownNodeHealth(health: ProxyNodeHealth | null | undefined): boolean {
  return Boolean(
    health &&
      (health.lastCheckedAt ||
        health.lastSuccessAt ||
        health.lastFailureAt ||
        health.lastError ||
        health.blacklisted ||
        health.probeRunning ||
        health.lastLatencyMs > 0)
  );
}

function isAvailableNodeHealth(health: ProxyNodeHealth | null | undefined): boolean {
  if (!health || health.blacklisted || (health.lastError && !health.probeRunning)) return false;
  return health.available || health.lastLatencyMs > 0 || health.probeRunning;
}

function isUnavailableNodeHealth(health: ProxyNodeHealth | null | undefined): boolean {
  if (!health || health.probeRunning) return false;
  return health.blacklisted || Boolean(health.lastError?.trim());
}

function nodeGroupCardHealthSummary(group: ProxyGroup): { allUnavailable: boolean } {
  const nodeIds = groupNodeIds(group);
  const total = Math.max(group.nodeCount, nodeIds.length);
  if (total === 0) return { allUnavailable: false };

  const runtimeRoute = anyProxyGroupRuntimeRoute(group);
  const runtimeItems = runtimeRoute?.nodes ?? [];
  if (runtimeItems.length > 0) {
    const available = runtimeItems.filter(runtimeRouteNodeAvailable).length;
    const probing = runtimeItems.filter(
      node => node.probeRunning || runtimeRoute?.probeRunning
    ).length;
    return {
      allUnavailable:
        runtimeItems.length >= Math.min(total, nodeIds.length || total) &&
        available === 0 &&
        probing === 0,
    };
  }

  if (nodeIds.length < total) return { allUnavailable: false };

  let available = 0;
  let unavailable = 0;
  let known = 0;

  for (const nodeId of nodeIds) {
    const health = nodeHealthById.value[nodeId];
    if (!health) continue;
    known += 1;
    if (isAvailableNodeHealth(health)) {
      available += 1;
    } else if (isUnavailableNodeHealth(health)) {
      unavailable += 1;
    }
  }

  return {
    allUnavailable: known >= total && available === 0 && unavailable >= total,
  };
}

function groupRouteHealthSummary(mapping: PortMapping, group: ProxyGroup) {
  const runtimeRoute = proxyGroupRuntimeRoute(mapping, group);
  const nodeIds = groupNodeIds(group);
  const runtimeItems = runtimeRoute?.nodes ?? [];
  const total = Math.max(group.nodeCount, nodeIds.length, runtimeItems.length);
  let available = 0;
  let known = runtimeItems.length;
  let probing = 0;
  let fastestLatencyMs = 0;

  if (runtimeRoute && runtimeItems.length > 0) {
    for (const node of runtimeItems) {
      if (runtimeRouteNodeAvailable(node)) available += 1;
      if (node.probeRunning || runtimeRoute.probeRunning) probing += 1;
      if (node.latencyMs > 0 && (fastestLatencyMs === 0 || node.latencyMs < fastestLatencyMs)) {
        fastestLatencyMs = node.latencyMs;
      }
    }
    return { available, fastestLatencyMs, known, probing, total };
  }

  for (const nodeId of nodeIds) {
    const health = nodeHealthById.value[nodeId];
    if (hasKnownNodeHealth(health)) known += 1;
    if (isAvailableNodeHealth(health)) available += 1;
    if (health?.probeRunning) probing += 1;
    if (
      health?.lastLatencyMs &&
      health.lastLatencyMs > 0 &&
      (fastestLatencyMs === 0 || health.lastLatencyMs < fastestLatencyMs)
    ) {
      fastestLatencyMs = health.lastLatencyMs;
    }
  }
  return { available, fastestLatencyMs, known, probing, total };
}

function groupRouteAvailabilityLabel(mapping: PortMapping, group: ProxyGroup): string {
  const summary = groupRouteHealthSummary(mapping, group);
  return t('home.nodeGroupHealth.availableRatio', {
    available: summary.available,
    total: summary.total,
  });
}

function groupRouteAvailableUnavailable(mapping: PortMapping, group: ProxyGroup): boolean {
  return groupRouteHealthSummary(mapping, group).available === 0;
}

function groupRouteLatencyLabel(mapping: PortMapping, group: ProxyGroup): string {
  const latency = groupRouteHealthSummary(mapping, group).fastestLatencyMs;
  return latency > 0 ? `${latency}ms` : '-ms';
}

function groupRouteHealthState(
  mapping: PortMapping,
  group: ProxyGroup
): 'success' | 'failure' | 'probing' | 'unknown' {
  const summary = groupRouteHealthSummary(mapping, group);
  if (summary.probing > 0) return 'probing';
  if (summary.available > 0) return 'success';
  if (summary.known > 0 && summary.total > 0) return 'failure';
  return 'unknown';
}

function groupRouteHealthTitle(mapping: PortMapping, group: ProxyGroup): string {
  const summary = groupRouteHealthSummary(mapping, group);
  const details = [
    t('home.nodeGroupHealth.total', { count: summary.total }),
    t('home.nodeGroupHealth.available', { count: summary.available }),
    t('home.nodeGroupHealth.fastest', {
      latency: summary.fastestLatencyMs > 0 ? `${summary.fastestLatencyMs}ms` : '-',
    }),
  ];
  if (summary.probing > 0) {
    details.push(t('home.nodeGroupHealth.probing', { count: summary.probing }));
  }
  return details.join('\n');
}

function mappingHasActiveRoute(mapping: PortMapping): boolean {
  return Boolean(mapping.activeNodeId || mapping.activeGroupId);
}

function shouldUseManualActiveRoute(mapping: PortMapping): boolean {
  return mapping.strategy === 'manual';
}

function isActiveRoute(
  mapping: PortMapping,
  targetType: MappingSwitchTargetType,
  targetId: string
): boolean {
  const runtimeNode = runtimeRouteNodeFor(mapping, targetType, targetId);
  if (runtimeNode && mapping.strategy !== 'load-balance') {
    return runtimeNode.selected;
  }
  if (!shouldUseManualActiveRoute(mapping)) return false;
  if (targetType === 'group') return mapping.activeGroupId === targetId;
  return mapping.activeNodeId === targetId && !mapping.activeGroupId;
}

function mappingEndpoint(mapping: PortMapping): string {
  return `${mapping.listenAddress}:${mapping.listenPort}`;
}

function currentPageEndpointHost(): string {
  if (typeof window === 'undefined') {
    return '127.0.0.1';
  }

  const hostname = window.location.hostname || '127.0.0.1';
  const unbracketedHostname =
    hostname.startsWith('[') && hostname.endsWith(']') ? hostname.slice(1, -1) : hostname;
  return unbracketedHostname.includes(':') ? `[${unbracketedHostname}]` : unbracketedHostname;
}

function copyableEndpointProtocol(mapping: PortMapping): 'http' | 'socks5' {
  return mapping.outboundProtocol === 'socks5' ? 'socks5' : 'http';
}

function copyableMappingEndpoint(mapping: PortMapping): string {
  return `${copyableEndpointProtocol(mapping)}://${currentPageEndpointHost()}:${mapping.listenPort}`;
}

const visibleGroups = computed(() =>
  hideEmptyNodeGroups.value ? groups.value.filter(group => group.nodeCount > 0) : groups.value
);

const nodeGroupFilterOptions = computed<NodeGroupFilterOption[]>(() => [
  {
    key: 'all',
    label: t('home.groupFilters.all'),
    countLabel: t('home.groupMeta.nodeCount', { count: nodeTotal.value }),
  },
  {
    key: 'default',
    label: t('home.groupFilters.default'),
    countLabel: t('home.groupMeta.nodeCount', { count: defaultNodeTotal.value }),
  },
  ...visibleGroups.value.map(group => ({
    key: toGroupFilterKey(group.id),
    label:
      group.type === 'subscription'
        ? t('home.groupFilters.subscriptionLabel', { name: group.name })
        : group.name,
    countLabel: t('home.groupMeta.nodeCount', { count: group.nodeCount }),
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
  return nodes.value.map(node => {
    const health = nodeHealthById.value[node.id];
    return health ? { ...node, health } : node;
  });
});

const selectedGroupRuntimeRoute = computed(() => {
  const group = selectedGroup.value;
  return group ? anyProxyGroupRuntimeRoute(group) : null;
});

const selectedNodeGroupHealthSummary = computed(() => {
  let available = 0;
  let probing = 0;
  let needsProbe = 0;
  let unavailable = 0;
  let fastestLatencyMs = 0;
  let autoProbeEnabled = false;
  let autoProbeRunning = false;

  if (selectedGroupRuntimeRoute.value) {
    autoProbeEnabled = true;
    autoProbeRunning = selectedGroupRuntimeRoute.value.probeRunning;
  }

  for (const node of selectedNodeGroupNodes.value) {
    const health = node.health;
    if (!health) {
      needsProbe += 1;
      continue;
    }
    if (health.probeRunning) {
      probing += 1;
    }
    if (!health.lastCheckedAt && !health.probeRunning) {
      needsProbe += 1;
    }
    if (health.blacklisted || (health.lastError && !health.probeRunning)) {
      unavailable += 1;
    } else if (health.available || health.lastLatencyMs > 0) {
      available += 1;
    }
    if (
      health.lastLatencyMs > 0 &&
      (fastestLatencyMs === 0 || health.lastLatencyMs < fastestLatencyMs)
    ) {
      fastestLatencyMs = health.lastLatencyMs;
    }
  }

  return {
    available,
    autoProbeEnabled,
    autoProbeRunning,
    fastestLatencyMs,
    needsProbe,
    probing,
    unavailable,
  };
});

const nodeListQuery = computed(() => ({
  keyword: nodeSearch.value,
  groupId: groupIdFromFilterKey(activeNodeGroupFilter.value),
  defaultOnly: activeNodeGroupFilter.value === 'default',
  page: 1,
  size: 80,
  withHealth: true,
}));

const {
  list: virtualNodeRows,
  containerProps: nodeListContainerProps,
  wrapperProps: nodeListWrapperProps,
  scrollTo: scrollNodeListTo,
} = useVirtualList(selectedNodeGroupNodes, {
  itemHeight: 116,
  overscan: 8,
});

let nodeSearchTimer: number | null = null;
let healthClockTimer: number | null = null;

onMounted(() => {
  healthClockTimer = window.setInterval(() => {
    healthClock.value = Date.now();
  }, 60_000);
  refreshRuntimeState().catch(() => undefined);
  startRuntimeStatusPolling();
});

onBeforeUnmount(() => {
  cancelCurrentTest();
  if (healthClockTimer !== null) {
    window.clearInterval(healthClockTimer);
    healthClockTimer = null;
  }
  stopRuntimeStatusPolling();
});

async function reloadCurrentNodes(): Promise<void> {
  await loadNodes(nodeListQuery.value);
  scrollNodeListTo(0);
}

async function loadNextNodePage(): Promise<void> {
  await loadMoreNodes(nodeListQuery.value);
}

const editingNode = computed(() =>
  editingNodeId.value ? (nodes.value.find(node => node.id === editingNodeId.value) ?? null) : null
);

const groupSummaryItems = computed<NodeGroupSummaryItem[]>(() => [
  {
    key: 'default',
    title: t('home.groupFilters.default'),
    typeLabel: t('home.groupFilters.virtual'),
    count: defaultNodeTotal.value,
    detail: t('home.groupFilters.defaultDetail'),
    strategyLabel: t('home.groupFilters.defaultStrategy'),
    filter: '',
    isSubscription: false,
    editable: false,
    allUnavailable: false,
  },
  ...groups.value.map(group => {
    const healthSummary = nodeGroupCardHealthSummary(group);
    return {
      key: toGroupFilterKey(group.id),
      groupId: group.id,
      subscriptionId: group.subscriptionId,
      title: group.name,
      typeLabel: t(`home.groupType.${group.type}`),
      count: group.nodeCount,
      detail: groupSummary(group),
      strategyLabel: t(`home.groupStrategy.${group.strategy}`),
      filter: group.filter,
      isSubscription: group.type === 'subscription',
      editable: group.type === 'manual',
      allUnavailable: healthSummary.allUnavailable,
    };
  }),
]);

watch(
  () => nodeGroupFilterOptions.value.map(option => option.key).join('|'),
  () => {
    if (!nodeGroupFilterOptions.value.some(option => option.key === activeNodeGroupFilter.value)) {
      activeNodeGroupFilter.value = 'all';
    }
  }
);

watch(
  () => [currentTab.value, route.query.group],
  () => {
    if (currentTab.value !== 'nodes') return;
    const nextFilter = filterKeyFromQuery(route.query.group);
    if (activeNodeGroupFilter.value !== nextFilter) {
      activeNodeGroupFilter.value = nextFilter;
    }
  },
  { immediate: true }
);

watch(currentTab, tab => {
  if (tab !== 'nodes' && activeNodeGroupFilter.value !== 'all') {
    activeNodeGroupFilter.value = 'all';
  }
});

watch(
  [activeNodeGroupFilter, nodeSearch],
  () => {
    if (nodeSearchTimer !== null) {
      window.clearTimeout(nodeSearchTimer);
    }
    nodeSearchTimer = window.setTimeout(() => {
      reloadCurrentNodes().catch(() => undefined);
      nodeSearchTimer = null;
    }, 220);
  },
  { immediate: true }
);

watch(
  () => mappings.value.flatMap(mapping => mapping.nodeIds).join('|'),
  ids => {
    ensureNodeOptions(ids.split('|').filter(Boolean)).catch(() => undefined);
  },
  { immediate: true }
);

function groupNodes(group: ProxyGroup): ProxyNode[] {
  return selectedNodeGroupNodes.value.filter(
    node =>
      node.groupId === group.id ||
      node.groupIds.includes(group.id) ||
      group.nodeIds.includes(node.id)
  );
}

function nodeGroupIds(node: ProxyNode): string[] {
  return Array.from(
    new Set([
      ...node.groupIds,
      ...(node.groupId ? [node.groupId] : []),
      ...groups.value.filter(group => group.nodeIds.includes(node.id)).map(group => group.id),
    ])
  );
}

function selectNodeGroupFilter(key: NodeGroupFilterKey): void {
  activeNodeGroupFilter.value = key;
}

function filterKeyFromQuery(value: unknown): NodeGroupFilterKey {
  const group = Array.isArray(value) ? value[0] : value;
  if (group === 'default') return 'default';
  if (typeof group === 'string' && group.trim() !== '') return toGroupFilterKey(group.trim());
  return 'all';
}

function queryValueFromFilterKey(key: NodeGroupFilterKey): string | undefined {
  if (key === 'default') return 'default';
  const groupId = groupIdFromFilterKey(key);
  return groupId || undefined;
}

function currentRoutePath(): string {
  const query = route.fullPath.split('?')[1];
  return query ? `${route.path}?${query}` : route.path;
}

function tabPath(tab: TabKey): string {
  if (tab === 'nodes') return '/nodes';
  if (tab === 'groups') return '/groups';
  return '/';
}

function nodeFilterGroupId(value: string): string | undefined {
  return value || undefined;
}

async function refreshOptionList(
  target: {
    options: typeof chainNodeOptions;
    total: typeof chainNodeTotal;
    page: typeof chainNodePage;
    loading: typeof isLoadingChainNodes;
  },
  input: {
    keyword: string;
    groupId: string;
    physicalOnly?: boolean;
    selectedIds?: string[];
    page?: number;
  },
  append = false
): Promise<void> {
  target.loading.value = true;
  try {
    const groupQuery = optionGroupQuery(input.groupId);
    const result = await fetchNodeOptions({
      keyword: input.keyword,
      nameOnly: true,
      groupId: nodeFilterGroupId(groupQuery.groupId),
      defaultOnly: groupQuery.defaultOnly,
      physicalOnly: input.physicalOnly,
      page: input.page ?? 1,
      size: 80,
    });
    const selectedOptions = input.selectedIds?.length
      ? await fetchSelectedNodeOptions(input.selectedIds)
      : [];
    const merged = mergeNodeOptions(
      append ? target.options.value : [],
      selectedOptions,
      result.items
    );
    target.options.value = merged;
    target.total.value = result.total;
    target.page.value = result.page;
  } finally {
    target.loading.value = false;
  }
}

async function fetchSelectedNodeOptions(ids: string[]): Promise<ProxyNodeOption[]> {
  const uniqueIds = Array.from(new Set(ids.filter(Boolean)));
  const items: ProxyNodeOption[] = [];
  for (let index = 0; index < uniqueIds.length; index += 200) {
    const batch = uniqueIds.slice(index, index + 200);
    const result = await fetchNodeOptions({ ids: batch, size: batch.length });
    items.push(...result.items);
  }
  return items;
}

function mergeNodeOptions(...groupsToMerge: ProxyNodeOption[][]): ProxyNodeOption[] {
  const byId = new Map<string, ProxyNodeOption>();
  for (const items of groupsToMerge) {
    for (const item of items) byId.set(item.id, item);
  }
  return Array.from(byId.values());
}

function groupFilterLabel(name: string, count: number): string {
  return t('home.groupMeta.optionNodeCount', {
    name,
    countLabel: t('home.groupMeta.nodeCount', { count }),
  });
}

function groupFilterOptions(includeAll = true): Array<{ id: string; label: string }> {
  return [
    ...(includeAll
      ? [{ id: '', label: groupFilterLabel(t('home.groupFilters.all'), nodeTotal.value) }]
      : []),
    {
      id: '__default__',
      label: groupFilterLabel(t('home.groupFilters.default'), defaultNodeTotal.value),
    },
    ...groups.value.map(group => ({
      id: group.id,
      label: groupFilterLabel(group.name, group.nodeCount),
    })),
  ];
}

function optionGroupQuery(groupId: string): { groupId: string; defaultOnly: boolean } {
  if (groupId === '__default__') return { groupId: '', defaultOnly: true };
  return { groupId, defaultOnly: false };
}

const chainOptionTarget = {
  options: chainNodeOptions,
  total: chainNodeTotal,
  page: chainNodePage,
  loading: isLoadingChainNodes,
};
const editChainOptionTarget = {
  options: editChainNodeOptions,
  total: editChainNodeTotal,
  page: editChainNodePage,
  loading: isLoadingEditChainNodes,
};
const manualGroupOptionTarget = {
  options: manualGroupNodeOptions,
  total: manualGroupNodeTotal,
  page: manualGroupNodePage,
  loading: isLoadingManualGroupNodes,
};
const routeNodeOptionTarget = {
  options: routeNodeOptions,
  total: routeNodeTotal,
  page: routeNodePage,
  loading: isLoadingRouteNodes,
};

watch([chainNodeSearch, chainNodeGroupId], reloadChainOptions, { immediate: true });
watch([editChainNodeSearch, editChainNodeGroupId], reloadEditChainOptions, { immediate: true });
watch([manualGroupNodeSearch, manualGroupNodeGroupId], reloadManualGroupNodeOptions, {
  immediate: true,
});
watch([routeNodeSearch, routeNodeGroupId], reloadRouteNodeOptions, { immediate: true });

function reloadChainOptions(): void {
  refreshOptionList(chainOptionTarget, {
    keyword: chainNodeSearch.value,
    groupId: chainNodeGroupId.value,
    physicalOnly: true,
    selectedIds: chainNodeForm.chainNodeIds,
  }).catch(() => undefined);
}

function reloadEditChainOptions(): void {
  refreshOptionList(editChainOptionTarget, {
    keyword: editChainNodeSearch.value,
    groupId: editChainNodeGroupId.value,
    physicalOnly: true,
    selectedIds: nodeEditForm.chainNodeIds,
  }).catch(() => undefined);
}

function reloadManualGroupNodeOptions(): void {
  refreshOptionList(manualGroupOptionTarget, {
    keyword: manualGroupNodeSearch.value,
    groupId: manualGroupNodeGroupId.value,
    selectedIds: manualGroupForm.nodeIds,
  }).catch(() => undefined);
}

function reloadRouteNodeOptions(): void {
  refreshOptionList(routeNodeOptionTarget, {
    keyword: routeNodeSearch.value,
    groupId: routeNodeGroupId.value,
    selectedIds: routeNodeForm.existingNodeId ? [routeNodeForm.existingNodeId] : [],
  }).catch(() => undefined);
}

function loadMoreChainOptions(): void {
  refreshOptionList(
    chainOptionTarget,
    {
      keyword: chainNodeSearch.value,
      groupId: chainNodeGroupId.value,
      physicalOnly: true,
      selectedIds: chainNodeForm.chainNodeIds,
      page: chainNodePage.value + 1,
    },
    true
  ).catch(() => undefined);
}

function loadMoreEditChainOptions(): void {
  refreshOptionList(
    editChainOptionTarget,
    {
      keyword: editChainNodeSearch.value,
      groupId: editChainNodeGroupId.value,
      physicalOnly: true,
      selectedIds: nodeEditForm.chainNodeIds,
      page: editChainNodePage.value + 1,
    },
    true
  ).catch(() => undefined);
}

function loadMoreManualGroupNodeOptions(): void {
  refreshOptionList(
    manualGroupOptionTarget,
    {
      keyword: manualGroupNodeSearch.value,
      groupId: manualGroupNodeGroupId.value,
      selectedIds: manualGroupForm.nodeIds,
      page: manualGroupNodePage.value + 1,
    },
    true
  ).catch(() => undefined);
}

function loadMoreRouteNodeOptions(): void {
  refreshOptionList(
    routeNodeOptionTarget,
    {
      keyword: routeNodeSearch.value,
      groupId: routeNodeGroupId.value,
      selectedIds: routeNodeForm.existingNodeId ? [routeNodeForm.existingNodeId] : [],
      page: routeNodePage.value + 1,
    },
    true
  ).catch(() => undefined);
}

function groupSummary(group: ProxyGroup): string {
  const nodeCount = group.nodeCount;
  if (group.type === 'manual') {
    return t('home.groupMeta.nodeCount', { count: nodeCount });
  }
  const groupCount = group.groupIds.length;
  const builtins = group.builtinTags.length;
  return t('home.groupMeta.summary', { nodeCount, groupCount, builtins });
}

function subscriptionGroupName(groupId: string): string {
  return groupById.value.get(groupId)?.name || t('home.subscription.noGroup');
}

function chainNodeNames(node: ProxyNode): string {
  return node.chainNodeIds
    .map(id => nodeById.value.get(id)?.name ?? nodeOptionById.value.get(id)?.name)
    .filter((name): name is string => Boolean(name))
    .join(' -> ');
}

function chainNodeFormPreview(): string {
  return chainNodeNamesFromIds(chainNodeForm.chainNodeIds);
}

function chainNodeNamesFromIds(nodeIds: string[]): string {
  return nodeIds
    .map(id => nodeById.value.get(id)?.name ?? nodeOptionById.value.get(id)?.name)
    .filter((name): name is string => Boolean(name))
    .join(' -> ');
}

function selectedChainNodes(): ProxyNode[] {
  return selectedChainNodesFromIds(chainNodeForm.chainNodeIds);
}

function selectedChainNodesFromIds(nodeIds: string[]): ProxyNode[] {
  return nodeIds.map(id => nodeFromCache(id)).filter((node): node is ProxyNode => Boolean(node));
}

function removeChainNodeSelection(nodeId: string): void {
  chainNodeForm.chainNodeIds = chainNodeForm.chainNodeIds.filter(id => id !== nodeId);
}

function toggleChainNodeSelection(nodeId: string): void {
  if (chainNodeForm.chainNodeIds.includes(nodeId)) {
    removeChainNodeSelection(nodeId);
    return;
  }
  chainNodeForm.chainNodeIds = [...chainNodeForm.chainNodeIds, nodeId];
  ensureNodeOptions([nodeId]).catch(() => undefined);
}

function nodeEditChainPreview(): string {
  return chainNodeNamesFromIds(nodeEditForm.chainNodeIds);
}

function selectedNodeEditChainNodes(): ProxyNode[] {
  return selectedChainNodesFromIds(nodeEditForm.chainNodeIds);
}

function removeNodeEditChainNodeSelection(nodeId: string): void {
  nodeEditForm.chainNodeIds = nodeEditForm.chainNodeIds.filter(id => id !== nodeId);
}

function toggleNodeEditChainNodeSelection(nodeId: string): void {
  if (nodeEditForm.chainNodeIds.includes(nodeId)) {
    removeNodeEditChainNodeSelection(nodeId);
    return;
  }
  nodeEditForm.chainNodeIds = [...nodeEditForm.chainNodeIds, nodeId];
  ensureNodeOptions([nodeId]).catch(() => undefined);
}

function toggleNodeEditGroup(groupId: string): void {
  if (nodeEditForm.groupIds.includes(groupId)) {
    nodeEditForm.groupIds = nodeEditForm.groupIds.filter(id => id !== groupId);
    return;
  }
  nodeEditForm.groupIds = [...nodeEditForm.groupIds, groupId];
}

function selectNodeEditDefaultGroup(): void {
  nodeEditForm.groupIds = [];
}

function toggleNodeCreateGroup(groupId: string): void {
  if (nodeCreateForm.groupIds.includes(groupId)) {
    nodeCreateForm.groupIds = nodeCreateForm.groupIds.filter(id => id !== groupId);
    return;
  }
  nodeCreateForm.groupIds = [...nodeCreateForm.groupIds, groupId];
}

function selectNodeCreateDefaultGroup(): void {
  nodeCreateForm.groupIds = [];
}

function toggleChainNodeGroup(groupId: string): void {
  if (chainNodeForm.groupIds.includes(groupId)) {
    chainNodeForm.groupIds = chainNodeForm.groupIds.filter(id => id !== groupId);
    return;
  }
  chainNodeForm.groupIds = [...chainNodeForm.groupIds, groupId];
}

function selectChainNodeDefaultGroup(): void {
  chainNodeForm.groupIds = [];
}

function toggleManualGroupNode(nodeId: string): void {
  if (manualGroupForm.nodeIds.includes(nodeId)) {
    manualGroupForm.nodeIds = manualGroupForm.nodeIds.filter(id => id !== nodeId);
    return;
  }
  manualGroupForm.nodeIds = [...manualGroupForm.nodeIds, nodeId];
  ensureNodeOptions([nodeId]).catch(() => undefined);
}

function selectedManualGroupNodes(): ProxyNode[] {
  return manualGroupForm.nodeIds
    .map(id => {
      const cachedNode = nodeById.value.get(id);
      const cachedOption = nodeOptionById.value.get(id);
      const fallback = cachedNode ?? (cachedOption ? optionToProxyNode(cachedOption) : null);
      const health = nodeHealthById.value[id];
      if (!fallback) return null;
      return health ? { ...fallback, health } : fallback;
    })
    .filter((node): node is ProxyNode => Boolean(node));
}

function groupSelectionSummary(groupIds: string[]): string {
  const ids = Array.from(new Set(groupIds.filter(Boolean)));
  if (ids.length === 0) return t('home.groupMeta.ungrouped');

  const names = ids
    .map(id => groupById.value.get(id)?.name)
    .filter((name): name is string => Boolean(name));
  if (names.length === 0) return t('home.groupMeta.ungrouped');
  if (names.length <= 2) return names.join('、');

  return t('home.groupMeta.selectedGroups', {
    first: names[0],
    second: names[1],
    count: names.length - 2,
  });
}

function nodeEndpointLabel(node: ProxyNode): string {
  if (node.protocol === 'chain') {
    return chainNodeNames(node) || t('home.nodeMeta.chainEmpty');
  }
  return `${node.server}:${node.port ?? '-'}`;
}

function optionEndpointLabel(option: ProxyNodeOption): string {
  if (option.protocol === 'chain') return t('home.protocol.chain');
  const endpoint = option.server ? `${option.server}:${option.port ?? '-'}` : '';
  return endpoint || t('common.noRemark');
}

function optionProtocolLabel(option: ProxyNodeOption): string {
  return protocolLabels.value[option.protocol] ?? option.protocol.toUpperCase();
}

function optionNameLabel(option: ProxyNodeOption): string {
  return option.name.trim() || optionEndpointLabel(option);
}

function uriHost(server: string): string {
  const value = server.trim();
  return value.includes(':') && !value.startsWith('[') ? `[${value}]` : value;
}

function uriFragment(name: string): string {
  const value = name.trim();
  return value ? `#${encodeURIComponent(value)}` : '';
}

function uriUserInfo(username: string, password = ''): string {
  const user = username.trim();
  const pass = password.trim();
  if (!user && !pass) return '';

  return `${encodeURIComponent(user)}${pass ? `:${encodeURIComponent(pass)}` : ''}@`;
}

function base64EncodeUtf8(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function nodeExportUri(node: ProxyNode): string {
  const rawUri = node.rawUri.trim();
  if (rawUri) return rawUri;
  if (node.protocol === 'chain' || node.protocol === 'unknown') return '';
  if (!node.server.trim() || !node.port) return '';

  const host = uriHost(node.server);
  const fragment = uriFragment(node.name);

  if (node.protocol === 'vmess') {
    if (!node.username.trim()) return '';
    return `vmess://${base64EncodeUtf8(
      JSON.stringify({
        v: '2',
        ps: node.name,
        add: node.server,
        port: String(node.port),
        id: node.username,
        aid: '0',
        scy: 'auto',
        net: 'tcp',
        type: 'none',
        host: '',
        path: '',
        tls: '',
      })
    )}`;
  }

  if (node.protocol === 'trojan') {
    if (!node.password.trim()) return '';
    return `trojan://${encodeURIComponent(node.password.trim())}@${host}:${node.port}${fragment}`;
  }

  if (node.protocol === 'hysteria' || node.protocol === 'hysteria2') {
    if (!node.password.trim()) return '';
    const scheme = node.protocol === 'hysteria2' ? 'hy2' : 'hysteria';
    return `${scheme}://${encodeURIComponent(node.password.trim())}@${host}:${node.port}${fragment}`;
  }

  if (node.protocol === 'vless') {
    if (!node.username.trim()) return '';
    return `vless://${encodeURIComponent(node.username.trim())}@${host}:${node.port}${fragment}`;
  }

  return `${node.protocol}://${uriUserInfo(node.username, node.password)}${host}:${node.port}${fragment}`;
}

function nodeUriPopoverText(node: ProxyNode): string {
  if (copiedNodeId.value === node.id) return t('common.copiedNodeUri');
  return nodeExportUri(node) ? t('common.exportNodeUri') : t('home.messages.nodeUriUnavailable');
}

function resetNodeCreateForm(): void {
  Object.assign(nodeCreateForm, {
    name: '',
    rawUri: '',
    groupIds: [],
    remark: '',
  });
  nodeCreateNameEdited.value = false;
  nodeCreateError.value = '';
  nodeCreateGroupsExpanded.value = false;
}

function resetChainNodeForm(): void {
  Object.assign(chainNodeForm, {
    name: '',
    chainNodeIds: [],
    groupIds: [],
    remark: '',
  });
  chainNodeSearch.value = '';
  chainNodeGroupId.value = '';
  chainNodeError.value = '';
  chainNodeGroupsExpanded.value = false;
}

function openAddNodeDialog(mode: AddNodeDialogMode): void {
  addNodeDialogMode.value = mode;
  if (mode === 'uri') {
    resetNodeCreateForm();
    return;
  }
  if (mode === 'import') {
    resetImportDialog();
    importMessage.value = '';
    return;
  }
  resetChainNodeForm();
  reloadChainOptions();
}

function resetManualGroupForm(): void {
  manualGroupForm.name = '';
  manualGroupForm.strategy = 'selector';
  manualGroupForm.nodeIds = [];
  manualGroupForm.remark = '';
  manualGroupNodeSearch.value = '';
  manualGroupNodeGroupId.value = '';
}

function openNewGroupDialog(): void {
  closeNodeEditDialog();
  editingGroupId.value = null;
  resetManualGroupForm();
  isGroupDialogOpen.value = true;
  reloadManualGroupNodeOptions();
}

function openEditGroupDialog(group: ProxyGroup): void {
  closeNodeEditDialog();
  if (group.type !== 'manual') return;

  editingGroupId.value = group.id;
  Object.assign(manualGroupForm, {
    name: group.name,
    strategy: group.strategy,
    nodeIds: [...group.nodeIds],
    remark: group.remark,
  });
  manualGroupNodeSearch.value = '';
  manualGroupNodeGroupId.value = '';
  isGroupDialogOpen.value = true;
  ensureNodeOptions(group.nodeIds).catch(() => undefined);
  reloadManualGroupNodeOptions();
}

function openEditGroupById(groupId: string): void {
  const group = groupById.value.get(groupId);
  if (group) openEditGroupDialog(group);
}

function closeGroupDialog(): void {
  isGroupDialogOpen.value = false;
  editingGroupId.value = null;
  resetManualGroupForm();
}

function closeAddNodeDialog(): void {
  addNodeDialogMode.value = null;
  resetNodeCreateForm();
  resetChainNodeForm();
  resetImportDialog();
}

function handleNodeCreateNameInput(): void {
  nodeCreateNameEdited.value = true;
}

async function saveNodeCreateDialog(): Promise<void> {
  if (!nodeCreateForm.rawUri.trim()) {
    nodeCreateError.value = t('home.messages.routeNodeRequired');
    return;
  }

  try {
    const node = await addNodeFromUri(
      nodeCreateForm.rawUri,
      nodeCreateForm.name,
      nodeCreateForm.groupIds[0] ?? '',
      [...nodeCreateForm.groupIds],
      nodeCreateForm.remark
    );
    importMessage.value = t('home.messages.nodeAdded', { name: node.name });
    closeAddNodeDialog();
  } catch (error) {
    nodeCreateError.value =
      error instanceof Error ? error.message : t('home.messages.requestFailed');
  }
}

function resetNodeEditForm(): void {
  Object.assign(nodeEditForm, {
    name: '',
    groupIds: [],
    chainNodeIds: [],
    rawUri: '',
    remark: '',
  });
  nodeEditError.value = '';
  nodeEditGroupsExpanded.value = false;
}

function openEditNodeDialog(node: ProxyNode): void {
  ensureNodeOptions(node.chainNodeIds).catch(() => undefined);
  Object.assign(nodeEditForm, {
    name: node.name,
    groupIds: nodeGroupIds(node),
    chainNodeIds: [...node.chainNodeIds],
    rawUri: node.protocol === 'chain' ? '' : nodeExportUri(node),
    remark: node.remark,
  });
  nodeEditError.value = '';
  editingNodeId.value = node.id;
}

function closeNodeEditDialog(): void {
  editingNodeId.value = null;
  resetNodeEditForm();
}

async function saveNodeEditDialog(): Promise<void> {
  if (!editingNodeId.value) return;
  const currentNode = editingNode.value;
  if (!currentNode) return;

  if (currentNode.protocol === 'chain' && nodeEditForm.chainNodeIds.length < 2) {
    nodeEditError.value = t('home.messages.chainNodesRequired');
    return;
  }
  if (currentNode.protocol !== 'chain' && !nodeEditForm.rawUri.trim()) {
    nodeEditError.value = t('home.messages.routeNodeRequired');
    return;
  }

  try {
    const node = await updateNode(editingNodeId.value, {
      name: nodeEditForm.name,
      groupId: nodeEditForm.groupIds[0] ?? '',
      groupIds: [...nodeEditForm.groupIds],
      chainNodeIds:
        currentNode.protocol === 'chain'
          ? [...nodeEditForm.chainNodeIds]
          : currentNode.chainNodeIds,
      ...(currentNode.protocol === 'chain' ? {} : { rawUri: nodeEditForm.rawUri }),
      remark: nodeEditForm.remark,
    });
    importMessage.value = t('home.messages.nodeUpdated', { name: node.name });
    closeNodeEditDialog();
  } catch (error) {
    nodeEditError.value = error instanceof Error ? error.message : t('home.messages.requestFailed');
  }
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
  routeNodeForm.existingNodeId = routeNodeOptions.value[0]?.id ?? '';
  routeNodeForm.groupId = groups.value[0]?.id ?? '';
  routeNodeForm.mode = 'uri';
  routeNodeSearch.value = '';
  routeNodeGroupId.value = '';
  reloadRouteNodeOptions();
  routeNodeNameEdited.value = false;
  routeNodeError.value = '';
  routeTargetMappingId.value = mapping.id;
}

function selectRouteNode(nodeId: string): void {
  routeNodeForm.existingNodeId = nodeId;
  ensureNodeOptions([nodeId]).catch(() => undefined);
}

watch(
  () => nodeCreateForm.rawUri,
  uri => {
    nodeCreateError.value = '';
    if (nodeCreateNameEdited.value) return;

    nodeCreateForm.name = inferNodeNameFromUri(uri);
  }
);

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
  duplicateRouteNodeDialog.value = null;
  routeNodeForm.name = '';
  routeNodeForm.uri = '';
  routeNodeForm.existingNodeId = '';
  routeNodeForm.groupId = '';
  routeNodeForm.mode = 'uri';
  routeNodeNameEdited.value = false;
  routeNodeError.value = '';
}

function normalizeRouteUri(value: string): string {
  return value.trim();
}

function findDuplicateRouteNode(rawUri: string): ProxyNode | null {
  const normalized = normalizeRouteUri(rawUri);
  if (!normalized) return null;

  return nodes.value.find(node => normalizeRouteUri(node.rawUri) === normalized) ?? null;
}

function attachNodeToMapping(mapping: PortMapping, nodeId: string): Promise<PortMapping> {
  const nodeIds = Array.from(new Set([...mapping.nodeIds, nodeId]));
  const patch: Partial<PortMapping> = { nodeIds };
  if (shouldUseManualActiveRoute(mapping) && !mappingHasActiveRoute(mapping)) {
    patch.activeNodeId = nodeId;
  }
  return updateMapping(mapping.id, patch);
}

async function addUriRouteToMapping(
  mapping: PortMapping,
  options: { forceNew?: boolean } = {}
): Promise<void> {
  if (!options.forceNew) {
    const duplicateNode = findDuplicateRouteNode(routeNodeForm.uri);
    if (duplicateNode) {
      duplicateRouteNodeDialog.value = { node: duplicateNode };
      return;
    }
  }

  const node = await addNodeFromUri(routeNodeForm.uri, routeNodeForm.name);
  await attachNodeToMapping(mapping, node.id);
  closeRouteDialog();
}

function reuseDuplicateRouteNode(): void {
  const duplicate = duplicateRouteNodeDialog.value;
  if (!duplicate) return;

  routeNodeForm.mode = 'node';
  routeNodeForm.existingNodeId = duplicate.node.id;
  routeNodeError.value = '';
  duplicateRouteNodeDialog.value = null;
}

async function forceAddDuplicateRouteNode(): Promise<void> {
  const mapping = routeTargetMapping.value;
  if (!mapping) return;

  duplicateRouteNodeDialog.value = null;

  try {
    await addUriRouteToMapping(mapping, { forceNew: true });
  } catch (error) {
    routeNodeError.value =
      error instanceof Error ? error.message : t('home.messages.requestFailed');
  }
}

function closeDuplicateRouteNodeDialog(): void {
  duplicateRouteNodeDialog.value = null;
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
      await addUriRouteToMapping(mapping);
    } else if (routeNodeForm.mode === 'node') {
      await attachNodeToMapping(mapping, routeNodeForm.existingNodeId);
    } else {
      const groupIds = Array.from(new Set([...mapping.groupIds, routeNodeForm.groupId]));
      const patch: Partial<PortMapping> = { groupIds };
      if (shouldUseManualActiveRoute(mapping) && !mappingHasActiveRoute(mapping)) {
        patch.activeGroupId = routeNodeForm.groupId;
      }
      await updateMapping(mapping.id, patch);
    }
    if (!duplicateRouteNodeDialog.value) closeRouteDialog();
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
  const patch: Partial<PortMapping> = { nodeIds };
  if (shouldUseManualActiveRoute(mapping)) {
    patch.activeNodeId =
      mapping.activeNodeId === nodeId ? nodeIds[0] || null : mapping.activeNodeId;
  }

  try {
    await updateMapping(mapping.id, patch);
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function removeGroupFromMapping(mapping: PortMapping, groupId: string): Promise<void> {
  const groupIds = mapping.groupIds.filter(id => id !== groupId);
  const patch: Partial<PortMapping> = { groupIds };
  if (shouldUseManualActiveRoute(mapping)) {
    patch.activeGroupId =
      mapping.activeGroupId === groupId ? groupIds[0] || null : mapping.activeGroupId;
  }

  try {
    await updateMapping(mapping.id, patch);
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function switchMappingRoute(
  mapping: PortMapping,
  targetType: MappingSwitchTargetType,
  targetId: string
): Promise<void> {
  if (mapping.strategy !== 'manual' || isActiveRoute(mapping, targetType, targetId)) return;

  try {
    await switchMapping(mapping.id, targetType, targetId);
    const message = t('home.messages.routeSwitched');
    importMessage.value = message;
    showCopyMessage(message, 'success');
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

function requestRemoveRoute(mapping: PortMapping, target: ProxyNode | ProxyGroup): void {
  const targetType = 'protocol' in target ? 'node' : 'group';
  const routeKind = t(`home.routeKind.${targetType}`);

  confirmationDialog.value = {
    title: t('home.confirm.removeRouteTitle'),
    message: t('home.confirm.removeRouteMessage', {
      kind: routeKind,
      name: target.name,
      port: mapping.listenPort,
    }),
    confirmLabel: t('common.removeRoute'),
    onConfirm: () =>
      targetType === 'node'
        ? removeNodeFromMapping(mapping, target.id)
        : removeGroupFromMapping(mapping, target.id),
  };
}

function requestRemoveMapping(mapping: PortMapping): void {
  confirmationDialog.value = {
    title: t('home.confirm.deletePortTitle'),
    message: t('home.confirm.deletePortMessage', { port: mapping.listenPort }),
    confirmLabel: t('common.deletePort'),
    onConfirm: () => removeMapping(mapping.id),
  };
}

function requestRemoveNode(node: ProxyNode): void {
  confirmationDialog.value = {
    title: t('home.confirm.deleteNodeTitle'),
    message: t('home.confirm.deleteNodeMessage', { name: node.name }),
    confirmLabel: t('common.deleteNode'),
    onConfirm: () => removeNode(node.id),
  };
}

function requestRemoveGroup(groupId: string): void {
  const group = groupById.value.get(groupId);
  if (!group || group.type !== 'manual') return;

  confirmationDialog.value = {
    title: t('home.confirm.deleteGroupTitle'),
    message: t('home.confirm.deleteGroupMessage', { name: group.name }),
    confirmLabel: t('common.deleteGroup'),
    onConfirm: () => removeGroup(group.id),
  };
}

function openMappingTestDialog(mapping: PortMapping): void {
  testDialog.value = {
    targetType: 'mapping',
    targetId: mapping.id,
    title: t('home.dialogs.testMappingTitle'),
    subtitle: mappingEndpoint(mapping),
    result: null,
    error: '',
  };
  void runCurrentTest();
}

function openNodeTestDialog(node: ProxyNode): void {
  testDialog.value = {
    targetType: 'node',
    targetId: node.id,
    title: t('home.dialogs.testNodeTitle'),
    subtitle: node.name,
    result: null,
    error: '',
  };
  void runCurrentTest();
}

function closeTestDialog(): void {
  cancelCurrentTest();
  testDialog.value = null;
}

function cancelCurrentTest(): void {
  currentTestController?.abort();
  currentTestController = null;
}

function isAbortError(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false;

  return (error as { name?: unknown }).name === 'AbortError';
}

async function runCurrentTest(): Promise<void> {
  const dialog = testDialog.value;
  if (!dialog) return;

  cancelCurrentTest();
  const controller = new AbortController();
  currentTestController = controller;
  const runId = ++currentTestRunId;
  isTesting.value = true;
  dialog.error = '';
  try {
    const result =
      dialog.targetType === 'mapping'
        ? await testMapping(dialog.targetId, testUrl.value, controller.signal)
        : await testNode(dialog.targetId, testUrl.value, controller.signal);
    if (controller.signal.aborted || currentTestRunId !== runId) return;
    testUrl.value = result.probeUrl || testUrl.value;
    if (
      testDialog.value?.targetId === dialog.targetId &&
      testDialog.value.targetType === dialog.targetType
    ) {
      testDialog.value = {
        ...dialog,
        result,
        error: '',
      };
    }
  } catch (error) {
    if (controller.signal.aborted || isAbortError(error) || currentTestRunId !== runId) return;
    if (
      testDialog.value?.targetId === dialog.targetId &&
      testDialog.value.targetType === dialog.targetType
    ) {
      testDialog.value = {
        ...dialog,
        result: null,
        error: error instanceof Error ? error.message : t('home.messages.requestFailed'),
      };
    }
  } finally {
    if (currentTestRunId === runId) {
      isTesting.value = false;
      if (currentTestController === controller) {
        currentTestController = null;
      }
    }
  }
}

function closeConfirmationDialog(): void {
  confirmationDialog.value = null;
}

async function confirmPendingAction(): Promise<void> {
  const dialog = confirmationDialog.value;
  if (!dialog) return;

  try {
    await dialog.onConfirm();
    confirmationDialog.value = null;
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

function copyPopoverText(mappingId: string): string {
  return copiedMappingId.value === mappingId
    ? t('common.copiedEndpoint')
    : t('common.copyEndpoint');
}

function importSignature(raw: string, groupId: string): string {
  return JSON.stringify({ raw: raw.trim(), groupId: groupId.trim() });
}

function subscriptionSignature(): string {
  return JSON.stringify({
    name: subscriptionForm.name.trim(),
    url: subscriptionForm.url.trim(),
  });
}

function resetImportPreview(): void {
  importPreview.value = null;
  importPreviewSignature.value = '';
}

function resetImportDialog(): void {
  rawImport.value = '';
  rawImportGroupId.value = '';
  resetImportPreview();
}

function resetSubscriptionPreview(): void {
  subscriptionPreview.value = null;
  subscriptionPreviewSignature.value = '';
}

function previewSummary(preview: ImportPreviewResult): string {
  const skipped = preview.items.filter(
    item => item.action === 'skip' || item.action === 'fail'
  ).length;
  const imports = preview.items.filter(item => item.action === 'import').length;
  const updates = preview.items.filter(item => item.action === 'update').length;
  return t('home.importPreview.summary', {
    total: preview.total,
    imports,
    updates,
    skipped,
  });
}

function previewTypeLabel(item: { type: string }): string {
  return t(`home.importPreview.type.${item.type}`);
}

function previewActionLabel(item: { action: string }): string {
  return t(`home.importPreview.action.${item.action}`);
}

watch([rawImport, rawImportGroupId], () => {
  resetImportPreview();
});

watch([() => subscriptionForm.name, () => subscriptionForm.url], () => {
  resetSubscriptionPreview();
});

async function handleImport(): Promise<void> {
  const raw = rawImport.value.trim();

  if (!raw) {
    importMessage.value = t('home.messages.importEmpty');
    return;
  }

  try {
    const signature = importSignature(raw, rawImportGroupId.value);
    if (!importPreview.value || importPreviewSignature.value !== signature) {
      importPreview.value = await previewImportNodes(raw, rawImportGroupId.value);
      importPreviewSignature.value = signature;
      importMessage.value = previewSummary(importPreview.value);
      return;
    }

    const result = await importNodes(raw, rawImportGroupId.value);
    if (result.nodes.length > 0 || result.groups.length > 0) {
      rawImport.value = '';
    }
    importPreview.value = result.preview;
    importPreviewSignature.value = '';
    importMessage.value = t('home.messages.importedWithGroups', {
      nodes: result.nodes.length,
      groups: result.groups.length,
      skipped: result.preview.skipped,
    });
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function handleChainNodeSubmit(): Promise<void> {
  if (chainNodeForm.chainNodeIds.length < 2) {
    chainNodeError.value = t('home.messages.chainNodesRequired');
    return;
  }

  try {
    const node = await addNode({
      name: chainNodeForm.name,
      protocol: 'chain',
      server: '',
      port: null,
      username: '',
      password: '',
      rawUri: '',
      tags: [],
      chainNodeIds: chainNodeForm.chainNodeIds,
      groupId: chainNodeForm.groupIds[0] ?? '',
      groupIds: [...chainNodeForm.groupIds],
      remark: chainNodeForm.remark,
    });

    importMessage.value = t('home.messages.chainNodeAdded', { name: node.name });
    closeAddNodeDialog();
  } catch (error) {
    chainNodeError.value =
      error instanceof Error ? error.message : t('home.messages.requestFailed');
  }
}

async function handleSubscriptionSubmit(): Promise<void> {
  if (!subscriptionForm.url.trim()) {
    importMessage.value = t('home.messages.subscriptionUrlRequired');
    return;
  }

  try {
    const signature = subscriptionSignature();
    if (!subscriptionPreview.value || subscriptionPreviewSignature.value !== signature) {
      subscriptionPreview.value = await previewSubscription({
        name: subscriptionForm.name,
        url: subscriptionForm.url,
        groupId: '',
        remark: '',
      });
      subscriptionPreviewSignature.value = signature;
      importMessage.value = previewSummary(subscriptionPreview.value);
      return;
    }

    const subscription = await addSubscription({
      name: subscriptionForm.name,
      url: subscriptionForm.url,
      groupId: '',
      remark: '',
    });
    await syncSubscription(subscription.id);
    subscriptionForm.name = '';
    subscriptionForm.url = '';
    resetSubscriptionPreview();
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
    const wasEditing = editingGroupId.value !== null;
    const input = {
      name: manualGroupForm.name,
      strategy: manualGroupForm.strategy,
      nodeIds: manualGroupForm.nodeIds,
      remark: manualGroupForm.remark,
    };
    const group = editingGroupId.value
      ? await updateGroup(editingGroupId.value, input)
      : await addGroup(input);
    closeGroupDialog();
    importMessage.value = wasEditing
      ? t('home.messages.groupUpdated', { name: group.name })
      : t('home.messages.groupAdded', { name: group.name });
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

function showCopyMessage(message: string, variant: ToastVariant = 'default'): void {
  if (copyMessageTimer.value !== null) {
    window.clearTimeout(copyMessageTimer.value);
  }

  copyMessage.value = message;
  copyMessageVariant.value = variant;
  copyMessageTimer.value = window.setTimeout(() => {
    copyMessage.value = '';
    copyMessageVariant.value = 'default';
    copiedMappingId.value = null;
    copiedNodeId.value = null;
    copyMessageTimer.value = null;
  }, 2200);
}

async function copyTextToClipboard(text: string, successMessage: string): Promise<void> {
  try {
    if (!clipboard.isSupported.value) {
      throw new Error('Clipboard is not supported');
    }
    await clipboard.copy(text);
    showCopyMessage(successMessage);
  } catch {
    showCopyMessage(text);
  }
}

async function copyEndpoint(mapping: PortMapping): Promise<void> {
  const endpoint = copyableMappingEndpoint(mapping);

  copiedMappingId.value = mapping.id;
  copiedNodeId.value = null;
  await copyTextToClipboard(endpoint, t('home.messages.endpointCopied', { endpoint }));
}

async function copyNodeUri(node: ProxyNode): Promise<void> {
  const uri = nodeExportUri(node);
  if (!uri) {
    showCopyMessage(t('home.messages.nodeUriUnavailable'));
    return;
  }

  copiedNodeId.value = node.id;
  copiedMappingId.value = null;
  await copyTextToClipboard(uri, t('home.messages.nodeUriCopied', { name: node.name }));
}

async function handleReset(): Promise<void> {
  try {
    await refreshState();
    importMessage.value = t('home.messages.backendRefreshed');
  } catch {
    // The composable exposes the backend error in the notice bar.
  }
}

async function openTab(tab: TabKey): Promise<void> {
  closeNodeEditDialog();
  if (tab === 'nodes' && currentTab.value !== 'nodes') {
    activeNodeGroupFilter.value = 'all';
  }
  await router.push({ path: tabPath(tab) });
}

async function selectNodeGroupFilterFromPanel(key: NodeGroupFilterKey): Promise<void> {
  selectNodeGroupFilter(key);
  const group = queryValueFromFilterKey(key);
  await router.push({
    path: '/nodes',
    query: group ? { group } : {},
  });
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
  return failure ? runtimeFailureDetail(failure) : portStatusLabel(mapping);
}

function portFailureReason(mapping: PortMapping): string {
  const failure = runtimeFailuresByMappingId.value.get(mapping.id);
  if (!failure) return '';

  return t('home.messages.runtimeFailureReason', {
    reason: runtimeFailureReason(failure.error),
  });
}

function routeLatencyLabel(node: ProxyNode, mapping?: PortMapping): string {
  const runtimeNode = mapping ? runtimeRouteNodeFor(mapping, 'node', node.id) : null;
  if (runtimeNode?.probeRunning) return t('home.nodeHealth.probing');
  if (runtimeNode) {
    if (runtimeNode.error) return t('home.nodeHealth.failedShort');
    const latency = runtimeNode.latencyMs;
    return latency > 0 ? `${latency}ms` : '-ms';
  }
  if (node.health?.probeRunning) return t('home.nodeHealth.probing');
  if (node.health?.lastError) return t('home.nodeHealth.failedShort');
  const latency = node.health?.lastLatencyMs ?? 0;
  return latency > 0 ? `${latency}ms` : '-ms';
}

function routeSuccessLabel(node: ProxyNode): string {
  return String(node.health?.successCount ?? 0);
}

function routeFailureLabel(node: ProxyNode): string {
  return String(node.health?.failureCount ?? 0);
}

function healthTime(value: string | null | undefined): number {
  if (!value) return 0;
  const timestamp = new Date(value).getTime();
  return Number.isFinite(timestamp) ? timestamp : 0;
}

function routeHealthState(
  node: ProxyNode,
  mapping?: PortMapping
): 'success' | 'failure' | 'probing' | 'unknown' {
  const runtimeNode = mapping ? runtimeRouteNodeFor(mapping, 'node', node.id) : null;
  if (runtimeNode) {
    if (runtimeNode.probeRunning) return 'probing';
    if (runtimeNode.error) return 'failure';
    if (runtimeNode.available || runtimeNode.latencyMs > 0) return 'success';
    return 'unknown';
  }
  const health = node.health;
  if (!health) return 'unknown';
  if (health.probeRunning) return 'probing';

  const lastSuccess = healthTime(health.lastSuccessAt);
  const lastFailure = healthTime(health.lastFailureAt);
  if (lastFailure > lastSuccess) return 'failure';
  if (lastSuccess > 0) return 'success';
  if (health.blacklisted || health.lastError?.trim()) return 'failure';
  if (health.available) return 'success';
  if (health.lastCheckedAt) return health.available ? 'success' : 'failure';
  return 'unknown';
}

function isProbeUnavailableNode(node: ProxyNode): boolean {
  const health = node.health;
  return Boolean(health && !health.blacklisted && !health.probeRunning && health.lastError?.trim());
}

function nodeHealthTime(value: string | null | undefined): number | null {
  if (!value) return null;

  const timestamp = new Date(value).getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
}

function nodeHealthDateTime(value: string): string {
  return formatDateTime(value, {
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    month: '2-digit',
    second: '2-digit',
    year: 'numeric',
  });
}

function formatHealthDuration(ms: number): string {
  const minutes = Math.max(1, Math.ceil(ms / 60_000));
  if (minutes < 60) return t('home.nodeHealth.durationMinutes', { count: minutes });

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (hours < 24) {
    return remainingMinutes > 0
      ? t('home.nodeHealth.durationHoursMinutes', {
          hours,
          minutes: remainingMinutes,
        })
      : t('home.nodeHealth.durationHours', { count: hours });
  }

  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  return remainingHours > 0
    ? t('home.nodeHealth.durationDaysHours', { days, hours: remainingHours })
    : t('home.nodeHealth.durationDays', { count: days });
}

function nodeBlacklistRemainingMs(node: ProxyNode): number | null {
  const until = nodeHealthTime(node.health?.blacklistedUntil);
  if (until === null) return null;
  return Math.max(0, until - healthClock.value);
}

function nodeBlacklistLabel(node: ProxyNode): string {
  if (!node.health?.blacklisted) return '';

  const remainingMs = nodeBlacklistRemainingMs(node);
  if (remainingMs === null) return t('home.nodeHealth.blacklistedPendingProbe');
  if (remainingMs === 0) return t('home.nodeHealth.blacklistedRecoveringSoon');

  return t('home.nodeHealth.blacklistedRemaining', {
    duration: formatHealthDuration(remainingMs),
  });
}

function testStatusLabel(result: ProxyTestResult | null, error = ''): string {
  if (isTesting.value) return t('home.test.running');
  if (error) return t('home.test.failed');
  if (!result) return t('home.test.waiting');
  return result.available ? t('home.test.success') : t('home.test.failed');
}

function testLatencyLabel(result: ProxyTestResult | null): string {
  if (!result) return '-';
  return `${Math.max(0, result.latencyMs)}ms`;
}

function testCheckedAtLabel(result: ProxyTestResult | null): string {
  return result?.checkedAt ? formatDateTime(result.checkedAt) : '-';
}

function testNodeLabel(result: ProxyTestResult | null): string {
  if (!result?.nodeName && !result?.nodeId) return '-';
  return [result.nodeName, result.nodeId].filter(Boolean).join(' · ');
}

function testNodeError(result: ProxyTestResult | null): string {
  return result?.nodeError?.trim() || '';
}

function nodeHealthTitle(node: ProxyNode, mapping?: PortMapping): string {
  const runtimeNode = mapping ? runtimeRouteNodeFor(mapping, 'node', node.id) : null;
  if (runtimeNode) {
    const details = [
      t('home.nodeHealth.summary', {
        latency: routeLatencyLabel(node, mapping),
        success: routeSuccessLabel(node),
        failure: routeFailureLabel(node),
      }),
    ];
    if (runtimeNode.probeRunning) {
      details.unshift(t('home.nodeHealth.probing'));
      if (runtimeNode.probeStartedAt) {
        details.push(
          t('home.nodeHealth.probeStartedAt', {
            time: nodeHealthDateTime(runtimeNode.probeStartedAt),
          })
        );
      }
    }
    if (runtimeNode.error) {
      details.push(t('home.nodeHealth.lastError', { reason: runtimeNode.error }));
    }
    if (runtimeNode.lastCheckedAt) {
      details.push(
        t('home.nodeHealth.checkedAt', {
          time: nodeHealthDateTime(runtimeNode.lastCheckedAt),
        })
      );
    }
    return details.join('\n');
  }
  if (!node.health) return '';

  const details = [
    t('home.nodeHealth.summary', {
      latency: routeLatencyLabel(node),
      success: routeSuccessLabel(node),
      failure: routeFailureLabel(node),
    }),
  ];

  if (node.health.probeRunning) {
    details.unshift(t('home.nodeHealth.probing'));
    if (node.health.probeStartedAt) {
      details.push(
        t('home.nodeHealth.probeStartedAt', {
          time: nodeHealthDateTime(node.health.probeStartedAt),
        })
      );
    }
  }

  if (node.health.blacklisted) {
    details.unshift(nodeBlacklistLabel(node));

    if (node.health.blacklistedUntil) {
      details.push(
        t('home.nodeHealth.blacklistedUntilTime', {
          time: nodeHealthDateTime(node.health.blacklistedUntil),
        })
      );
    }
  }

  const error = node.health?.lastError?.trim();
  if (error) {
    details.push(t('home.nodeHealth.lastError', { reason: error }));
  }

  if (node.health.lastCheckedAt) {
    details.push(
      t('home.nodeHealth.checkedAt', {
        time: nodeHealthDateTime(node.health.lastCheckedAt),
      })
    );
  }

  return details.join('\n');
}

const homeContext = {
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
  isActiveRoute,
  switchMappingRoute,
  openNodeTestDialog,
  requestRemoveRoute,
  protocolLabels,
  routeHealthState,
  nodeHealthTitle,
  isProbeUnavailableNode,
  routeLatencyLabel,
  routeSuccessLabel,
  routeFailureLabel,
  mappingGroups,
  groupRouteAvailabilityLabel,
  groupRouteAvailableUnavailable,
  groupRouteLatencyLabel,
  groupRouteHealthState,
  groupRouteHealthTitle,
  openNewMappingDialog,
  nodeSearch,
  hideEmptyNodeGroups,
  nodeGroupFilterOptions,
  activeNodeGroupFilter,
  selectNodeGroupFilter: selectNodeGroupFilterFromPanel,
  groupSummaryItems,
  selectedGroup,
  selectedNodeGroupTitle,
  selectedNodeGroupHealthSummary,
  currentNodeTotal,
  selectedNodeGroupNodes,
  nodeListContainerProps,
  nodeListWrapperProps,
  virtualNodeRows,
  nodeEndpointLabel,
  nodeUriPopoverText,
  nodeExportUri,
  copyNodeUri,
  openEditNodeDialog,
  requestRemoveNode,
  nodeBlacklistLabel,
  isLoadingNodes,
  loadNextNodePage,
  groups,
  groupFilterOptions,
  optionProtocolLabel,
  optionNameLabel,
  optionEndpointLabel,
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
  openEditGroupById,
  openEditGroupDialog,
  requestRemoveGroup,
  handleManualGroupSubmit,
  groupSummary,
  removeGroup,
  subscriptionForm,
  handleSubscriptionSubmit,
  subscriptionPreview,
  previewSummary,
  previewTypeLabel,
  previewActionLabel,
  subscriptions,
  syncExistingSubscription,
  removeSubscription,
  subscriptionGroupName,
  formatDateTime,
  rawImport,
  rawImportGroupId,
  importPreview,
  handleImport,
} satisfies HomeViewContext;
</script>

<template>
  <main class="console-shell">
    <section class="shell-header">
      <header class="brand-bar">
        <div class="brand-lockup">
          <img class="brand-logo" :src="proxyHubMarkUrl" alt="" aria-hidden="true" />
          <span class="brand-name">{{ t('app.name') }}</span>
          <AppVersionBadge />
        </div>

        <div class="brand-actions">
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                class="language-menu-trigger"
                :aria-label="currentLanguageTitle"
                :title="currentLanguageTitle"
              >
                <Languages class="size-4" aria-hidden="true" />
                <span>{{ t('common.language') }}</span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" :side-offset="8" class="language-menu">
              <DropdownMenuItem
                v-for="option in languagePreferenceOptions"
                :key="option.value"
                :class="[
                  'language-menu-item',
                  { active: localePreference === option.value },
                ]"
                @select="setLocalePreference(option.value)"
              >
                <Check
                  v-if="localePreference === option.value"
                  class="size-4"
                  aria-hidden="true"
                />
                <span v-else class="language-menu-check-placeholder" aria-hidden="true"></span>
                <span>{{ option.label }}</span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <a
            class="github-link"
            href="https://github.com/fy0/proxy-hub"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="GitHub"
            title="GitHub"
          >
            <Github class="size-4" aria-hidden="true" />
            <span>GitHub</span>
          </a>
          <RouterLink
            class="settings-link"
            :to="{ name: 'settings' }"
            :title="t('common.settings')"
          >
            <Settings class="size-4" aria-hidden="true" />
            <span>{{ t('common.settings') }}</span>
          </RouterLink>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            class="status-refresh"
            :aria-label="t('common.refreshStatus')"
            :title="t('common.refreshStatus')"
            :disabled="isLoading || isSaving"
            @click="handleReset"
          >
            <RefreshCw
              class="size-4"
              :class="{ 'spin-icon': isLoading || isSaving }"
              aria-hidden="true"
            />
            <span>{{ t('common.refreshStatus') }}</span>
          </Button>
        </div>
      </header>

      <section
        v-if="showExtraUiInfo"
        class="notice-bar"
        :class="{ error: hasNoticeError }"
        role="status"
      >
        <span class="notice-icon" aria-hidden="true"></span>
        <span class="notice-message">{{ backendNotice }}</span>
        <RouterLink v-if="loginRequired" class="notice-link" :to="loginRoute">
          {{ t('common.goLogin') }}
        </RouterLink>
      </section>
    </section>

    <section class="workspace-panel">
      <header v-if="showExtraUiInfo" class="workspace-header">
        <div class="workspace-copy">
          <div class="workspace-title-row">
            <h1>{{ workspaceTitle }}</h1>
            <span v-if="currentTab === 'mappings'" class="workspace-count">{{
              mappingCountLabel
            }}</span>
            <span v-else-if="currentTab === 'nodes'" class="workspace-count">{{ nodeTotal }}</span>
            <span v-else-if="currentTab === 'groups'" class="workspace-count">{{
              groups.length + 1
            }}</span>
          </div>
          <p>{{ workspaceLead }}</p>
        </div>

        <div
          v-if="currentTab === 'mappings'"
          class="port-strip"
          :aria-label="t('home.aria.activePorts')"
        >
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
      </header>

      <div class="workspace-toolbar">
        <HomeTabs
          :current-tab="currentTab"
          :groups-label="groupsTabLabel"
          :nodes-label="nodesTabLabel"
          :compact-groups-label="groupsTabCompactLabel"
          :compact-nodes-label="nodesTabCompactLabel"
          :compact-mappings-label="mappingsTabCompactLabel"
          @select="openTab"
        />

        <Button
          v-if="currentTab === 'mappings'"
          type="button"
          class="top-add-port-button"
          @click="openNewMappingDialog"
        >
          <Plus class="size-4" aria-hidden="true" />
          <span>{{ t('common.addPort') }}</span>
        </Button>

        <div v-else-if="currentTab === 'nodes'" class="top-add-node-control">
          <Button type="button" class="top-add-node-button" @click="openAddNodeDialog('uri')">
            <Plus class="size-4" aria-hidden="true" />
            <span>{{ t('common.addNode') }}</span>
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                type="button"
                class="top-add-node-menu-button"
                :aria-label="t('home.aria.addNodeMenu')"
                :title="t('home.aria.addNodeMenu')"
              >
                <ChevronDown class="size-4" aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" :side-offset="8" class="add-node-menu">
              <DropdownMenuItem class="add-node-menu-item" @select="openAddNodeDialog('uri')">
                <Link2 class="size-4" aria-hidden="true" />
                <span>{{ t('home.nodeCreate.uriNode') }}</span>
              </DropdownMenuItem>
              <DropdownMenuItem class="add-node-menu-item" @select="openAddNodeDialog('chain')">
                <Route class="size-4" aria-hidden="true" />
                <span>{{ t('home.nodeCreate.chainNode') }}</span>
              </DropdownMenuItem>
              <DropdownMenuItem class="add-node-menu-item" @select="openAddNodeDialog('import')">
                <Download class="size-4" aria-hidden="true" />
                <span>{{ t('home.nodeCreate.batchImport') }}</span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <Button
          v-else-if="currentTab === 'groups'"
          type="button"
          class="top-add-group-button"
          @click="openNewGroupDialog"
        >
          <Plus class="size-4" aria-hidden="true" />
          <span>{{ t('common.addGroup') }}</span>
        </Button>
      </div>

      <Transition name="workspace-switch" mode="out-in">
        <MappingsPanel v-if="currentTab === 'mappings'" key="mappings" :context="homeContext" />
        <NodesPanel v-else-if="currentTab === 'nodes'" key="nodes" :context="homeContext" />
        <GroupsPanel v-else-if="currentTab === 'groups'" key="groups" :context="homeContext" />
      </Transition>
    </section>

    <TransitionGroup name="modal-pop" tag="div" class="modal-layer">
      <div
        v-if="addNodeDialogMode === 'uri'"
        key="add-node-uri"
        class="modal-backdrop"
        role="presentation"
        @click.self="closeAddNodeDialog"
      >
      <form
        class="modal-card node-create-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="node-create-dialog-title"
        @submit.prevent="saveNodeCreateDialog"
      >
        <div class="modal-heading">
          <div>
            <h2 id="node-create-dialog-title">{{ t('home.dialogs.addNodeTitle') }}</h2>
            <p>{{ t('home.dialogs.addNodeLead') }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeAddNodeDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <label>
          <span>{{ t('home.form.nodeName') }}</span>
          <input
            v-model.trim="nodeCreateForm.name"
            type="text"
            autocomplete="off"
            :placeholder="t('home.placeholders.nodeName')"
            @input="handleNodeCreateNameInput"
          />
        </label>

        <label>
          <span>{{ t('home.form.nodeUri') }} <em class="required-mark">*</em></span>
          <input
            v-model.trim="nodeCreateForm.rawUri"
            type="text"
            required
            autocomplete="off"
            :placeholder="t('home.placeholders.nodeUri')"
          />
        </label>

        <div class="node-edit-group-field" role="group" :aria-label="t('home.form.nodeGroup')">
          <span class="node-edit-group-label">{{ t('home.form.nodeGroup') }}</span>
          <button
            type="button"
            class="node-group-collapse-trigger"
            :aria-expanded="nodeCreateGroupsExpanded"
            @click="nodeCreateGroupsExpanded = !nodeCreateGroupsExpanded"
          >
            <strong>{{ groupSelectionSummary(nodeCreateForm.groupIds) }}</strong>
            <ChevronDown class="size-4" aria-hidden="true" />
          </button>
          <div
            class="node-group-collapse-panel"
            :data-expanded="nodeCreateGroupsExpanded"
            :aria-hidden="!nodeCreateGroupsExpanded"
          >
            <div class="node-edit-group-options">
              <label
                class="node-edit-default-group"
                :class="{ selected: nodeCreateForm.groupIds.length === 0 }"
              >
                <input
                  type="checkbox"
                  :checked="nodeCreateForm.groupIds.length === 0"
                  @change="selectNodeCreateDefaultGroup"
                />
                {{ t('home.groupMeta.ungrouped') }}
              </label>
              <label
                v-for="group in groups"
                :key="group.id"
                :class="{ selected: nodeCreateForm.groupIds.includes(group.id) }"
              >
                <input
                  type="checkbox"
                  :checked="nodeCreateForm.groupIds.includes(group.id)"
                  @change="toggleNodeCreateGroup(group.id)"
                />
                {{ group.name }}
              </label>
            </div>
          </div>
        </div>

        <label>
          <span>{{ t('home.form.remark') }}</span>
          <input
            v-model.trim="nodeCreateForm.remark"
            type="text"
            :placeholder="t('common.optional')"
          />
        </label>

        <p v-if="nodeCreateError" class="route-node-error">{{ nodeCreateError }}</p>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeAddNodeDialog">{{
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
      v-if="addNodeDialogMode === 'chain'"
      key="add-node-chain"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeAddNodeDialog"
    >
      <form
        class="modal-card chain-node-create-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="chain-node-create-dialog-title"
        @submit.prevent="handleChainNodeSubmit"
      >
        <div class="modal-heading">
          <div>
            <h2 id="chain-node-create-dialog-title">{{ t('home.dialogs.addChainNodeTitle') }}</h2>
            <p>{{ t('home.sections.chainLead') }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeAddNodeDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <label>
          <span>{{ t('home.form.chainName') }}</span>
          <input
            v-model.trim="chainNodeForm.name"
            type="text"
            autocomplete="off"
            :placeholder="t('home.placeholders.chainName')"
            required
          />
        </label>

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
              <NodeGroupFilterSelect
                v-model="chainNodeGroupId"
                :options="groupFilterOptions()"
                :aria-label="t('home.form.nodeGroup')"
              />
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

        <div class="node-edit-group-field" role="group" :aria-label="t('home.form.nodeGroup')">
          <span class="node-edit-group-label">{{ t('home.form.nodeGroup') }}</span>
          <button
            type="button"
            class="node-group-collapse-trigger"
            :aria-expanded="chainNodeGroupsExpanded"
            @click="chainNodeGroupsExpanded = !chainNodeGroupsExpanded"
          >
            <strong>{{ groupSelectionSummary(chainNodeForm.groupIds) }}</strong>
            <ChevronDown class="size-4" aria-hidden="true" />
          </button>
          <div
            class="node-group-collapse-panel"
            :data-expanded="chainNodeGroupsExpanded"
            :aria-hidden="!chainNodeGroupsExpanded"
          >
            <div class="node-edit-group-options">
              <label
                class="node-edit-default-group"
                :class="{ selected: chainNodeForm.groupIds.length === 0 }"
              >
                <input
                  type="checkbox"
                  :checked="chainNodeForm.groupIds.length === 0"
                  @change="selectChainNodeDefaultGroup"
                />
                {{ t('home.groupMeta.ungrouped') }}
              </label>
              <label
                v-for="group in groups"
                :key="group.id"
                :class="{ selected: chainNodeForm.groupIds.includes(group.id) }"
              >
                <input
                  type="checkbox"
                  :checked="chainNodeForm.groupIds.includes(group.id)"
                  @change="toggleChainNodeGroup(group.id)"
                />
                {{ group.name }}
              </label>
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

        <p v-if="chainNodeError" class="route-node-error">{{ chainNodeError }}</p>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeAddNodeDialog">{{
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
      v-if="isGroupDialogOpen"
      key="group-dialog"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeGroupDialog"
    >
      <form
        class="modal-card group-create-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="group-create-dialog-title"
        @submit.prevent="handleManualGroupSubmit"
      >
        <div class="modal-heading">
          <div>
            <h2 id="group-create-dialog-title">
              {{ editingGroupId ? t('home.dialogs.editGroupTitle') : t('common.addGroup') }}
            </h2>
            <p>
              {{
                editingGroupId ? t('home.dialogs.editGroupLead') : t('home.dialogs.addGroupLead')
              }}
            </p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeGroupDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <div class="field-grid two">
          <label>
            <span>{{ t('home.form.groupName') }}</span>
            <input v-model.trim="manualGroupForm.name" type="text" required />
          </label>
          <label>
            <span>{{ t('home.form.groupStrategy') }}</span>
            <select v-model="manualGroupForm.strategy">
              <option value="selector">{{ t('home.groupStrategy.selector') }}</option>
              <option value="load-balance">{{ t('home.groupStrategy.load-balance') }}</option>
              <option value="url-test">{{ t('home.groupStrategy.url-test') }}</option>
              <option value="least-latency">{{ t('home.groupStrategy.least-latency') }}</option>
            </select>
          </label>
        </div>

        <div class="chain-node-builder">
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
                <NodeGroupFilterSelect
                  v-model="manualGroupNodeGroupId"
                  :options="groupFilterOptions()"
                  :aria-label="t('home.form.nodeGroup')"
                />
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
            </div>
          </label>
          <div class="chain-node-preview">
            <strong>{{ t('home.form.groupNodes') }}</strong>
            <span v-if="manualGroupForm.nodeIds.length">{{
              t('home.groupMeta.nodeCount', { count: manualGroupForm.nodeIds.length })
            }}</span>
            <span v-else>{{ t('home.groupMeta.noSelectedNodes') }}</span>
            <div v-if="manualGroupForm.nodeIds.length" class="chain-node-order">
              <button
                v-for="(node, index) in selectedManualGroupNodes()"
                :key="node.id"
                type="button"
                @click="toggleManualGroupNode(node.id)"
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
            v-model.trim="manualGroupForm.remark"
            type="text"
            :placeholder="t('common.optional')"
          />
        </label>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeGroupDialog">{{
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
      v-if="addNodeDialogMode === 'import'"
      key="add-node-import"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeAddNodeDialog"
    >
      <form
        class="modal-card import-node-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="import-node-dialog-title"
        @submit.prevent="handleImport"
      >
        <div class="modal-heading">
          <div>
            <h2 id="import-node-dialog-title">{{ t('home.dialogs.importNodeTitle') }}</h2>
            <p>{{ t('home.dialogs.importNodeLead') }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeAddNodeDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <label>
          <span>{{ t('home.form.shareLink') }}</span>
          <textarea
            v-model="rawImport"
            rows="6"
            required
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

        <div v-if="importPreview" class="import-preview-panel">
          <div class="import-preview-heading">
            <strong>{{ t('home.importPreview.title') }}</strong>
            <span>{{ previewSummary(importPreview) }}</span>
          </div>
          <div class="import-preview-list">
            <article
              v-for="(item, index) in importPreview.items"
              :key="`${item.type}-${item.name}-${index}`"
              class="import-preview-item"
              :class="{ muted: item.action === 'skip' || item.action === 'fail' }"
            >
              <span>{{ previewTypeLabel(item) }}</span>
              <strong>{{ item.name }}</strong>
              <small>{{ item.detail || previewActionLabel(item) }}</small>
            </article>
          </div>
        </div>

        <span class="inline-message">{{ importMessage }}</span>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeAddNodeDialog">{{
            t('common.cancel')
          }}</Button>
          <Button type="submit">
            <Download class="size-4" aria-hidden="true" />
            {{ importPreview ? t('home.importPreview.confirmImport') : t('common.importLinks') }}
          </Button>
        </div>
      </form>
    </div>

    <div
      v-if="isMappingDialogOpen"
      key="mapping-dialog"
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
      key="route-dialog"
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
          <fieldset class="route-source-field">
            <legend>{{ t('home.form.routeSource') }}</legend>
            <div class="route-source-options">
              <label
                v-for="option in routeSourceOptions"
                :key="option.value"
                class="route-source-option"
                :class="{ active: routeNodeForm.mode === option.value }"
              >
                <input v-model="routeNodeForm.mode" type="radio" :value="option.value" />
                <span>
                  <component :is="option.icon" class="size-4" aria-hidden="true" />
                  {{ option.label }}
                </span>
              </label>
            </div>
          </fieldset>

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
            <div class="node-picker-box">
              <div class="node-option-toolbar">
                <input
                  v-model.trim="routeNodeSearch"
                  type="search"
                  autocomplete="off"
                  :placeholder="t('home.placeholders.nodeSearch')"
                />
                <NodeGroupFilterSelect
                  v-model="routeNodeGroupId"
                  :options="groupFilterOptions()"
                  :aria-label="t('home.form.nodeGroup')"
                />
              </div>
              <div class="route-node-option-list">
                <button
                  v-for="node in routeNodeOptions"
                  :key="node.id"
                  type="button"
                  :class="{ selected: routeNodeForm.existingNodeId === node.id }"
                  @click="selectRouteNode(node.id)"
                >
                  <em>{{ optionProtocolLabel(node) }}</em>
                  <span>{{ optionNameLabel(node) }}</span>
                  <small>{{ optionEndpointLabel(node) }}</small>
                </button>
                <Button
                  v-if="routeNodeOptions.length < routeNodeTotal"
                  type="button"
                  variant="outline"
                  :disabled="isLoadingRouteNodes"
                  @click="loadMoreRouteNodeOptions"
                >
                  {{
                    isLoadingRouteNodes
                      ? t('home.messages.loadingNodes')
                      : t('home.actions.loadMore')
                  }}
                </Button>
              </div>
            </div>
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

    <div
      v-if="editingNode"
      key="node-edit-dialog"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeNodeEditDialog"
    >
      <form
        class="modal-card node-edit-modal"
        :class="{ 'chain-node-edit-modal': editingNode.protocol === 'chain' }"
        role="dialog"
        aria-modal="true"
        aria-labelledby="node-edit-dialog-title"
        @submit.prevent="saveNodeEditDialog"
      >
        <div class="modal-heading">
          <div>
            <h2 id="node-edit-dialog-title">{{ t('home.dialogs.editNodeTitle') }}</h2>
            <p>{{ editingNode.name }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeNodeEditDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <label>
          <span>{{ t('home.form.nodeName') }}</span>
          <input
            v-model.trim="nodeEditForm.name"
            type="text"
            autocomplete="off"
            :placeholder="t('home.placeholders.nodeName')"
          />
        </label>

        <label v-if="editingNode.protocol !== 'chain'">
          <span>{{ t('home.form.nodeUri') }} <em class="required-mark">*</em></span>
          <input
            v-model.trim="nodeEditForm.rawUri"
            type="text"
            required
            autocomplete="off"
            :placeholder="t('home.placeholders.nodeUri')"
          />
        </label>

        <div v-if="editingNode.protocol === 'chain'" class="chain-node-builder">
          <fieldset>
            <span>{{ t('home.form.chainNodes') }}</span>
            <div class="node-option-toolbar">
              <input
                v-model.trim="editChainNodeSearch"
                type="search"
                autocomplete="off"
                :placeholder="t('home.placeholders.nodeSearch')"
              />
              <NodeGroupFilterSelect
                v-model="editChainNodeGroupId"
                :options="groupFilterOptions()"
                :aria-label="t('home.form.nodeGroup')"
              />
            </div>
            <div class="chain-node-options">
              <label
                v-for="node in editChainNodeOptions"
                :key="node.id"
                :class="{ selected: nodeEditForm.chainNodeIds.includes(node.id) }"
              >
                <input
                  type="checkbox"
                  :checked="nodeEditForm.chainNodeIds.includes(node.id)"
                  @change="toggleNodeEditChainNodeSelection(node.id)"
                />
                <span class="node-option-card">
                  <em>{{ optionProtocolLabel(node) }}</em>
                  <strong>{{ optionNameLabel(node) }}</strong>
                  <small>{{ optionEndpointLabel(node) }}</small>
                </span>
              </label>
              <Button
                v-if="editChainNodeOptions.length < editChainNodeTotal"
                type="button"
                variant="outline"
                :disabled="isLoadingEditChainNodes"
                @click="loadMoreEditChainOptions"
              >
                {{
                  isLoadingEditChainNodes
                    ? t('home.messages.loadingNodes')
                    : t('home.actions.loadMore')
                }}
              </Button>
            </div>
          </fieldset>
          <div class="chain-node-preview">
            <strong>{{ t('home.form.chainPreview') }}</strong>
            <span v-if="nodeEditForm.chainNodeIds.length">{{ nodeEditChainPreview() }}</span>
            <span v-else>{{ t('home.nodeMeta.chainEmpty') }}</span>
            <div v-if="nodeEditForm.chainNodeIds.length" class="chain-node-order">
              <button
                v-for="(node, index) in selectedNodeEditChainNodes()"
                :key="node.id"
                type="button"
                @click="removeNodeEditChainNodeSelection(node.id)"
              >
                <em>{{ index + 1 }}</em>
                <span>{{ node.name }}</span>
                <X class="size-3" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>

        <div class="node-edit-group-field" role="group" :aria-label="t('home.form.nodeGroup')">
          <span class="node-edit-group-label">{{ t('home.form.nodeGroup') }}</span>
          <button
            type="button"
            class="node-group-collapse-trigger"
            :aria-expanded="nodeEditGroupsExpanded"
            @click="nodeEditGroupsExpanded = !nodeEditGroupsExpanded"
          >
            <strong>{{ groupSelectionSummary(nodeEditForm.groupIds) }}</strong>
            <ChevronDown class="size-4" aria-hidden="true" />
          </button>
          <div
            class="node-group-collapse-panel"
            :data-expanded="nodeEditGroupsExpanded"
            :aria-hidden="!nodeEditGroupsExpanded"
          >
            <div class="node-edit-group-options">
              <label
                class="node-edit-default-group"
                :class="{ selected: nodeEditForm.groupIds.length === 0 }"
              >
                <input
                  type="checkbox"
                  :checked="nodeEditForm.groupIds.length === 0"
                  @change="selectNodeEditDefaultGroup"
                />
                {{ t('home.groupMeta.ungrouped') }}
              </label>
              <label
                v-for="group in groups"
                :key="group.id"
                :class="{ selected: nodeEditForm.groupIds.includes(group.id) }"
              >
                <input
                  type="checkbox"
                  :checked="nodeEditForm.groupIds.includes(group.id)"
                  @change="toggleNodeEditGroup(group.id)"
                />
                {{ group.name }}
              </label>
            </div>
          </div>
        </div>

        <label>
          <span>{{ t('home.form.remark') }}</span>
          <input
            v-model.trim="nodeEditForm.remark"
            type="text"
            :placeholder="t('common.optional')"
          />
        </label>

        <p v-if="nodeEditError" class="route-node-error">{{ nodeEditError }}</p>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeNodeEditDialog">{{
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
      v-if="confirmationDialog"
      key="confirmation-dialog"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeConfirmationDialog"
    >
      <section
        class="modal-card confirm-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
      >
        <div class="modal-heading">
          <div>
            <h2 id="confirm-dialog-title">{{ confirmationDialog.title }}</h2>
            <p>{{ confirmationDialog.message }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeConfirmationDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="closeConfirmationDialog">{{
            t('common.cancel')
          }}</Button>
          <Button type="button" variant="destructive" @click="confirmPendingAction">
            <Trash2 class="size-4" aria-hidden="true" />
            {{ confirmationDialog.confirmLabel }}
          </Button>
        </div>
      </section>
    </div>

    <div
      v-if="duplicateRouteNodeDialog"
      key="duplicate-route-dialog"
      class="modal-backdrop"
      role="presentation"
      @click.self="closeDuplicateRouteNodeDialog"
    >
      <section
        class="modal-card duplicate-route-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="duplicate-route-dialog-title"
      >
        <div class="modal-heading">
          <div>
            <h2 id="duplicate-route-dialog-title">
              {{ t('home.confirm.duplicateRouteTitle') }}
            </h2>
            <p>
              {{
                t('home.confirm.duplicateRouteMessage', {
                  name: duplicateRouteNodeDialog.node.name,
                })
              }}
            </p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeDuplicateRouteNodeDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <div class="modal-actions">
          <Button type="button" variant="outline" @click="forceAddDuplicateRouteNode">
            <Plus class="size-4" aria-hidden="true" />
            {{ t('home.confirm.duplicateRouteForce') }}
          </Button>
          <Button type="button" @click="reuseDuplicateRouteNode">
            <Link2 class="size-4" aria-hidden="true" />
            {{ t('home.confirm.duplicateRouteReuse') }}
          </Button>
        </div>
      </section>
    </div>

      <div
        v-if="testDialog"
        key="test-dialog"
        class="modal-backdrop"
        role="presentation"
        @click.self="closeTestDialog"
      >
      <section
        class="modal-card test-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="test-dialog-title"
      >
        <div class="modal-heading">
          <div>
            <h2 id="test-dialog-title">{{ testDialog.title }}</h2>
            <p>{{ testDialog.subtitle }}</p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('common.close')"
            :title="t('common.close')"
            @click="closeTestDialog"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <form class="test-url-form" @submit.prevent="runCurrentTest">
          <label>
            <span>{{ t('home.form.testUrl') }}</span>
            <input
              v-model.trim="testUrl"
              type="url"
              autocomplete="off"
              :placeholder="t('home.placeholders.testUrl')"
              required
            />
          </label>
          <Button type="submit" :disabled="isTesting">
            <RefreshCw class="size-4" :class="{ 'spin-icon': isTesting }" aria-hidden="true" />
            {{ t('home.test.retest') }}
          </Button>
        </form>

        <div
          class="test-result-panel"
          :class="{
            success: testDialog.result?.available,
            failed: testDialog.result && !testDialog.result.available,
          }"
        >
          <div class="test-result-summary">
            <span class="test-result-icon">
              <Gauge class="size-4" aria-hidden="true" />
            </span>
            <div>
              <strong>{{ testStatusLabel(testDialog.result, testDialog.error) }}</strong>
              <small>{{ testDialog.result?.probeUrl || testUrl }}</small>
            </div>
          </div>

          <dl class="test-result-grid">
            <div>
              <dt>{{ t('home.test.latency') }}</dt>
              <dd>{{ testLatencyLabel(testDialog.result) }}</dd>
            </div>
            <div>
              <dt>{{ t('home.test.checkedAt') }}</dt>
              <dd>{{ testCheckedAtLabel(testDialog.result) }}</dd>
            </div>
            <div>
              <dt>{{ t('home.test.target') }}</dt>
              <dd>{{ testDialog.result?.targetName || testDialog.subtitle }}</dd>
            </div>
            <div>
              <dt>{{ t('home.test.result') }}</dt>
              <dd>{{ testStatusLabel(testDialog.result, testDialog.error) }}</dd>
            </div>
            <div v-if="testDialog.result?.nodeId || testDialog.result?.nodeName">
              <dt>{{ t('home.test.node') }}</dt>
              <dd>{{ testNodeLabel(testDialog.result) }}</dd>
            </div>
            <div v-if="testDialog.result?.nodeTag">
              <dt>{{ t('home.test.nodeTag') }}</dt>
              <dd>{{ testDialog.result.nodeTag }}</dd>
            </div>
          </dl>

          <p v-if="testDialog.error || testDialog.result?.error" class="test-error">
            {{ testDialog.error || testDialog.result?.error }}
          </p>
          <p v-if="testNodeError(testDialog.result)" class="test-error">
            {{ testNodeError(testDialog.result) }}
          </p>
        </div>
      </section>
    </div>
    </TransitionGroup>

    <Transition name="toast-slide">
      <p
        v-if="copyMessage"
        class="toast-message"
        :class="`toast-${copyMessageVariant}`"
        role="status"
      >
        {{ copyMessage }}
      </p>
    </Transition>
  </main>
</template>
