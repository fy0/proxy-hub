import { computed, ref } from 'vue';
import {
  deleteProxyGroupsById,
  deleteProxyMappingsById,
  deleteProxyNodesById,
  deleteProxySubscriptionsById,
  getProxyNodeOptions,
  getProxyNodesHealth,
  getProxyNodes,
  getProxyRuntimeStatus,
  getProxyState,
  postProxyGroups,
  postProxyMappings,
  postProxyMappingsByIdTest,
  postProxyNodes,
  postProxyNodesByIdTest,
  postProxyNodesImport,
  postProxyNodesImportPreview,
  postProxySubscriptions,
  postProxySubscriptionsPreview,
  postProxySubscriptionsByIdSync,
  putProxyGroupsById,
  putProxyMappingsById,
  putProxyNodesById,
  putProxySubscriptionsById,
} from '@/api/generated';
import type {
  GroupUpsertRequestWritable,
  NodeImportPreviewItem,
  NodeImportResult,
  MappingUpsertRequestWritable,
  NodeUpsertRequestWritable,
  PortMappingDto,
  ProxyGroupDto,
  ProxyNodeDto,
  ProxyNodeOptionDto,
  ProxySubscriptionDto,
  ProxyTestResultDto,
  RuntimeStatus,
  StateSnapshotDto,
  SubscriptionUpsertRequestWritable,
} from '@/api/generated';
import { isAuthCredentialError } from '@/api/auth';
import type {
  OutboundProtocol,
  ImportPreviewAction,
  ImportPreviewItem,
  ImportPreviewResult,
  ImportPreviewType,
  PortMapping,
  ProxyGroup,
  ProxyGroupStrategy,
  ProxyGroupType,
  ProxyHubStateSnapshot,
  ProxyNode,
  ProxyNodeHealth,
  ProxyNodeOption,
  ProxyProtocol,
  ProxySubscription,
  ProxyTestResult,
  RouteStrategy,
} from '@/types/proxyHub';
import { t } from '@/i18n';

interface NodeInput {
  name: string;
  protocol: ProxyProtocol;
  server: string;
  port: number | null;
  username: string;
  password: string;
  rawUri: string;
  tags: string[];
  chainNodeIds: string[];
  groupId: string;
  groupIds: string[];
  remark: string;
}

interface MappingInput {
  listenAddress: string;
  listenPort: number;
  outboundProtocol: OutboundProtocol;
  username: string;
  password: string;
  strategy: RouteStrategy;
  nodeIds: string[];
  activeNodeId: string | null;
  groupIds: string[];
  activeGroupId: string | null;
  enabled: boolean;
  remark: string;
}

interface GroupInput {
  name: string;
  strategy: ProxyGroupStrategy;
  nodeIds: string[];
  groupIds: string[];
  remark: string;
}

interface SubscriptionInput {
  name: string;
  url: string;
  groupId: string;
  remark: string;
}

export interface ImportNodesResult {
  nodes: ProxyNode[];
  groups: ProxyGroup[];
  preview: ImportPreviewResult;
}

export interface NodeQueryInput {
  keyword?: string;
  groupId?: string;
  defaultOnly?: boolean;
  physicalOnly?: boolean;
  page?: number;
  size?: number;
  withHealth?: boolean;
}

export interface NodeOptionQueryInput {
  keyword?: string;
  nameOnly?: boolean;
  groupId?: string;
  defaultOnly?: boolean;
  physicalOnly?: boolean;
  page?: number;
  size?: number;
  ids?: string[];
}

const nodes = ref<ProxyNode[]>([]);
const nodeTotal = ref(0);
const currentNodeTotal = ref(0);
const defaultNodeTotal = ref(0);
const nodesPage = ref(1);
const nodesPageSize = ref(50);
const isLoadingNodes = ref(false);
const nodeOptionsCache = ref<Record<string, ProxyNodeOption>>({});
const nodeHealthById = ref<Record<string, ProxyNodeHealth>>({});
const groups = ref<ProxyGroup[]>([]);
const subscriptions = ref<ProxySubscription[]>([]);
const mappings = ref<PortMapping[]>([]);
const lastSavedAt = ref<string | null>(null);
const runtime = ref<RuntimeStatus | null>(null);
const isLoading = ref(false);
const activeMutations = ref(0);
const errorMessage = ref('');
const loginRequired = ref(false);
const isSaving = computed(() => activeMutations.value > 0);

let didInitialLoad = false;
let initialLoadPromise: Promise<void> | null = null;

function normalizeProtocol(value: string | null | undefined): ProxyProtocol {
  const protocol = value?.toLowerCase().replace(':', '') ?? 'unknown';

  if (protocol === 'https') return 'http';
  if (protocol === 'socks') return 'socks5';
  if (protocol === 'ss') return 'shadowsocks';
  if (protocol === 'hy2') return 'hysteria2';
  if (
    protocol === 'vless' ||
    protocol === 'vmess' ||
    protocol === 'trojan' ||
    protocol === 'socks5' ||
    protocol === 'http' ||
    protocol === 'shadowsocks' ||
    protocol === 'hysteria' ||
    protocol === 'hysteria2' ||
    protocol === 'tuic' ||
    protocol === 'ssh' ||
    protocol === 'chain'
  ) {
    return protocol;
  }

  return 'unknown';
}

