export type ProxyProtocol = 'vless' | 'vmess' | 'trojan' | 'socks5' | 'http' | 'unknown';

export type OutboundProtocol = 'mixed' | 'socks5' | 'http';

export type RouteStrategy = 'failover' | 'load-balance' | 'manual';

export type ProxyGroupType = 'manual' | 'subscription';

export type ProxyGroupStrategy = 'selector' | 'url-test';

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
  subscriptionId: string;
  groupId: string;
  sourceKey: string;
  createdAt: string;
  updatedAt: string;
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
