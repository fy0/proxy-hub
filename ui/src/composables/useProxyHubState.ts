import { computed, ref, watch } from 'vue';
import type {
  OutboundProtocol,
  PortMapping,
  ProxyHubStateSnapshot,
  ProxyNode,
  ProxyProtocol,
  RouteStrategy,
} from '@/types/proxyHub';
import { t } from '@/i18n';

const STORAGE_KEY = 'proxy-hub.console.v1';

interface NodeInput {
  name: string;
  protocol: ProxyProtocol;
  server: string;
  port: number | null;
  username: string;
  password: string;
  rawUri: string;
  tags: string[];
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
  enabled: boolean;
  remark: string;
}

interface StoredState {
  nodes: ProxyNode[];
  mappings: PortMapping[];
  lastSavedAt: string | null;
}

function nowIso(): string {
  return new Date().toISOString();
}

function createId(prefix: string): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return `${prefix}_${crypto.randomUUID()}`;
  }

  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
}

function normalizeProtocol(value: string | null | undefined): ProxyProtocol {
  const protocol = value?.toLowerCase().replace(':', '') ?? 'unknown';

  if (
    protocol === 'vless' ||
    protocol === 'vmess' ||
    protocol === 'trojan' ||
    protocol === 'socks5' ||
    protocol === 'http'
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

function toPort(value: string | number | null | undefined): number | null {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
    return null;
  }

  return parsed;
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

function parseVmessUri(rawUri: string): NodeInput | null {
  if (!rawUri.toLowerCase().startsWith('vmess://')) return null;

  try {
    const payload = rawUri.replace(/^vmess:\/\//i, '').trim();
    const parsed = JSON.parse(decodeBase64Payload(payload)) as Record<string, unknown>;
    const server = stringValue(parsed.add).trim();
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
      tags: ['vmess'],
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

    return {
      name,
      protocol,
      server: parsed.hostname,
      port: toPort(parsed.port),
      username: decodeURIComponent(parsed.username || '').trim(),
      password: decodeURIComponent(parsed.password || '').trim(),
      rawUri,
      tags: [protocol].filter(tag => tag !== 'unknown'),
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
      remark: t('state.node.unsupportedRemark'),
    };
  }
}

export function inferNodeNameFromUri(rawUri: string): string {
  const value = rawUri.trim();
  if (!/^[a-z0-9+.-]+:\/\//i.test(value)) return '';

  return parseProxyUri(value).name;
}

function seedState(): StoredState {
  const createdAt = nowIso();
  const node1Name = t('state.seed.node1Name');
  const node2Name = t('state.seed.node2Name');
  const node3Name = t('state.seed.node3Name');
  const nodeA: ProxyNode = {
    id: createId('node'),
    name: node1Name,
    protocol: 'vless',
    server: 'edge-a.example.net',
    port: 443,
    username: 'uuid',
    password: '',
    rawUri: `vless://uuid@edge-a.example.net:443?type=http&security=tls#${encodeURIComponent(node1Name)}`,
    tags: ['vless', 'h2'],
    remark: t('state.seed.node1Remark'),
    createdAt,
    updatedAt: createdAt,
  };
  const nodeB: ProxyNode = {
    id: createId('node'),
    name: node2Name,
    protocol: 'trojan',
    server: 'relay-b.example.net',
    port: 443,
    username: '',
    password: 'password',
    rawUri: `trojan://password@relay-b.example.net:443#${encodeURIComponent(node2Name)}`,
    tags: ['trojan', 'backup'],
    remark: t('state.seed.node2Remark'),
    createdAt,
    updatedAt: createdAt,
  };
  const nodeC: ProxyNode = {
    id: createId('node'),
    name: node3Name,
    protocol: 'socks5',
    server: '10.0.0.8',
    port: 1080,
    username: '',
    password: '',
    rawUri: `socks5://10.0.0.8:1080#${encodeURIComponent(node3Name)}`,
    tags: ['lan'],
    remark: t('state.seed.node3Remark'),
    createdAt,
    updatedAt: createdAt,
  };

  return {
    nodes: [nodeA, nodeB, nodeC],
    mappings: [
      {
        id: createId('map'),
        enabled: true,
        listenAddress: '0.0.0.0',
        listenPort: 1080,
        outboundProtocol: 'mixed',
        username: '',
        password: '',
        strategy: 'failover',
        nodeIds: [nodeA.id, nodeB.id],
        activeNodeId: nodeA.id,
        remark: t('state.seed.mapping1Remark'),
        createdAt,
        updatedAt: createdAt,
      },
      {
        id: createId('map'),
        enabled: true,
        listenAddress: '0.0.0.0',
        listenPort: 1082,
        outboundProtocol: 'mixed',
        username: '',
        password: '',
        strategy: 'manual',
        nodeIds: [nodeC.id],
        activeNodeId: nodeC.id,
        remark: t('state.seed.mapping2Remark'),
        createdAt,
        updatedAt: createdAt,
      },
    ],
    lastSavedAt: createdAt,
  };
}

function isProxyNode(value: unknown): value is ProxyNode {
  if (!value || typeof value !== 'object') return false;
  const node = value as Partial<ProxyNode>;
  return typeof node.id === 'string' && typeof node.name === 'string';
}

function isPortMapping(value: unknown): value is PortMapping {
  if (!value || typeof value !== 'object') return false;
  const mapping = value as Partial<PortMapping>;
  return typeof mapping.id === 'string' && Number.isInteger(mapping.listenPort);
}

function normalizeMapping(mapping: PortMapping): PortMapping {
  return {
    ...mapping,
    outboundProtocol: normalizeOutboundProtocol(mapping.outboundProtocol),
    username: typeof mapping.username === 'string' ? mapping.username : '',
    password: typeof mapping.password === 'string' ? mapping.password : '',
  };
}

function loadState(): { state: StoredState; shouldPersist: boolean } {
  if (typeof localStorage === 'undefined') {
    return { state: seedState(), shouldPersist: true };
  }

  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) {
    return { state: seedState(), shouldPersist: true };
  }

  try {
    const parsed = JSON.parse(raw) as Partial<StoredState>;
    const nodes = Array.isArray(parsed.nodes) ? parsed.nodes.filter(isProxyNode) : [];
    const mappings = Array.isArray(parsed.mappings)
      ? parsed.mappings.filter(isPortMapping).map(normalizeMapping)
      : [];

    return {
      state: {
        nodes,
        mappings,
        lastSavedAt: typeof parsed.lastSavedAt === 'string' ? parsed.lastSavedAt : null,
      },
      shouldPersist: false,
    };
  } catch {
    return {
      state: seedState(),
      shouldPersist: true,
    };
  }
}

const loaded = loadState();
const initialState = loaded.state;

const nodes = ref<ProxyNode[]>(initialState.nodes);
const mappings = ref<PortMapping[]>(initialState.mappings);
const lastSavedAt = ref<string | null>(initialState.lastSavedAt);

function persistState(): void {
  if (typeof localStorage === 'undefined') return;

  lastSavedAt.value = nowIso();
  const payload: StoredState = {
    nodes: nodes.value,
    mappings: mappings.value,
    lastSavedAt: lastSavedAt.value,
  };

  localStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
}

watch(
  [nodes, mappings],
  () => {
    persistState();
  },
  { deep: true }
);

if (loaded.shouldPersist) {
  persistState();
}

function cleanMappingNodes(
  nodeIds: string[],
  activeNodeId: string | null
): Pick<PortMapping, 'nodeIds' | 'activeNodeId'> {
  const available = new Set(nodes.value.map(node => node.id));
  const uniqueNodeIds = Array.from(new Set(nodeIds)).filter(id => available.has(id));
  const nextActive =
    activeNodeId && uniqueNodeIds.includes(activeNodeId) ? activeNodeId : uniqueNodeIds[0] || null;

  return {
    nodeIds: uniqueNodeIds,
    activeNodeId: nextActive,
  };
}

function addNode(input: NodeInput): ProxyNode {
  const timestamp = nowIso();
  const node: ProxyNode = {
    id: createId('node'),
    name: input.name.trim() || t('state.node.unnamed'),
    protocol: input.protocol,
    server: input.server.trim(),
    port: input.port,
    username: input.username.trim(),
    password: input.password.trim(),
    rawUri: input.rawUri.trim(),
    tags: input.tags.map(tag => tag.trim()).filter(Boolean),
    remark: input.remark.trim(),
    createdAt: timestamp,
    updatedAt: timestamp,
  };

  nodes.value = [node, ...nodes.value];
  return node;
}

function addNodeFromUri(rawUri: string, nameOverride = ''): ProxyNode {
  const input = parseProxyUri(rawUri);
  const name = nameOverride.trim();

  return addNode({
    ...input,
    name: name || input.name,
  });
}

function updateNode(id: string, patch: Partial<NodeInput>): void {
  const timestamp = nowIso();
  nodes.value = nodes.value.map(node =>
    node.id === id
      ? {
          ...node,
          ...patch,
          name: patch.name?.trim() || node.name,
          server: patch.server?.trim() ?? node.server,
          username: patch.username?.trim() ?? node.username,
          password: patch.password?.trim() ?? node.password,
          rawUri: patch.rawUri?.trim() ?? node.rawUri,
          remark: patch.remark?.trim() ?? node.remark,
          tags: patch.tags?.map(tag => tag.trim()).filter(Boolean) ?? node.tags,
          updatedAt: timestamp,
        }
      : node
  );
}

function removeNode(id: string): void {
  nodes.value = nodes.value.filter(node => node.id !== id);
  mappings.value = mappings.value.map(mapping => {
    const cleaned = cleanMappingNodes(
      mapping.nodeIds.filter(nodeId => nodeId !== id),
      mapping.activeNodeId === id ? null : mapping.activeNodeId
    );

    return {
      ...mapping,
      ...cleaned,
      updatedAt: nowIso(),
    };
  });
}

function addMapping(input: MappingInput): PortMapping {
  const timestamp = nowIso();
  const cleaned = cleanMappingNodes(input.nodeIds, input.activeNodeId);
  const mapping: PortMapping = {
    id: createId('map'),
    enabled: input.enabled,
    listenAddress: input.listenAddress.trim() || '0.0.0.0',
    listenPort: input.listenPort,
    outboundProtocol: normalizeOutboundProtocol(input.outboundProtocol),
    username: input.username.trim(),
    password: input.password.trim(),
    strategy: input.strategy,
    nodeIds: cleaned.nodeIds,
    activeNodeId: cleaned.activeNodeId,
    remark: input.remark.trim(),
    createdAt: timestamp,
    updatedAt: timestamp,
  };

  mappings.value = [mapping, ...mappings.value];
  return mapping;
}

function updateMapping(id: string, patch: Partial<MappingInput>): void {
  const timestamp = nowIso();

  mappings.value = mappings.value.map(mapping => {
    if (mapping.id !== id) return mapping;

    const nodeIds = patch.nodeIds ?? mapping.nodeIds;
    const activeNodeId =
      patch.activeNodeId === undefined ? mapping.activeNodeId : patch.activeNodeId;
    const cleaned = cleanMappingNodes(nodeIds, activeNodeId);

    return {
      ...mapping,
      ...patch,
      ...cleaned,
      listenAddress: patch.listenAddress?.trim() || mapping.listenAddress,
      outboundProtocol: normalizeOutboundProtocol(
        patch.outboundProtocol ?? mapping.outboundProtocol
      ),
      username: patch.username?.trim() ?? mapping.username,
      password: patch.password?.trim() ?? mapping.password,
      remark: patch.remark?.trim() ?? mapping.remark,
      updatedAt: timestamp,
    };
  });
}

function removeMapping(id: string): void {
  mappings.value = mappings.value.filter(mapping => mapping.id !== id);
}

function resetDemoData(): void {
  const next = seedState();
  nodes.value = next.nodes;
  mappings.value = next.mappings;
  lastSavedAt.value = next.lastSavedAt;
}

function snapshot(): ProxyHubStateSnapshot {
  return {
    nodes: nodes.value,
    mappings: mappings.value,
    lastSavedAt: lastSavedAt.value,
  };
}

export function useProxyHubState() {
  const enabledMappings = computed(() => mappings.value.filter(mapping => mapping.enabled));
  const nodeById = computed(() => new Map(nodes.value.map(node => [node.id, node])));

  return {
    nodes,
    mappings,
    lastSavedAt,
    enabledMappings,
    nodeById,
    addNode,
    addNodeFromUri,
    updateNode,
    removeNode,
    addMapping,
    updateMapping,
    removeMapping,
    resetDemoData,
    snapshot,
  };
}
