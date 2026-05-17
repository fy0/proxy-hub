export type ProxyProtocol =
  | 'vless'
  | 'vmess'
  | 'trojan'
  | 'socks5'
  | 'http'
  | 'shadowsocks'
  | 'hysteria'
  | 'hysteria2'
  | 'tuic'
  | 'ssh'
  | 'chain'
  | 'unknown';

export type OutboundProtocol = 'mixed' | 'socks5' | 'http';

export type RouteStrategy = 'failover' | 'load-balance' | 'manual';

export type ProxyGroupType = 'manual' | 'subscription';

export type ProxyGroupStrategy = 'selector' | 'url-test';

export type ImportPreviewType = 'node' | 'group' | 'builtin' | 'failure';

export type ImportPreviewAction = 'import' | 'update' | 'skip' | 'fail';

export interface ImportPreviewItem {
  type: ImportPreviewType;
  name: string;
  action: ImportPreviewAction;
  reason: string;
  detail: string;
}

export interface ImportPreviewResult {
  items: ImportPreviewItem[];
  total: number;
  imported: number;
  failed: number;
  updated: number;
  deleted: number;
  skipped: number;
}

export interface ProxyNodeHealth {
  nodeId: string;
  available: boolean;
  failureCount: number;
  successCount: number;
  blacklisted: boolean;
  blacklistedUntil: string | null;
  lastLatencyMs: number;
  lastError: string;
  lastCheckedAt: string | null;
  lastSuccessAt: string | null;
  lastFailureAt: string | null;
  updatedAt: string;
}

export interface ProxyNode {
  id: string;
  name: string;
  protocol: ProxyProtocol;
  server: string;
  port: number | null;
  username: string;
  password: string;
  rawUri: string;
  tags: string[];
  remark: string;
  chainNodeIds: string[];
  subscriptionId: string;
  groupId: string;
  groupIds: string[];
  sourceKey: string;
  health: ProxyNodeHealth | null;
  createdAt: string;
  updatedAt: string;
}

export interface ProxyNodeOption {
  id: string;
  name: string;
  protocol: ProxyProtocol;
  server: string;
  port: number | null;
  groupIds: string[];
}

export interface ProxySubscription {
  id: string;
  name: string;
  url: string;
  groupId: string;
  remark: string;
  lastSyncedAt: string | null;
  lastSyncStatus: string;
  lastSyncError: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProxyGroup {
  id: string;
  name: string;
  type: ProxyGroupType;
  strategy: ProxyGroupStrategy;
  subscriptionId: string;
  sourceKey: string;
  nodeIds: string[];
  groupIds: string[];
  builtinTags: string[];
  includesAll: boolean;
  filter: string;
  remark: string;
  nodeCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface PortMapping {
  id: string;
  enabled: boolean;
  listenAddress: string;
  listenPort: number;
  order: number;
  outboundProtocol: OutboundProtocol;
  username: string;
  password: string;
  strategy: RouteStrategy;
  nodeIds: string[];
  activeNodeId: string | null;
  groupIds: string[];
  activeGroupId: string | null;
  remark: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProxyHubStateSnapshot {
  nodes: ProxyNode[];
  groups: ProxyGroup[];
  subscriptions: ProxySubscription[];
  mappings: PortMapping[];
  lastSavedAt: string | null;
}

export interface RuntimeExcludedNode {
  mappingId: string;
  nodeId: string;
  nodeName: string;
  tag: string;
  error: string;
}
