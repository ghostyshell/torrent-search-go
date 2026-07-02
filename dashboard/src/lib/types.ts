// Response shapes for /api/monitoring/* (kept loose where the backend emits
// free-form maps, e.g. database stats - mirrors how the old dashboard read them).

export interface ApiResult<T> {
  success: boolean;
  error?: string;
  data?: T;
}

export interface SystemStats {
  uptime: number;
  memory: { heapUsed: number | string };
  nodeVersion?: string;
  goVersion?: string;
}

export interface ApiStats {
  totalRequests: number;
  requestsPerMinute: number;
  byMethod: Record<string, number>;
  byStatus: Record<string, number>;
}

export type DatabaseStats = Record<string, unknown>;

export interface BackgroundTask {
  name: string;
  status: 'idle' | 'completed' | 'disabled' | 'error' | string;
  lastRun?: string | number | null;
  nextRun?: string | number | null;
}

export interface DashboardData {
  success: boolean;
  error?: string;
  system: SystemStats;
  api: ApiStats;
  database?: DatabaseStats;
  backgroundTasks: BackgroundTask[];
}

export interface EndpointStat {
  endpoint: string;
  count: number;
}

export interface RecentRequest {
  method: string;
  path: string;
  status: number;
  duration: number;
}

export interface ApiUsageData {
  success: boolean;
  error?: string;
  stats?: { topEndpoints: EndpointStat[] };
  recentRequests?: RecentRequest[];
}

export interface LogEntry {
  level?: string;
  timestamp?: string | number;
  message?: string;
  raw?: string;
  method?: string;
  url?: string;
  path?: string;
}

export interface LogsData {
  success: boolean;
  error?: string;
  logs?: LogEntry[];
}

export interface JobLog {
  success: boolean;
  manual: boolean;
  timestamp: string | number;
  error?: string;
  // stream-url refresh fields
  totalFavorites?: number;
  usersProcessed?: number;
  refreshed?: number;
  skipped?: number;
  failed?: number;
  // description/image cache fields
  totalSearches?: number;
  totalTorrents?: number;
  imagesFound?: number;
  cached?: number;
  replaced?: number;
}

export interface JobStatusData {
  success: boolean;
  error?: string;
  status: 'idle' | 'running' | 'completed' | 'error' | string;
  lastRun?: string | number | null;
  nextRun?: string | number | null;
  recentAppLogs?: LogEntry[];
  logs?: JobLog[];
}

export interface JobLogFile {
  job: string;
  date: string;
  name: string;
  mtime: string;
  compressed?: boolean;
}

export interface JobLogListData {
  success: boolean;
  error?: string;
  logVersion: string | number;
  count: number;
  root: string;
  files?: JobLogFile[];
}

export interface JobLogMatch {
  file: string;
  line: string;
}

export interface JobLogSearchData {
  success: boolean;
  error?: string;
  matches: JobLogMatch[];
  nextOffset: number;
  hasMore: boolean;
}

export interface IPTrafficRow {
  ip: string;
  requestCount?: number;
  count1h?: number;
  count6h?: number;
  count1d?: number;
  count1w?: number;
  count1mo?: number;
  isBlocked?: boolean;
}

export interface BlockedIPRow {
  ip: string;
  reason: string;
  notes?: string;
  requestCount?: number;
  blockedAt?: string | number | null;
}

export interface TriggerAck {
  success: boolean;
  error?: string;
  message?: string;
}

// Addon status report (public /api/addon-status; managed via /api/monitoring/addon-status).
export interface AddonMeta {
  id: string;
  name: string;
  status: 'LIVE' | 'DOWN' | 'MAINTENANCE' | string;
  updatedAt: string;
}

export interface AddonSource {
  id: string;
  name: string;
  note?: string;
  status: 'LIVE' | 'DOWN' | 'MAINTENANCE' | string;
  detail: string;
}

export interface AddonIssue {
  id: string;
  title: string;
  status: string;
  summary: string;
  updatedAt: string;
}

export interface AddonChangelog {
  version: string;
  date: string;
  highlights: string[];
}

export interface AddonFeature {
  title: string;
  body: string;
}

export interface AddonStatusReport {
  addon: AddonMeta;
  sources: AddonSource[];
  issues: AddonIssue[];
  changelog: AddonChangelog[];
  features: AddonFeature[];
  changelogSourceUrl?: string;
}

export interface AddonStatusListResult {
  success: boolean;
  error?: string;
  reports: AddonStatusReport[];
}

export interface AddonStatusOneResult {
  success: boolean;
  error?: string;
  report?: AddonStatusReport;
}

export interface AddonStatusWriteResult {
  success: boolean;
  error?: string;
  id?: string;
}

// Redis cache viewer/buster (/api/monitoring/redis-caches*).

export interface RedisCacheGroup {
  prefix: string;
  label: string;
  description: string;
  ttlSeconds: number;
  keyCount: number;
}

export interface RedisCachesResult {
  success: boolean;
  error?: string;
  redisEnabled: boolean;
  groups: RedisCacheGroup[];
}

export interface BustResult {
  success: boolean;
  error?: string;
  deleted?: number;
  prefix?: string;
  groups?: number;
}