function normalizeOutboundProtocol(value: string | null | undefined): OutboundProtocol {
  const protocol = value?.toLowerCase() ?? 'mixed';

  if (protocol === 'mixed' || protocol === 'socks5' || protocol === 'http') {
    return protocol;
  }

  return 'mixed';
}

function normalizeStrategy(value: string | null | undefined): RouteStrategy {
  const strategy = value?.toLowerCase() ?? 'manual';

  if (strategy === 'failover' || strategy === 'load-balance' || strategy === 'manual') {
    return strategy;
  }

  return 'manual';
}

function normalizeGroupType(value: string | null | undefined): ProxyGroupType {
  return value === 'subscription' ? 'subscription' : 'manual';
}

function normalizeGroupStrategy(value: string | null | undefined): ProxyGroupStrategy {
  return value === 'url-test' ? 'url-test' : 'selector';
}

function normalizePreviewType(value: string | null | undefined): ImportPreviewType {
  if (value === 'group' || value === 'builtin' || value === 'failure') return value;
  return 'node';
}

function normalizePreviewAction(value: string | null | undefined): ImportPreviewAction {
  if (value === 'update' || value === 'skip' || value === 'fail') return value;
  return 'import';
}

function toPort(value: string | number | null | undefined): number | null {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
    return null;
  }

  return parsed;
}

function toRequestPort(value: number | null | undefined): number | undefined {
  return toPort(value) ?? undefined;
}

function decodeBase64Payload(value: string): string {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=');
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, char => char.charCodeAt(0));

  return new TextDecoder().decode(bytes);
}

function stringValue(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : '';
}

function normalizeTransportTag(value: string | null): string {
  const transport = value?.trim().toLowerCase() ?? '';
  if (!transport || transport === 'tcp' || transport === 'raw' || transport === 'none') return '';
  if (transport === 'websocket') return 'ws';
  return transport;
}

function uriTags(protocol: ProxyProtocol, searchParams: URLSearchParams): string[] {
  const tags: string[] = [protocol].filter(tag => tag !== 'unknown');
  const transport = normalizeTransportTag(
    searchParams.get('type') || searchParams.get('network') || ''
  );
  if (transport) tags.push(transport);

  const security = (searchParams.get('security') || '').trim().toLowerCase();
  const tls = (searchParams.get('tls') || '').trim().toLowerCase();
  if (security === 'tls' || security === 'reality') tags.push(security);
  if (!security && ['1', 'true', 'yes', 'y', 'tls'].includes(tls)) tags.push('tls');

  return Array.from(new Set(tags));
}

