// Verbatim ports of the format helpers in the old static/dashboard.html.

export function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

export function formatTime(timestamp: string | number): string {
  return new Date(timestamp).toLocaleTimeString();
}

export function formatTimeUntil(timestamp: string | number): string {
  const now = Date.now();
  const target = new Date(timestamp).getTime();
  const diff = target - now;

  if (diff <= 0) return 'now';

  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) return `in ${days}d ${hours % 24}h`;
  if (hours > 0) return `in ${hours}h ${minutes % 60}m`;
  if (minutes > 0) return `in ${minutes}m ${seconds % 60}s`;
  return `in ${seconds}s`;
}

export function formatLabel(key: string): string {
  return key.replace(/([A-Z])/g, ' $1').replace(/^./, (s) => s.toUpperCase());
}

const TASK_NAMES: Record<string, string> = {
  storageCleanup: 'Storage Cleanup',
  streamUrlRefresh: 'Stream URL Refresh',
  descriptionImageCache: 'Description & Image Cache',
  searchResultsCache: 'Filter stream URL cache',
  jobLogMaintenance: 'Job log maintenance',
};

export function formatTaskName(name: string): string {
  return TASK_NAMES[name] || name;
}

// "in Xh Ym" / "in Ym" / "soon" - ported from the stream-refresh / desc-cache
// next-run formatting in the old dashboard.
export function formatNextRun(nextRun: string | number | null | undefined): string {
  if (!nextRun) return '';
  const diffMs = new Date(nextRun).getTime() - Date.now();
  if (diffMs <= 0) return 'soon';
  const hours = Math.floor(diffMs / (1000 * 60 * 60));
  const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
  return hours > 0 ? `in ${hours}h ${minutes}m` : `in ${minutes}m`;
}

export function statusClass(status: string): string {
  switch (status) {
    case 'idle':
    case 'completed':
    case 'disabled':
    case 'error':
      return `status-${status}`;
    case 'running':
      return 'status-running';
    default:
      return 'status-idle';
  }
}

export function methodClass(method: string): string {
  return `method-${method.toLowerCase()}`;
}

export function statusBandClass(status: number): string {
  return `status-${Math.floor(status / 100)}xx`;
}