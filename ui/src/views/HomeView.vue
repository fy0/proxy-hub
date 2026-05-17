<script setup lang="ts">
import { useClipboard, useVirtualList } from '@vueuse/core';
import { computed, onMounted, reactive, ref, watch } from 'vue';
import {
  Check,
  Copy,
  Edit3,
  Import,
  Gauge,
  Link2,
  MoreVertical,
  Plus,
  Power,
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import ActionTooltip from '@/components/ActionTooltip.vue';
import { inferNodeNameFromUri, useProxyHubState } from '@/composables/useProxyHubState';
import { useI18n } from '@/i18n';
import { useAppStore } from '@/stores/app';
import { formatVersionForDisplay } from '@/utils/versionDisplay';
import type {
  ImportPreviewResult,
  OutboundProtocol,
  PortMapping,
  ProxyGroup,
  ProxyNode,
  ProxyNodeOption,
  ProxyTestResult,
  ProxyProtocol,
  RouteStrategy,
  RuntimeExcludedNode,
} from '@/types/proxyHub';
import './home.css';

type TabKey = 'mappings' | 'nodes' | 'subscriptions' | 'import';
type NodeGroupFilterKey = 'all' | 'summary' | 'default' | `group:${string}`;
type PortRuntimeState = 'running' | 'failed' | 'closed' | 'notRunning';
type RouteActionTargetType = 'node' | 'group';
type RouteNodeMode = 'uri' | 'node' | 'group';

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
const appStore = useAppStore();
const clipboard = useClipboard({
  legacy: true,
});

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
}));

const strategyOptions = computed<Array<{ label: string; value: RouteStrategy }>>(() => [
  { label: strategyLabels.value.failover, value: 'failover' },
  { label: strategyLabels.value['load-balance'], value: 'load-balance' },
  { label: strategyLabels.value.manual, value: 'manual' },
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
  groupById,
  runtime,
  isLoading,
  isSaving,
  errorMessage,
  loginRequired,
  refreshState,
  addNode,
  addNodeFromUri,
  previewImportNodes,
  importNodes,
  updateNode,
  removeNode,
  testNode,
  addGroup,
  removeGroup,
  previewSubscription,
  addSubscription,
  syncSubscription,
  removeSubscription,
  addMapping,
  updateMapping,
  removeMapping,
  testMapping,
  loadNodes,
  loadMoreNodes,
  fetchNodeOptions,
  ensureNodeOptions,
} = useProxyHubState();

const currentTab = ref<TabKey>('mappings');
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
const copyMessageTimer = ref<number | null>(null);
const copiedMappingId = ref<string | null>(null);
const copiedNodeId = ref<string | null>(null);
const editingMappingId = ref<string | null>(null);
const editingNodeId = ref<string | null>(null);
const routeTargetMappingId = ref<string | null>(null);
const openRouteActionKey = ref<string | null>(null);
const confirmationDialog = ref<ConfirmationDialog | null>(null);
const duplicateRouteNodeDialog = ref<DuplicateRouteNodeDialog | null>(null);
const testDialog = ref<TestDialogState | null>(null);
const testUrl = ref('https://www.gstatic.com/generate_204');
const isTesting = ref(false);

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
  mode: 'uri' as RouteNodeMode,
});
const routeNodeNameEdited = ref(false);
const routeNodeError = ref('');

const routeSourceOptions = computed(() => [
  { value: 'uri' as const, label: t('home.routeSource.uri'), icon: Link2 },
  { value: 'node' as const, label: t('home.routeSource.node'), icon: Server },
  { value: 'group' as const, label: t('home.routeSource.group'), icon: Users },
]);
const displayAppVersion = computed(() => formatVersionForDisplay(appStore.appInfo.version));

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

const nodeEditForm = reactive({
  name: '',
  groupIds: [] as string[],
  chainNodeIds: [] as string[],
  rawUri: '',
  remark: '',
});
const nodeEditError = ref('');

const chainNodeForm = reactive({
  name: '',
  chainNodeIds: [] as string[],
  groupId: '',
  remark: '',
});

const subscriptionForm = reactive({
  name: '',
  url: '',
  groupId: '',
  remark: '',
});
const subscriptionPreview = ref<ImportPreviewResult | null>(null);
const subscriptionPreviewSignature = ref('');

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

