export type ProxyProtocol = 'vless' | 'vmess' | 'trojan' | 'socks5' | 'http' | 'unknown';

export type OutboundProtocol = 'mixed' | 'socks5' | 'http';

export type RouteStrategy = 'failover' | 'load-balance' | 'manual';

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
  remark: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProxyHubStateSnapshot {
  nodes: ProxyNode[];
  mappings: PortMapping[];
  lastSavedAt: string | null;
}
