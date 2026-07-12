// Typed fetch helpers for /api/monitoring/*. The global fetch override in
// lib/auth.ts injects the dashboard password header, so these just call fetch.

import type {
  DashboardData,
  ApiUsageData,
  LogsData,
  JobStatusData,
  JobLogListData,
  JobLogSearchData,
  IPTrafficRow,
  BlockedIPRow,
  TriggerAck,
  AddonStatusReport,
  AddonStatusListResult,
  AddonStatusOneResult,
  AddonStatusWriteResult,
  RedisCachesResult,
  BustResult,
} from './types';

async function parse<T>(res: Response): Promise<T> {
  return (await res.json()) as T;
}

export async function fetchDashboard(): Promise<DashboardData> {
  return parse<DashboardData>(await fetch('/api/monitoring/dashboard'));
}

export async function fetchApiUsage(): Promise<ApiUsageData> {
  return parse<ApiUsageData>(await fetch('/api/monitoring/api-usage'));
}

export async function fetchLogs(level = 'all', limit = 100): Promise<LogsData> {
  return parse<LogsData>(await fetch(`/api/monitoring/logs?level=${level}&limit=${limit}`));
}

export async function fetchStreamRefreshLogs(): Promise<JobStatusData> {
  return parse<JobStatusData>(
    await fetch('/api/monitoring/stream-url-refresh-logs?limit=20&includeAppLogs=true'),
  );
}

export async function triggerStreamUrlRefresh(): Promise<TriggerAck> {
  return parse<TriggerAck>(await fetch('/api/monitoring/stream-url-refresh-trigger', { method: 'POST' }));
}

export async function fetchDescCacheLogs(): Promise<JobStatusData> {
  return parse<JobStatusData>(
    await fetch('/api/monitoring/description-image-cache-logs?limit=20&includeAppLogs=true'),
  );
}

export async function triggerDescriptionImageCache(): Promise<TriggerAck> {
  return parse<TriggerAck>(
    await fetch('/api/monitoring/description-image-cache-trigger', { method: 'POST' }),
  );
}

export async function triggerDescriptionImageCacheForceRefresh(): Promise<TriggerAck> {
  return parse<TriggerAck>(
    await fetch('/api/monitoring/description-image-cache-force-refresh', { method: 'POST' }),
  );
}

export async function fetchJobLogList(): Promise<JobLogListData> {
  return parse<JobLogListData>(await fetch('/api/monitoring/job-logs/list'));
}

export async function searchJobLogFiles(params: {
  q: string;
  job?: string;
  sort: 'asc' | 'desc';
  limit?: number;
  offset?: number;
}): Promise<JobLogSearchData> {
  const p = new URLSearchParams({
    q: params.q,
    limit: String(params.limit ?? 50),
    offset: String(params.offset ?? 0),
    sort: params.sort,
    includeCompressed: 'true',
  });
  if (params.job) p.set('job', params.job);
  return parse<JobLogSearchData>(await fetch('/api/monitoring/job-logs/search?' + p.toString()));
}

export async function triggerJobLogMaintenance(): Promise<TriggerAck> {
  return parse<TriggerAck>(await fetch('/api/monitoring/job-logs/maintenance', { method: 'POST' }));
}

export async function fetchIPTraffic(): Promise<{ traffic: IPTrafficRow[] }> {
  return parse(await fetch('/api/monitoring/ip-traffic'));
}

export async function fetchBlockedIPs(): Promise<{ blocked: BlockedIPRow[] }> {
  return parse(await fetch('/api/monitoring/ip-block'));
}

export async function blockIP(ip: string, notes?: string): Promise<TriggerAck> {
  return parse<TriggerAck>(
    await fetch('/api/monitoring/ip-block', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ip, notes }),
    }),
  );
}

export async function unblockIP(ip: string): Promise<TriggerAck> {
  return parse<TriggerAck>(
    await fetch(`/api/monitoring/ip-block/${encodeURIComponent(ip)}`, { method: 'DELETE' }),
  );
}

// Addon status reports. Public reads at /api/addon-status (no auth header needed);
// writes at /api/monitoring/addon-status (the auth fetch wrapper injects the password).

export async function fetchAddonStatusReports(): Promise<AddonStatusListResult> {
  return parse<AddonStatusListResult>(await fetch('/api/addon-status'));
}

export async function fetchAddonStatusReport(id: string): Promise<AddonStatusOneResult> {
  return parse<AddonStatusOneResult>(
    await fetch(`/api/addon-status/${encodeURIComponent(id)}`),
  );
}

export async function createAddonStatusReport(rep: AddonStatusReport): Promise<AddonStatusWriteResult> {
  return parse<AddonStatusWriteResult>(
    await fetch('/api/monitoring/addon-status', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(rep),
    }),
  );
}

export async function replaceAddonStatusReport(
  id: string,
  rep: AddonStatusReport,
): Promise<AddonStatusWriteResult> {
  return parse<AddonStatusWriteResult>(
    await fetch(`/api/monitoring/addon-status/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(rep),
    }),
  );
}

export async function deleteAddonStatusReport(id: string): Promise<AddonStatusWriteResult> {
  return parse<AddonStatusWriteResult>(
    await fetch(`/api/monitoring/addon-status/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  );
}
// Redis cache viewer/buster. Auth header is injected by the global fetch
// override in lib/auth.ts (these are /api/monitoring/* calls).

export async function fetchRedisCaches(): Promise<RedisCachesResult> {
  return parse<RedisCachesResult>(await fetch('/api/monitoring/redis-caches'));
}

export async function bustRedisCache(prefix: string): Promise<BustResult> {
  return parse<BustResult>(
    await fetch('/api/monitoring/redis-caches/bust', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ prefix }),
    }),
  );
}

export async function bustAllRedisCaches(): Promise<BustResult> {
  return parse<BustResult>(
    await fetch('/api/monitoring/redis-caches/bust-all', { method: 'POST' }),
  );
}