function parseVmessUri(rawUri: string): NodeInput | null {
  if (!rawUri.toLowerCase().startsWith('vmess://')) return null;

  try {
    const payload = rawUri.replace(/^vmess:\/\//i, '').trim();
    const parsed = JSON.parse(decodeBase64Payload(payload)) as Record<string, unknown>;
    const server = stringValue(parsed.add).trim();
    const transport = normalizeTransportTag(stringValue(parsed.net));
    const name =
      stringValue(parsed.ps).trim() || (server ? `VMess ${server}` : t('state.node.vmessDefault'));

    return {
      name,
      protocol: 'vmess',
      server,
      port: toPort(stringValue(parsed.port)),
      username: stringValue(parsed.id).trim(),
      password: '',
      rawUri,
      tags: ['vmess', transport].filter(Boolean),
      chainNodeIds: [],
      groupId: '',
      groupIds: [],
      remark: '',
    };
  } catch {
    return null;
  }
}

function parseProxyUri(rawValue: string): NodeInput {
  const rawUri = rawValue.trim();
  const fallbackName = rawUri ? rawUri.slice(0, 32) : t('state.node.unnamed');
  const vmessInput = parseVmessUri(rawUri);

  if (vmessInput) {
    return vmessInput;
  }

  try {
    const parsed = new URL(rawUri);
    const protocol = normalizeProtocol(parsed.protocol);
    const nameFromHash = decodeURIComponent(parsed.hash.replace(/^#/, '')).trim();
    const name = nameFromHash || `${protocol.toUpperCase()} ${parsed.hostname || fallbackName}`;
    const username = decodeURIComponent(parsed.username || '').trim();
    const password = decodeURIComponent(parsed.password || '').trim();
    const passwordOnlyProtocol =
      protocol === 'trojan' || protocol === 'hysteria' || protocol === 'hysteria2';
    const queryPassword = parsed.searchParams.get('password')?.trim() ?? '';

    return {
      name,
      protocol,
      server: parsed.hostname,
      port: toPort(parsed.port),
      username: passwordOnlyProtocol ? '' : username,
      password: passwordOnlyProtocol ? username : password || (protocol === 'tuic' ? queryPassword : ''),
      rawUri,
      tags: uriTags(protocol, parsed.searchParams),
      chainNodeIds: [],
      groupId: '',
      groupIds: [],
      remark: '',
    };
  } catch {
    const protocol = normalizeProtocol(rawUri.match(/^([a-z0-9+.-]+):\/\//i)?.[1]);

    return {
      name: fallbackName,
      protocol,
      server: '',
      port: null,
      username: '',
      password: '',
      rawUri,
      tags: [protocol].filter(tag => tag !== 'unknown'),
      chainNodeIds: [],
      groupId: '',
      groupIds: [],
      remark: t('state.node.unsupportedRemark'),
    };
  }
}

export function inferNodeNameFromUri(rawUri: string): string {
  const value = rawUri.trim();
  if (!/^[a-z0-9+.-]+:\/\//i.test(value)) return '';

  return parseProxyUri(value).name;
}

function toProxyNode(dto: ProxyNodeDto): ProxyNode {
  return {
    id: dto.id,
    name: dto.name,
    protocol: normalizeProtocol(dto.protocol),
    server: dto.server,
    port: toPort(dto.port),
    username: dto.username,
    password: dto.password,
    rawUri: dto.rawUri,
    tags: dto.tags ?? [],
    remark: dto.remark,
    chainNodeIds: dto.chainNodeIds ?? [],
    subscriptionId: dto.subscriptionId,
    groupId: dto.groupId,
    groupIds: dto.groupIds ?? (dto.groupId ? [dto.groupId] : []),
    sourceKey: dto.sourceKey,
    health: toProxyNodeHealth(dto.health),
    createdAt: dto.createdAt,
    updatedAt: dto.updatedAt,
  };
}

function toProxyNodeOption(dto: ProxyNodeOptionDto): ProxyNodeOption {
  return {
    id: dto.id,
    name: dto.name,
    protocol: normalizeProtocol(dto.protocol),
    server: dto.server,
    port: toPort(dto.port),
    groupIds: dto.groupIds ?? [],
  };
}

function toProxyNodeHealth(dto: ProxyNodeDto['health']): ProxyNodeHealth | null {
  if (!dto) return null;

  return {
    nodeId: dto.nodeId,
    available: dto.available,
    failureCount: dto.failureCount,
    successCount: dto.successCount,
    blacklisted: dto.blacklisted,
    blacklistedUntil: dto.blacklistedUntil,
    lastLatencyMs: dto.lastLatencyMs,
    lastError: dto.lastError,
    lastCheckedAt: dto.lastCheckedAt,
    lastSuccessAt: dto.lastSuccessAt,
    lastFailureAt: dto.lastFailureAt,
    updatedAt: dto.updatedAt,
  };
}

function cacheNodeHealth(items: Array<ProxyNodeHealth | null | undefined>): void {
  const healthItems = items.filter((item): item is ProxyNodeHealth => Boolean(item?.nodeId));
  if (healthItems.length === 0) return;

  const next = { ...nodeHealthById.value };
  for (const health of healthItems) {
    next[health.nodeId] = health;
  }
  nodeHealthById.value = next;
}

function toProxyTestResult(dto: ProxyTestResultDto): ProxyTestResult {
  return {
    targetType: dto.targetType,
    targetId: dto.targetId,
    targetName: dto.targetName,
    probeUrl: dto.probeUrl,
    available: dto.available,
    latencyMs: dto.latencyMs,
    error: dto.error ?? '',
    checkedAt: dto.checkedAt,
    health: toProxyNodeHealth(dto.health),
    nodeId: dto.nodeId ?? '',
    nodeName: dto.nodeName ?? '',
    nodeTag: dto.nodeTag ?? '',
    nodeError: dto.nodeError ?? '',
  };
}

function toProxySubscription(dto: ProxySubscriptionDto): ProxySubscription {
  return {
    id: dto.id,
    name: dto.name,
    url: dto.url,
    groupId: dto.groupId,
    remark: dto.remark,
    lastSyncedAt: dto.lastSyncedAt,
    lastSyncStatus: dto.lastSyncStatus,
    lastSyncError: dto.lastSyncError,
    createdAt: dto.createdAt,
    updatedAt: dto.updatedAt,
  };
}

function toProxyGroup(dto: ProxyGroupDto): ProxyGroup {
  return {
    id: dto.id,
    name: dto.name,
    type: normalizeGroupType(dto.type),
    strategy: normalizeGroupStrategy(dto.strategy),
    subscriptionId: dto.subscriptionId,
    sourceKey: dto.sourceKey,
    nodeIds: dto.nodeIds ?? [],
    groupIds: dto.groupIds ?? [],
    builtinTags: dto.builtinTags ?? [],
    includesAll: dto.includesAll,
    filter: dto.filter,
    remark: dto.remark,
    nodeCount: dto.nodeCount ?? dto.nodeIds?.length ?? 0,
    createdAt: dto.createdAt,
    updatedAt: dto.updatedAt,
  };
}

function toPortMapping(dto: PortMappingDto): PortMapping {
  return {
    id: dto.id,
    enabled: dto.enabled,
    listenAddress: dto.listenAddress,
    listenPort: dto.listenPort,
    order: dto.order ?? 0,
    outboundProtocol: normalizeOutboundProtocol(dto.outboundProtocol),
    username: dto.username,
    password: dto.password,
    strategy: normalizeStrategy(dto.strategy),
    nodeIds: dto.nodeIds ?? [],
    activeNodeId: dto.activeNodeId,
    groupIds: dto.groupIds ?? [],
    activeGroupId: dto.activeGroupId,
    remark: dto.remark,
    createdAt: dto.createdAt,
    updatedAt: dto.updatedAt,
  };
}

function toImportPreviewItem(dto: NodeImportPreviewItem): ImportPreviewItem {
  return {
    type: normalizePreviewType(dto.type),
    name: dto.name,
    action: normalizePreviewAction(dto.action),
    reason: dto.reason ?? '',
    detail: dto.detail ?? '',
  };
}

function toImportPreviewResult(dto: NodeImportResult): ImportPreviewResult {
  return {
    items: (dto.previewItems ?? []).map(toImportPreviewItem),
    total: dto.total,
    imported: dto.imported,
    failed: dto.failed,
    updated: dto.updated,
    deleted: dto.deleted,
    skipped: dto.skipped,
  };
}

function nodeToOption(node: ProxyNode): ProxyNodeOption {
  return {
    id: node.id,
    name: node.name,
    protocol: node.protocol,
    server: node.server,
    port: node.port,
    groupIds: node.groupIds,
  };
}

function cacheNodeOptions(options: ProxyNodeOption[]): void {
  if (options.length === 0) return;
  const next = { ...nodeOptionsCache.value };
  for (const option of options) {
    next[option.id] = option;
  }
  nodeOptionsCache.value = next;
}

function applySnapshot(snapshot: StateSnapshotDto): void {
  nodes.value = (snapshot.nodes ?? []).map(toProxyNode);
  nodeTotal.value = snapshot.nodeTotal ?? nodes.value.length;
  currentNodeTotal.value = nodes.value.length;
  defaultNodeTotal.value = snapshot.defaultTotal ?? 0;
  groups.value = (snapshot.groups ?? []).map(toProxyGroup);
  subscriptions.value = (snapshot.subscriptions ?? []).map(toProxySubscription);
  mappings.value = (snapshot.mappings ?? []).map(toPortMapping);
  runtime.value = snapshot.runtime;
  lastSavedAt.value = snapshot.lastSavedAt;
  cacheNodeOptions(nodes.value.map(nodeToOption));
  cacheNodeHealth(nodes.value.map(node => node.health));
}

function markSaved(timestamp = new Date().toISOString()): void {
  lastSavedAt.value = timestamp;
}

function errorToMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }

  if (typeof error === 'object' && error !== null) {
    const candidate = error as Record<string, unknown>;
    for (const key of ['message', 'detail', 'title']) {
      const value = candidate[key];
      if (typeof value === 'string' && value.trim() !== '') {
        return value;
      }
    }
  }

  return t('home.messages.requestFailed');
}

function clearBackendError(): void {
  errorMessage.value = '';
  loginRequired.value = false;
}

function setBackendError(error: unknown): void {
  errorMessage.value = errorToMessage(error);
  loginRequired.value = isAuthCredentialError(error);
}

async function runMutation<T>(work: () => Promise<T>): Promise<T> {
  activeMutations.value += 1;
  clearBackendError();

  try {
    return await work();
  } catch (error) {
    setBackendError(error);
    throw error;
  } finally {
    activeMutations.value = Math.max(0, activeMutations.value - 1);
  }
}

function upsertNode(node: ProxyNode): void {
  const index = nodes.value.findIndex(item => item.id === node.id);
  if (index === -1) {
    nodes.value = [node, ...nodes.value];
    nodeTotal.value += 1;
    cacheNodeOptions([nodeToOption(node)]);
    return;
  }

  nodes.value = nodes.value.map(item => (item.id === node.id ? node : item));
  cacheNodeOptions([nodeToOption(node)]);
}

function upsertGroup(group: ProxyGroup): void {
  const index = groups.value.findIndex(item => item.id === group.id);
  if (index === -1) {
    groups.value = [group, ...groups.value];
    return;
  }

  groups.value = groups.value.map(item => (item.id === group.id ? group : item));
}

function upsertSubscription(subscription: ProxySubscription): void {
  const index = subscriptions.value.findIndex(item => item.id === subscription.id);
  if (index === -1) {
    subscriptions.value = [subscription, ...subscriptions.value];
    return;
  }

  subscriptions.value = subscriptions.value.map(item =>
    item.id === subscription.id ? subscription : item
  );
}

function upsertMapping(mapping: PortMapping): void {
  const index = mappings.value.findIndex(item => item.id === mapping.id);
  if (index === -1) {
    mappings.value = [...mappings.value, mapping];
    return;
  }

  mappings.value = mappings.value.map(item => (item.id === mapping.id ? mapping : item));
}

function nodeToRequest(input: NodeInput): NodeUpsertRequestWritable {
  return {
    name: input.name.trim() || t('state.node.unnamed'),
    protocol: input.protocol,
    server: input.server.trim(),
    port: toRequestPort(input.port),
    username: input.username.trim(),
    password: input.password.trim(),
    rawUri: input.rawUri.trim(),
    tags: input.tags.map(tag => tag.trim()).filter(Boolean),
    chainNodeIds: input.chainNodeIds.map(nodeId => nodeId.trim()).filter(Boolean),
    groupId: input.groupId.trim() || undefined,
    groupIds: input.groupIds.map(groupId => groupId.trim()).filter(Boolean),
    remark: input.remark.trim(),
  };
}

function mappingToRequest(input: MappingInput): MappingUpsertRequestWritable {
  return {
    enabled: input.enabled,
    listenAddress: input.listenAddress.trim() || '0.0.0.0',
    listenPort: toPort(input.listenPort) ?? input.listenPort,
    outboundProtocol: normalizeOutboundProtocol(input.outboundProtocol),
    username: input.username.trim(),
    password: input.password.trim(),
    strategy: normalizeStrategy(input.strategy),
    nodeIds: input.nodeIds,
    activeNodeId: input.activeNodeId ?? undefined,
    groupIds: input.groupIds,
    activeGroupId: input.activeGroupId ?? undefined,
    remark: input.remark.trim(),
  };
}

function groupToRequest(input: GroupInput): GroupUpsertRequestWritable {
  return {
    name: input.name.trim(),
    strategy: normalizeGroupStrategy(input.strategy),
    nodeIds: input.nodeIds,
    groupIds: input.groupIds,
    remark: input.remark.trim(),
  };
}

function subscriptionToRequest(input: SubscriptionInput): SubscriptionUpsertRequestWritable {
  return {
    name: input.name.trim() || undefined,
    url: input.url.trim(),
    groupId: input.groupId.trim() || undefined,
    remark: input.remark.trim(),
  };
}

function mergeMappingPatch(mapping: PortMapping, patch: Partial<MappingInput>): MappingInput {
  return {
    listenAddress: patch.listenAddress ?? mapping.listenAddress,
    listenPort: patch.listenPort ?? mapping.listenPort,
    outboundProtocol: patch.outboundProtocol ?? mapping.outboundProtocol,
    username: patch.username ?? mapping.username,
    password: patch.password ?? mapping.password,
    strategy: patch.strategy ?? mapping.strategy,
    nodeIds: patch.nodeIds ?? mapping.nodeIds,
    activeNodeId: patch.activeNodeId === undefined ? mapping.activeNodeId : patch.activeNodeId,
    groupIds: patch.groupIds ?? mapping.groupIds,
    activeGroupId: patch.activeGroupId === undefined ? mapping.activeGroupId : patch.activeGroupId,
    enabled: patch.enabled ?? mapping.enabled,
    remark: patch.remark ?? mapping.remark,
  };
}

function mergeNodePatch(node: ProxyNode, patch: Partial<NodeInput>): NodeInput {
  return {
    name: patch.name ?? node.name,
    protocol: patch.protocol ?? node.protocol,
    server: patch.server ?? node.server,
    port: patch.port === undefined ? node.port : patch.port,
    username: patch.username ?? node.username,
    password: patch.password ?? node.password,
    rawUri: patch.rawUri ?? node.rawUri,
    tags: patch.tags ?? node.tags,
    chainNodeIds: patch.chainNodeIds ?? node.chainNodeIds,
    groupId: patch.groupId ?? node.groupId,
    groupIds: patch.groupIds ?? node.groupIds,
    remark: patch.remark ?? node.remark,
  };
}

function removeNodeFromLocalState(id: string): void {
  nodes.value = nodes.value.filter(node => node.id !== id);
  nodeTotal.value = Math.max(0, nodeTotal.value - 1);
  const nextOptions = { ...nodeOptionsCache.value };
  delete nextOptions[id];
  nodeOptionsCache.value = nextOptions;
  const nextHealth = { ...nodeHealthById.value };
  delete nextHealth[id];
  nodeHealthById.value = nextHealth;
  groups.value = groups.value.map(group => ({
    ...group,
    nodeIds: group.nodeIds.filter(nodeId => nodeId !== id),
    nodeCount: Math.max(0, group.nodeCount - (group.nodeIds.includes(id) ? 1 : 0)),
    updatedAt: new Date().toISOString(),
  }));
  mappings.value = mappings.value.map(mapping => {
    const nodeIds = mapping.nodeIds.filter(nodeId => nodeId !== id);
    const activeNodeId = mapping.activeNodeId === id ? nodeIds[0] || null : mapping.activeNodeId;

    return {
      ...mapping,
      nodeIds,
      activeNodeId,
      updatedAt: new Date().toISOString(),
    };
  });
}

function removeGroupFromLocalState(id: string): void {
  groups.value = groups.value
    .filter(group => group.id !== id)
    .map(group => ({
      ...group,
      groupIds: group.groupIds.filter(groupId => groupId !== id),
      updatedAt: new Date().toISOString(),
    }));
  nodes.value = nodes.value.map(node =>
    node.groupId === id || node.groupIds.includes(id)
      ? {
          ...node,
          groupId: node.groupId === id ? '' : node.groupId,
          groupIds: node.groupIds.filter(groupId => groupId !== id),
          updatedAt: new Date().toISOString(),
        }
      : node
  );
  mappings.value = mappings.value.map(mapping => {
    const groupIds = mapping.groupIds.filter(groupId => groupId !== id);
    const activeGroupId =
      mapping.activeGroupId === id ? groupIds[0] || null : mapping.activeGroupId;

    return {
      ...mapping,
      groupIds,
      activeGroupId,
      updatedAt: new Date().toISOString(),
    };
  });
}

export async function refreshProxyHubState(): Promise<void> {
  isLoading.value = true;
  clearBackendError();

  try {
    const { data } = await getProxyState({
      query: { includeNodes: false, includeGroupMembers: false },
      throwOnError: true,
    });
    applySnapshot(data);
    await refreshNodeHealth();
  } catch (error) {
    setBackendError(error);
    throw error;
  } finally {
    isLoading.value = false;
  }
}

async function refreshRuntimeStatus(): Promise<void> {
  try {
    const { data } = await getProxyRuntimeStatus({ throwOnError: true });
    runtime.value = data;
  } catch {
    // Runtime status is secondary; the main mutation result is already applied.
  }
}

async function loadNodes(input: NodeQueryInput = {}, append = false): Promise<void> {
  isLoadingNodes.value = true;
  clearBackendError();
  const page = input.page ?? 1;
  const size = input.size ?? nodesPageSize.value;

  try {
    const { data } = await getProxyNodes({
      query: {
        page,
        size,
        keyword: input.keyword?.trim() || undefined,
        groupId: input.groupId?.trim() || undefined,
        defaultOnly: input.defaultOnly || undefined,
        physicalOnly: input.physicalOnly || undefined,
        withHealth: input.withHealth ?? true,
      },
      throwOnError: true,
    });
    const items = (data.items ?? []).map(toProxyNode);
    nodes.value = append ? [...nodes.value, ...items] : items;
    currentNodeTotal.value = data.total;
    if (!input.keyword?.trim() && !input.groupId?.trim() && !input.defaultOnly && !input.physicalOnly) {
      nodeTotal.value = data.total;
    }
    nodesPage.value = data.page;
    nodesPageSize.value = data.size;
    cacheNodeOptions(items.map(nodeToOption));
    cacheNodeHealth(items.map(node => node.health));
  } catch (error) {
    setBackendError(error);
    throw error;
  } finally {
    isLoadingNodes.value = false;
  }
}

async function refreshNodeHealth(): Promise<void> {
  try {
    const { data } = await getProxyNodesHealth({ throwOnError: true });
    cacheNodeHealth((data.items ?? []).map(toProxyNodeHealth));
  } catch {
    // Health is secondary; node and runtime state stay usable without it.
  }
}

async function loadMoreNodes(input: NodeQueryInput = {}): Promise<void> {
  if (nodes.value.length >= currentNodeTotal.value || isLoadingNodes.value) return;
  await loadNodes({ ...input, page: nodesPage.value + 1, size: nodesPageSize.value }, true);
}

async function fetchNodeOptions(input: NodeOptionQueryInput = {}): Promise<{
  items: ProxyNodeOption[];
  total: number;
  page: number;
  size: number;
}> {
  const { data } = await getProxyNodeOptions({
    query: {
      page: input.page ?? 1,
      size: input.size ?? 50,
      keyword: input.keyword?.trim() || undefined,
      nameOnly: input.nameOnly || undefined,
      groupId: input.groupId?.trim() || undefined,
      defaultOnly: input.defaultOnly || undefined,
      physicalOnly: input.physicalOnly || undefined,
      ids: input.ids && input.ids.length > 0 ? input.ids : undefined,
    },
    throwOnError: true,
  });
  const items = (data.items ?? []).map(toProxyNodeOption);
  cacheNodeOptions(items);
  return {
    items,
    total: data.total,
    page: data.page,
    size: data.size,
  };
}

async function ensureNodeOptions(ids: string[]): Promise<void> {
  const missing = ids.filter(id => id && !nodeOptionsCache.value[id]);
  if (missing.length === 0) return;
  await fetchNodeOptions({ ids: Array.from(new Set(missing)), size: Math.min(200, missing.length) });
}

function ensureInitialLoad(): void {
  if (didInitialLoad || initialLoadPromise) return;

  didInitialLoad = true;
  initialLoadPromise = refreshProxyHubState()
    .catch(() => {
      didInitialLoad = false;
    })
    .finally(() => {
      initialLoadPromise = null;
    });
}

async function addNode(input: NodeInput): Promise<ProxyNode> {
  return runMutation(async () => {
    const { data } = await postProxyNodes({ body: nodeToRequest(input), throwOnError: true });
    const node = toProxyNode(data.item);
    upsertNode(node);
    markSaved(node.updatedAt);
    await refreshRuntimeStatus();
    return node;
  });
}

async function addNodeFromUri(rawUri: string, nameOverride = '', groupId = ''): Promise<ProxyNode> {
  const input = parseProxyUri(rawUri);
  const name = nameOverride.trim();

  return addNode({
    ...input,
    name: name || input.name,
    groupId,
    groupIds: groupId ? [groupId] : [],
  });
}

async function previewImportNodes(raw: string, groupId = ''): Promise<ImportPreviewResult> {
  return runMutation(async () => {
    const { data } = await postProxyNodesImportPreview({
      body: { raw, groupId: groupId.trim() || undefined },
      throwOnError: true,
    });
    return toImportPreviewResult(data);
  });
}

async function importNodes(raw: string, groupId = ''): Promise<ImportNodesResult> {
  return runMutation(async () => {
    const { data } = await postProxyNodesImport({
      body: { raw, groupId: groupId.trim() || undefined },
      query: { includeItems: false },
      throwOnError: true,
    });
    const imported = (data.items ?? []).map(toProxyNode);
    const importedGroups = (data.groups ?? []).map(toProxyGroup);
    if (imported.length > 0) {
      for (const node of imported) upsertNode(node);
      markSaved(imported[0].updatedAt);
      await refreshRuntimeStatus();
    }
    if (importedGroups.length > 0) {
      for (const group of importedGroups) upsertGroup(group);
      markSaved(importedGroups[0].updatedAt);
      await refreshRuntimeStatus();
    }
    if (data.failures?.length) {
      errorMessage.value = data.failures.map(failure => failure.message).join('\n');
    }
    await refreshProxyHubState();
    return {
      nodes: imported,
      groups: importedGroups,
      preview: toImportPreviewResult(data),
    };
  });
}

async function updateNode(id: string, patch: Partial<NodeInput>): Promise<ProxyNode> {
  return runMutation(async () => {
    const current = nodes.value.find(node => node.id === id);
    if (!current) {
      throw new Error(t('home.messages.nodeNotFound'));
    }

    const { data } = await putProxyNodesById({
      path: { id },
      body: nodeToRequest(mergeNodePatch(current, patch)),
      throwOnError: true,
    });
    const node = toProxyNode(data.item);
    upsertNode(node);
    markSaved(node.updatedAt);
    await refreshRuntimeStatus();
    return node;
  });
}

async function removeNode(id: string): Promise<void> {
  await runMutation(async () => {
    await deleteProxyNodesById({ path: { id }, throwOnError: true });
    removeNodeFromLocalState(id);
    markSaved();
    await refreshRuntimeStatus();
  });
}

async function testNode(id: string, probeUrl = ''): Promise<ProxyTestResult> {
  return runMutation(async () => {
    const { data } = await postProxyNodesByIdTest({
      path: { id },
      body: { probeUrl: probeUrl.trim() || undefined },
      throwOnError: true,
    });
    const result = toProxyTestResult(data);
    if (result.health) {
      cacheNodeHealth([result.health]);
      nodes.value = nodes.value.map(node =>
        node.id === id ? { ...node, health: result.health } : node
      );
    }
    return result;
  });
}

async function addGroup(input: GroupInput): Promise<ProxyGroup> {
  return runMutation(async () => {
    const { data } = await postProxyGroups({ body: groupToRequest(input), throwOnError: true });
    const group = toProxyGroup(data.item);
    upsertGroup(group);
    markSaved(group.updatedAt);
    await refreshRuntimeStatus();
    return group;
  });
}

async function updateGroup(id: string, input: GroupInput): Promise<ProxyGroup> {
  return runMutation(async () => {
    const { data } = await putProxyGroupsById({
      path: { id },
      body: groupToRequest(input),
      throwOnError: true,
    });
    const group = toProxyGroup(data.item);
    upsertGroup(group);
    markSaved(group.updatedAt);
    await refreshRuntimeStatus();
    return group;
  });
}

async function removeGroup(id: string): Promise<void> {
  await runMutation(async () => {
    await deleteProxyGroupsById({ path: { id }, throwOnError: true });
    removeGroupFromLocalState(id);
    markSaved();
    await refreshRuntimeStatus();
  });
}

async function addSubscription(input: SubscriptionInput): Promise<ProxySubscription> {
  return runMutation(async () => {
    const { data } = await postProxySubscriptions({
      body: subscriptionToRequest(input),
      throwOnError: true,
    });
    const subscription = toProxySubscription(data.item);
    upsertSubscription(subscription);
    markSaved(subscription.updatedAt);
    return subscription;
  });
}

async function previewSubscription(input: SubscriptionInput): Promise<ImportPreviewResult> {
  return runMutation(async () => {
    const { data } = await postProxySubscriptionsPreview({
      body: subscriptionToRequest(input),
      throwOnError: true,
    });
    return toImportPreviewResult(data);
  });
}

async function updateSubscription(
  id: string,
  input: SubscriptionInput
): Promise<ProxySubscription> {
  return runMutation(async () => {
    const { data } = await putProxySubscriptionsById({
      path: { id },
      body: subscriptionToRequest(input),
      throwOnError: true,
    });
    const subscription = toProxySubscription(data.item);
    upsertSubscription(subscription);
    markSaved(subscription.updatedAt);
    return subscription;
  });
}

async function syncSubscription(id: string): Promise<void> {
  await runMutation(async () => {
    const { data } = await postProxySubscriptionsByIdSync({
      path: { id },
      body: {},
      query: { includeItems: false },
      throwOnError: true,
    });
    if (data.failures?.length) {
      errorMessage.value = data.failures.map(failure => failure.message).join('\n');
    }
    await refreshProxyHubState();
    await refreshRuntimeStatus();
  });
}

async function removeSubscription(id: string): Promise<void> {
  await runMutation(async () => {
    await deleteProxySubscriptionsById({ path: { id }, throwOnError: true });
    subscriptions.value = subscriptions.value.filter(subscription => subscription.id !== id);
    await refreshProxyHubState();
    await refreshRuntimeStatus();
  });
}

async function addMapping(input: MappingInput): Promise<PortMapping> {
  return runMutation(async () => {
    const { data } = await postProxyMappings({ body: mappingToRequest(input), throwOnError: true });
    const mapping = toPortMapping(data.item);
    upsertMapping(mapping);
    markSaved(mapping.updatedAt);
    await refreshRuntimeStatus();
    return mapping;
  });
}

async function updateMapping(id: string, patch: Partial<MappingInput>): Promise<PortMapping> {
  return runMutation(async () => {
    const current = mappings.value.find(mapping => mapping.id === id);
    if (!current) {
      throw new Error(t('home.messages.mappingNotFound'));
    }

    const { data } = await putProxyMappingsById({
      path: { id },
      body: mappingToRequest(mergeMappingPatch(current, patch)),
      throwOnError: true,
    });
    const mapping = toPortMapping(data.item);
    upsertMapping(mapping);
    markSaved(mapping.updatedAt);
    await refreshRuntimeStatus();
    return mapping;
  });
}

async function removeMapping(id: string): Promise<void> {
  await runMutation(async () => {
    await deleteProxyMappingsById({ path: { id }, throwOnError: true });
    mappings.value = mappings.value.filter(mapping => mapping.id !== id);
    markSaved();
    await refreshRuntimeStatus();
  });
}

async function testMapping(id: string, probeUrl = ''): Promise<ProxyTestResult> {
  return runMutation(async () => {
    const { data } = await postProxyMappingsByIdTest({
      path: { id },
      body: { probeUrl: probeUrl.trim() || undefined },
      throwOnError: true,
    });
    return toProxyTestResult(data);
  });
}

async function resetDemoData(): Promise<void> {
  await refreshProxyHubState();
}

function snapshot(): ProxyHubStateSnapshot {
  return {
    nodes: nodes.value,
    groups: groups.value,
    subscriptions: subscriptions.value,
    mappings: mappings.value,
    lastSavedAt: lastSavedAt.value,
  };
}

export function useProxyHubState() {
  ensureInitialLoad();

  const enabledMappings = computed(() => mappings.value.filter(mapping => mapping.enabled));
  const nodeById = computed(() => new Map(nodes.value.map(node => [node.id, node])));
  const nodeOptionById = computed(() => new Map(Object.entries(nodeOptionsCache.value)));
  const groupById = computed(() => new Map(groups.value.map(group => [group.id, group])));
  const subscriptionById = computed(
    () => new Map(subscriptions.value.map(subscription => [subscription.id, subscription]))
  );

  return {
    nodes,
    nodeTotal,
    currentNodeTotal,
    defaultNodeTotal,
    nodesPage,
    nodesPageSize,
    isLoadingNodes,
    nodeOptionsCache,
    nodeHealthById,
    groups,
    subscriptions,
    mappings,
    lastSavedAt,
    runtime,
    isLoading,
    isSaving,
    errorMessage,
    loginRequired,
    enabledMappings,
    nodeById,
    nodeOptionById,
    groupById,
    subscriptionById,
    refreshState: refreshProxyHubState,
    refreshNodeHealth,
    loadNodes,
    loadMoreNodes,
    fetchNodeOptions,
    ensureNodeOptions,
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
    updateSubscription,
    syncSubscription,
    removeSubscription,
    addMapping,
    updateMapping,
    removeMapping,
    testMapping,
    resetDemoData,
    snapshot,
  };
}
