import type { ComputedRef, Ref } from 'vue';
import type {
  ImportPreviewItem,
  ImportPreviewResult,
  MappingSwitchTargetType,
  OutboundProtocol,
  PortMapping,
  ProxyGroup,
  ProxyGroupStrategy,
  ProxyNode,
  ProxyNodeOption,
  ProxyProtocol,
  ProxySubscription,
  RouteStrategy,
} from '@/types/proxyHub';

export type TabKey = 'mappings' | 'nodes' | 'groups' | 'subscriptions';
export type NodeGroupFilterKey = 'all' | 'default' | `group:${string}`;
export type PortRuntimeState = 'running' | 'failed' | 'closed' | 'notRunning';
export type RouteNodeMode = 'uri' | 'node' | 'group';

type Readable<T> = Ref<T> | ComputedRef<T>;

interface VirtualNodeRow {
  data: ProxyNode;
  index: number;
}

interface GroupFilterOption {
  id: string;
  label: string;
}

interface SubscriptionForm {
  name: string;
  url: string;
  groupId: string;
  remark: string;
}

interface ManualGroupForm {
  name: string;
  strategy: ProxyGroupStrategy;
  nodeIds: string[];
  groupIds: string[];
  remark: string;
}

export interface NodeGroupFilterOption {
  key: NodeGroupFilterKey;
  label: string;
  countLabel: string;
}

export interface NodeGroupSummaryItem {
  key: NodeGroupFilterKey;
  groupId?: string;
  title: string;
  typeLabel: string;
  count: number;
  detail: string;
  strategyLabel: string;
  filter: string;
  isSubscription: boolean;
  editable: boolean;
  allUnavailable: boolean;
}

export interface HomeViewContext {
  mappings: Readable<PortMapping[]>;
  portRuntimeState: (mapping: PortMapping) => PortRuntimeState;
  portEnabledLabel: (mapping: PortMapping) => string;
  toggleMappingEnabled: (mapping: PortMapping) => Promise<void>;
  mappingEndpoint: (mapping: PortMapping) => string;
  outboundProtocolLabels: Readable<Record<OutboundProtocol, string>>;
  strategyLabels: Readable<Record<RouteStrategy, string>>;
  openEditMappingDialog: (mapping: PortMapping) => void;
  copyPopoverText: (mappingId: string) => string;
  copyEndpoint: (mapping: PortMapping) => Promise<void>;
  openRouteDialog: (mapping: PortMapping) => void;
  openMappingTestDialog: (mapping: PortMapping) => void;
  requestRemoveMapping: (mapping: PortMapping) => void;
  portFailureReason: (mapping: PortMapping) => string;
  portStatusTitle: (mapping: PortMapping) => string;
  portStatusLabel: (mapping: PortMapping) => string;
  mappingNodes: (mapping: PortMapping) => ProxyNode[];
  isActiveRoute: (
    mapping: PortMapping,
    targetType: MappingSwitchTargetType,
    targetId: string
  ) => boolean;
  switchMappingRoute: (
    mapping: PortMapping,
    targetType: MappingSwitchTargetType,
    targetId: string
  ) => Promise<void>;
  openNodeTestDialog: (node: ProxyNode) => void;
  requestRemoveRoute: (mapping: PortMapping, target: ProxyNode | ProxyGroup) => void;
  protocolLabels: Readable<Record<ProxyProtocol, string>>;
  nodeHealthTitle: (node: ProxyNode) => string;
  isProbeUnavailableNode: (node: ProxyNode) => boolean;
  routeLatencyLabel: (node: ProxyNode) => string;
  routeSuccessLabel: (node: ProxyNode) => string;
  routeFailureLabel: (node: ProxyNode) => string;
  mappingGroups: (mapping: PortMapping) => ProxyGroup[];
  groupRouteTotalLabel: (mapping: PortMapping, group: ProxyGroup) => string;
  groupRouteAvailableLabel: (mapping: PortMapping, group: ProxyGroup) => string;
  groupRouteLatencyLabel: (mapping: PortMapping, group: ProxyGroup) => string;
  groupRouteHealthTitle: (mapping: PortMapping, group: ProxyGroup) => string;
  openNewMappingDialog: () => void;