const runtimeExcludedNodes = computed<RuntimeExcludedNode[]>(() => {
  const excludedNodes = (runtime.value as { excludedNodes?: RuntimeExcludedNode[] } | null)
    ?.excludedNodes;
  return excludedNodes ?? [];
});

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
const runtimeExcludedNodeCount = computed(() => runtimeExcludedNodes.value.length);

const runtimeExcludedNodeNotice = computed(() => {
  if (runtimeExcludedNodeCount.value === 0) return '';
  return t('home.messages.runtimeExcludedNodes', { count: runtimeExcludedNodeCount.value });
});

const backendNotice = computed(() => {
  if (errorMessage.value) return errorMessage.value;
  if (isLoading.value) return t('home.messages.loadingBackend');
  if (isSaving.value) return t('home.messages.savingBackend');
  if (runtime.value?.error)
    return t('home.messages.runtimeError', { message: runtime.value.error });
  if (runtime.value?.running) {
    const runningMessage = t('home.messages.runtimeRunning', { count: runtimeInboundCount.value });
    return runtimeExcludedNodeNotice.value
      ? `${runningMessage} ${runtimeExcludedNodeNotice.value}`
      : runningMessage;
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

const nodesTabLabel = computed(() => t('home.tabs.nodesWithCount', { count: nodeTotal.value }));
const mappingCountLabel = computed(
  () => `${enabledMappings.value.length}/${mappings.value.length}`
);

const workspaceTitle = computed(() => {
  if (currentTab.value === 'nodes') return t('home.sections.nodesTitle');
  if (currentTab.value === 'subscriptions') return t('home.sections.subscriptionsTitle');
  if (currentTab.value === 'import') return t('home.sections.importTitle');

  return t('home.sections.mappingsTitle');
});

const workspaceLead = computed(() => {
  if (currentTab.value === 'nodes') return t('home.sections.nodesLead');
  if (currentTab.value === 'subscriptions') return t('home.sections.subscriptionsLead');
  if (currentTab.value === 'import') return t('home.sections.importLead');

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
  return nodeById.value.get(id) ?? optionToProxyNode(nodeOptionById.value.get(id) ?? nullOption(id));
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

const manualGroups = computed(() => groups.value.filter(group => group.type === 'manual'));
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
    key: 'summary',
    label: t('home.groupFilters.summary'),
    countLabel: t('home.groupMeta.groupCount', { count: visibleGroups.value.length + 1 }),
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
  return nodes.value;
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

async function reloadCurrentNodes(): Promise<void> {
  if (activeNodeGroupFilter.value === 'summary') return;
  await loadNodes(nodeListQuery.value);
  scrollNodeListTo(0);
}

async function loadNextNodePage(): Promise<void> {
  if (activeNodeGroupFilter.value === 'summary') return;
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
  },
  ...visibleGroups.value.map(group => ({
    key: toGroupFilterKey(group.id),
    title: group.name,
    typeLabel: t(`home.groupType.${group.type}`),
    count: group.nodeCount,
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
  return nodes.value.filter(
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
    const selectedResult = input.selectedIds?.length
      ? await fetchNodeOptions({ ids: input.selectedIds, size: Math.min(200, input.selectedIds.length) })
      : null;
    const merged = mergeNodeOptions(
      append ? target.options.value : [],
      selectedResult?.items ?? [],
      result.items
    );
    target.options.value = merged;
    target.total.value = result.total;
    target.page.value = result.page;
  } finally {
    target.loading.value = false;
  }
}

function mergeNodeOptions(...groupsToMerge: ProxyNodeOption[][]): ProxyNodeOption[] {
  const byId = new Map<string, ProxyNodeOption>();
  for (const items of groupsToMerge) {
    for (const item of items) byId.set(item.id, item);
  }
  return Array.from(byId.values());
}

function groupFilterOptions(includeAll = true): Array<{ id: string; label: string }> {
  return [
    ...(includeAll ? [{ id: '', label: t('home.groupFilters.all') }] : []),
    { id: '__default__', label: t('home.groupFilters.default') },
    ...groups.value.map(group => ({ id: group.id, label: group.name })),
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
  return nodeIds
    .map(id => nodeFromCache(id))
    .filter((node): node is ProxyNode => Boolean(node));
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

function toggleManualGroupNode(nodeId: string): void {
  if (manualGroupForm.nodeIds.includes(nodeId)) {
    manualGroupForm.nodeIds = manualGroupForm.nodeIds.filter(id => id !== nodeId);
    return;
  }
  manualGroupForm.nodeIds = [...manualGroupForm.nodeIds, nodeId];
  ensureNodeOptions([nodeId]).catch(() => undefined);
}

function selectedManualGroupNodes(): ProxyNode[] {
  return selectedChainNodesFromIds(manualGroupForm.nodeIds);
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

function resetNodeEditForm(): void {
  Object.assign(nodeEditForm, {
    name: '',
    groupIds: [],
    chainNodeIds: [],
    rawUri: '',
    remark: '',
  });
  nodeEditError.value = '';
}

function openEditNodeDialog(node: ProxyNode): void {
  closeRouteActionMenu();
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
  closeRouteActionMenu();
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
  return updateMapping(mapping.id, {
    nodeIds,
    activeNodeId: mapping.activeNodeId || nodeId,
  });
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
      await updateMapping(mapping.id, {
        groupIds,
        activeGroupId: mapping.activeGroupId || routeNodeForm.groupId,
      });
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

function routeActionKey(
  mapping: PortMapping,
  targetType: RouteActionTargetType,
  targetId: string
): string {
  return `${mapping.id}:${targetType}:${targetId}`;
}

function isRouteActionMenuOpen(
  mapping: PortMapping,
  targetType: RouteActionTargetType,
  targetId: string
): boolean {
  return openRouteActionKey.value === routeActionKey(mapping, targetType, targetId);
}

function toggleRouteActionMenu(
  mapping: PortMapping,
  targetType: RouteActionTargetType,
  targetId: string
): void {
  const nextKey = routeActionKey(mapping, targetType, targetId);
  openRouteActionKey.value = openRouteActionKey.value === nextKey ? null : nextKey;
}

function closeRouteActionMenu(): void {
  openRouteActionKey.value = null;
}

function requestRemoveRoute(mapping: PortMapping, target: ProxyNode | ProxyGroup): void {
  closeRouteActionMenu();
  const targetType: RouteActionTargetType = 'protocol' in target ? 'node' : 'group';
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
  closeRouteActionMenu();
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

function openMappingTestDialog(mapping: PortMapping): void {
  closeRouteActionMenu();
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
  closeRouteActionMenu();
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
  if (isTesting.value) return;
  testDialog.value = null;
}

async function runCurrentTest(): Promise<void> {
  const dialog = testDialog.value;
  if (!dialog) return;

  isTesting.value = true;
  dialog.error = '';
  try {
    const result =
      dialog.targetType === 'mapping'
        ? await testMapping(dialog.targetId, testUrl.value)
        : await testNode(dialog.targetId, testUrl.value);
    testUrl.value = result.probeUrl || testUrl.value;
    if (testDialog.value?.targetId === dialog.targetId) {
      testDialog.value = {
        ...dialog,
        result,
        error: '',
      };
    }
  } catch (error) {
    if (testDialog.value?.targetId === dialog.targetId) {
      testDialog.value = {
        ...dialog,
        result: null,
        error: error instanceof Error ? error.message : t('home.messages.requestFailed'),
      };
    }
  } finally {
    isTesting.value = false;
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
    groupId: subscriptionForm.groupId.trim(),
    remark: subscriptionForm.remark.trim(),
  });
}

function resetImportPreview(): void {
  importPreview.value = null;
  importPreviewSignature.value = '';
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

watch(
  () => [
    subscriptionForm.name,
    subscriptionForm.url,
    subscriptionForm.groupId,
    subscriptionForm.remark,
  ],
  () => {
    resetSubscriptionPreview();
  }
);

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
      chainNodeIds: [],
      groupId: manualNodeForm.groupId,
      groupIds: manualNodeForm.groupId ? [manualNodeForm.groupId] : [],
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

async function handleChainNodeSubmit(): Promise<void> {
  if (chainNodeForm.chainNodeIds.length < 2) {
    importMessage.value = t('home.messages.chainNodesRequired');
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
      groupId: chainNodeForm.groupId,
      groupIds: chainNodeForm.groupId ? [chainNodeForm.groupId] : [],
      remark: chainNodeForm.remark,
    });

    chainNodeForm.name = '';
    chainNodeForm.chainNodeIds = [];
    chainNodeForm.groupId = '';
    chainNodeForm.remark = '';
    importMessage.value = t('home.messages.chainNodeAdded', { name: node.name });
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
    const signature = subscriptionSignature();
    if (!subscriptionPreview.value || subscriptionPreviewSignature.value !== signature) {
      subscriptionPreview.value = await previewSubscription({
        name: subscriptionForm.name,
        url: subscriptionForm.url,
        groupId: subscriptionForm.groupId,
        remark: subscriptionForm.remark,
      });
      subscriptionPreviewSignature.value = signature;
      importMessage.value = previewSummary(subscriptionPreview.value);
      return;
    }

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

function showCopyMessage(message: string): void {
  if (copyMessageTimer.value !== null) {
    window.clearTimeout(copyMessageTimer.value);
  }

  copyMessage.value = message;
  copyMessageTimer.value = window.setTimeout(() => {
    copyMessage.value = '';
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

function openTab(tab: TabKey): void {
  closeRouteActionMenu();
  closeNodeEditDialog();
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
  return failure ? runtimeFailureDetail(failure) : portStatusLabel(mapping);
}

function portFailureReason(mapping: PortMapping): string {
  const failure = runtimeFailuresByMappingId.value.get(mapping.id);
  if (!failure) return '';

  return t('home.messages.runtimeFailureReason', {
    reason: runtimeFailureReason(failure.error),
  });
}

function routeLatencyLabel(node: ProxyNode): string {
  const latency = node.health?.lastLatencyMs ?? 0;
  return latency > 0 ? `${latency}ms` : '-ms';
}

function routeSuccessLabel(node: ProxyNode): string {
  return String(node.health?.successCount ?? 0);
}

function routeFailureLabel(node: ProxyNode): string {
  return String(node.health?.failureCount ?? 0);
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

function nodeHealthTitle(node: ProxyNode): string {
  const error = node.health?.lastError?.trim();
  if (!node.health?.blacklisted && !error) return '';
  if (node.health?.blacklisted && error) {
    return t('home.nodeHealth.blacklistedWithReason', { reason: error });
  }
  if (node.health?.blacklisted) return t('home.nodeHealth.blacklisted');
  return error ?? '';
}
</script>

<template>
  <main class="console-shell" @click="closeRouteActionMenu">
    <section class="shell-header">
      <header class="brand-bar">
        <div class="brand-lockup">
          <span class="brand-logo" aria-hidden="true">
            <span class="brand-logo-core"></span>
          </span>
          <span class="brand-name">{{ t('app.name') }}</span>
          <span class="brand-version">v{{ displayAppVersion }}</span>
        </div>

        <div class="brand-actions">
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

      <section class="notice-bar" :class="{ error: hasNoticeError }" role="status">
        <span class="notice-icon" aria-hidden="true"></span>
        <span class="notice-message">{{ backendNotice }}</span>
        <RouterLink v-if="loginRequired" class="notice-link" :to="loginRoute">
          {{ t('common.goLogin') }}
        </RouterLink>
      </section>
    </section>

    <section class="workspace-panel">
      <header class="workspace-header">
        <div class="workspace-copy">
          <div class="workspace-title-row">
            <h1>{{ workspaceTitle }}</h1>
            <span v-if="currentTab === 'mappings'" class="workspace-count">{{
              mappingCountLabel
            }}</span>
            <span v-else-if="currentTab === 'nodes'" class="workspace-count">{{
              nodeTotal
            }}</span>
            <span v-else-if="currentTab === 'subscriptions'" class="workspace-count">{{
              subscriptions.length
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
        <nav class="tab-bar" :aria-label="t('home.aria.tabs')">
          <button
            :class="{ active: currentTab === 'mappings' }"
            type="button"
            @click="openTab('mappings')"
          >
            <Link2 class="size-4" aria-hidden="true" />
            <span>{{ t('home.tabs.mappings') }}</span>
          </button>
          <button
            :class="{ active: currentTab === 'nodes' }"
            type="button"
            @click="openTab('nodes')"
          >
            <Server class="size-4" aria-hidden="true" />
            <span>{{ nodesTabLabel }}</span>
          </button>
          <button
            :class="{ active: currentTab === 'subscriptions' }"
            type="button"
            @click="openTab('subscriptions')"
          >
            <RefreshCw class="size-4" aria-hidden="true" />
            <span>{{ t('home.tabs.subscriptions') }}</span>
          </button>
          <button
            :class="{ active: currentTab === 'import' }"
            type="button"
            @click="openTab('import')"
          >
            <Import class="size-4" aria-hidden="true" />
            <span>{{ t('home.tabs.import') }}</span>
          </button>
        </nav>

        <Button
          v-if="currentTab === 'mappings'"
          type="button"
          class="top-add-port-button"
          @click="openNewMappingDialog"
        >
          <Plus class="size-4" aria-hidden="true" />
          <span>{{ t('common.addPort') }}</span>
        </Button>
      </div>

      <section v-if="currentTab === 'mappings'" class="panel port-panel">
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
                  :class="{ blacklisted: node.health?.blacklisted }"
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
                  <span class="route-card-meta">
                    <span class="route-kind-badge group">{{ t('home.routeKind.group') }}</span>
                    <span class="route-detail">{{
                      t(`home.groupStrategy.${group.strategy}`)
                    }}</span>
                  </span>
                </div>
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

      <section v-else-if="currentTab === 'nodes'" class="panel simple-panel">
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
            <div
              v-bind="nodeListContainerProps"
              class="node-virtual-scroll"
            >
              <div v-bind="nodeListWrapperProps">
                <article
                  v-for="row in virtualNodeRows"
                  :key="row.data.id"
                  class="node-row"
                  :class="{ blacklisted: row.data.health?.blacklisted }"
                  :style="{ height: '116px' }"
                  :title="nodeHealthTitle(row.data)"
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
                    <small v-if="row.data.protocol !== 'chain' && !row.data.username && !row.data.password">{{
                      t('common.noAccount')
                    }}</small>
                    <small v-if="row.data.health?.blacklisted" class="blacklisted">{{
                      t('home.nodeHealth.blacklisted')
                    }}</small>
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
                  <option
                    v-for="group in groupFilterOptions()"
                    :key="group.id"
                    :value="group.id"
                  >
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
                  {{ isLoadingChainNodes ? t('home.messages.loadingNodes') : t('home.actions.loadMore') }}
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
                    <option
                      v-for="group in groupFilterOptions()"
                      :key="group.id"
                      :value="group.id"
                    >
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

      <section v-else-if="currentTab === 'subscriptions'" class="panel simple-panel">
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
              {{
                subscriptionPreview
                  ? t('home.importPreview.confirmSubscription')
                  : t('home.importPreview.previewSubscription')
              }}
            </Button>
          </form>
        </div>

        <div v-if="subscriptionPreview" class="import-preview-panel">
          <div class="import-preview-heading">
            <strong>{{ t('home.importPreview.title') }}</strong>
            <span>{{ previewSummary(subscriptionPreview) }}</span>
          </div>
          <div class="import-preview-list">
            <article
              v-for="(item, index) in subscriptionPreview.items"
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
                  <option value="shadowsocks">{{ protocolLabels.shadowsocks }}</option>
                  <option value="tuic">{{ protocolLabels.tuic }}</option>
                  <option value="ssh">{{ protocolLabels.ssh }}</option>
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

        <div class="button-row">
          <Button type="button" @click="handleImport">
            <Import class="size-4" aria-hidden="true" />
            {{ importPreview ? t('home.importPreview.confirmImport') : t('common.importLinks') }}
          </Button>
          <span class="inline-message">{{ importMessage }}</span>
        </div>
      </section>
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
                <select v-model="routeNodeGroupId">
                  <option
                    v-for="group in groupFilterOptions()"
                    :key="group.id"
                    :value="group.id"
                  >
                    {{ group.label }}
                  </option>
                </select>
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
              <select v-model="editChainNodeGroupId">
                <option
                  v-for="group in groupFilterOptions()"
                  :key="group.id"
                  :value="group.id"
                >
                  {{ group.label }}
                </option>
              </select>
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

        <fieldset class="node-edit-group-field">
          <span>{{ t('home.form.nodeGroup') }}</span>
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
        </fieldset>

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
            :disabled="isTesting"
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

        <div class="test-result-panel" :class="{ success: testDialog.result?.available, failed: testDialog.result && !testDialog.result.available }">
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
          </dl>

          <p v-if="testDialog.error || testDialog.result?.error" class="test-error">
            {{ testDialog.error || testDialog.result?.error }}
          </p>
        </div>
      </section>
    </div>

    <p v-if="copyMessage" class="toast-message" role="status">{{ copyMessage }}</p>
  </main>
</template>