  nodeSearch: Ref<string>;
  hideEmptyNodeGroups: Ref<boolean>;
  nodeGroupFilterOptions: Readable<NodeGroupFilterOption[]>;
  activeNodeGroupFilter: Ref<NodeGroupFilterKey>;
  selectNodeGroupFilter: (key: NodeGroupFilterKey) => Promise<void>;
  groupSummaryItems: Readable<NodeGroupSummaryItem[]>;
  selectedGroup: Readable<ProxyGroup | null>;
  selectedNodeGroupTitle: Readable<string>;
  selectedNodeGroupHealthSummary: Readable<{
    available: number;
    autoProbeEnabled: boolean;
    autoProbeRunning: boolean;
    fastestLatencyMs: number;
    needsProbe: number;
    probing: number;
    unavailable: number;
  }>;
  currentNodeTotal: Readable<number>;
  selectedNodeGroupNodes: Readable<ProxyNode[]>;
  nodeListContainerProps: object;
  nodeListWrapperProps: object;
  virtualNodeRows: Readable<VirtualNodeRow[]>;
  nodeEndpointLabel: (node: ProxyNode) => string;
  nodeUriPopoverText: (node: ProxyNode) => string;
  nodeExportUri: (node: ProxyNode) => string;
  copyNodeUri: (node: ProxyNode) => Promise<void>;
  openEditNodeDialog: (node: ProxyNode) => void;
  requestRemoveNode: (node: ProxyNode) => void;
  nodeBlacklistLabel: (node: ProxyNode) => string;
  isLoadingNodes: Readable<boolean>;
  loadNextNodePage: () => Promise<void>;
  groups: Readable<ProxyGroup[]>;
  groupFilterOptions: (includeAll?: boolean) => GroupFilterOption[];
  optionProtocolLabel: (option: ProxyNodeOption) => string;
  optionNameLabel: (option: ProxyNodeOption) => string;
  optionEndpointLabel: (option: ProxyNodeOption) => string;
  importMessage: Ref<string>;
  manualGroupForm: ManualGroupForm;
  manualGroupNodeSearch: Ref<string>;
  manualGroupNodeGroupId: Ref<string>;
  manualGroupNodeOptions: Readable<ProxyNodeOption[]>;
  toggleManualGroupNode: (nodeId: string) => void;
  manualGroupNodeTotal: Readable<number>;
  isLoadingManualGroupNodes: Readable<boolean>;
  loadMoreManualGroupNodeOptions: () => void;
  selectedManualGroupNodes: () => ProxyNode[];
  manualGroups: Readable<ProxyGroup[]>;
  openEditGroupById: (groupId: string) => void;
  openEditGroupDialog: (group: ProxyGroup) => void;
  handleManualGroupSubmit: () => Promise<void>;
  groupSummary: (group: ProxyGroup) => string;
  removeGroup: (id: string) => Promise<void>;

  subscriptionForm: SubscriptionForm;
  handleSubscriptionSubmit: () => Promise<void>;
  subscriptionPreview: Ref<ImportPreviewResult | null>;
  previewSummary: (preview: ImportPreviewResult) => string;
  previewTypeLabel: (item: Pick<ImportPreviewItem, 'type'>) => string;
  previewActionLabel: (item: Pick<ImportPreviewItem, 'action'>) => string;
  subscriptions: Readable<ProxySubscription[]>;
  syncExistingSubscription: (id: string) => Promise<void>;
  removeSubscription: (id: string) => Promise<void>;
  subscriptionGroupName: (groupId: string) => string;
  formatDateTime: (value: Date | number | string, options?: Intl.DateTimeFormatOptions) => string;

  rawImport: Ref<string>;
  rawImportGroupId: Ref<string>;
  importPreview: Ref<ImportPreviewResult | null>;
  handleImport: () => Promise<void>;
}
