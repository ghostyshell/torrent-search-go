'use client';

import { useEffect } from 'react';
import { useCardData } from '@/hooks/use-card-data';
import { useRefresh } from '@/lib/refresh-context';
import { fetchDashboard } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  formatLabel,
  formatTaskName,
  formatTime,
  formatTimeUntil,
  formatUptime,
  statusClass,
} from '@/lib/format';
import type { DashboardData } from '@/lib/types';

const DB_DENY = new Set(['databaseType', 'environment', 'torrentDetails', 'cache']);

function Hint({ children }: { children: React.ReactNode }) {
  return <div className="text-muted-foreground py-4 text-center text-sm">{children}</div>;
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-background/60 p-3">
      <div className="text-lg font-semibold">{value}</div>
      <div className="mt-0.5 text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

export function OverviewCards() {
  const { data, error, loading, refresh } = useCardData<DashboardData>(fetchDashboard);
  const { register } = useRefresh();

  useEffect(() => {
    const unregister = register(refresh);
    return unregister;
  }, [register, refresh]);

  return (
    <>
      {/* System */}
      <Card>
        <CardHeader><CardTitle>System</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-2 gap-3">
          <Stat label="Uptime" value={data ? formatUptime(Number(data.system.uptime)) : '-'} />
          <Stat label="Memory (MB)" value={data ? String(data.system.memory.heapUsed) : '-'} />
          <Stat label="Node.js" value={data ? (data.system.nodeVersion || data.system.goVersion || '-') : '-'} />
        </CardContent>
      </Card>

      {/* API Usage */}
      <Card>
        <CardHeader><CardTitle>API Usage</CardTitle></CardHeader>
        <CardContent>
          {loading && !data ? (
            <Hint>Loading...</Hint>
          ) : error ? (
            <Hint>{error}</Hint>
          ) : data ? (
            <>
              <div className="text-3xl font-bold text-foreground">
                {data.api.totalRequests.toLocaleString()}
              </div>
              <div className="text-xs text-muted-foreground">Total Requests</div>
              <div className="mt-3 text-sm text-muted-foreground">
                {data.api.requestsPerMinute.toFixed(2)} req/min
              </div>
              <div className="mt-3 flex flex-wrap gap-3">
                {Object.entries(data.api.byMethod).map(([m, c]) => (
                  <span key={m} className={`method-${m.toLowerCase()}`}>{m}: {c}</span>
                ))}
              </div>
              <div className="mt-3 flex flex-wrap gap-3">
                {Object.entries(data.api.byStatus).map(([s, c]) => (
                  <span key={s} className={`status-${s}xx`}>{s}xx: {c}</span>
                ))}
              </div>
            </>
          ) : null}
        </CardContent>
      </Card>

      {/* Database */}
      <Card>
        <CardHeader><CardTitle>Database</CardTitle></CardHeader>
        <CardContent>
          {!data?.database ? (
            <Hint>Loading...</Hint>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              {Object.entries(data.database)
                .filter(([k]) => !DB_DENY.has(k))
                .map(([k, v]) => (
                  <div key={k} className="rounded-lg bg-background/60 p-3 text-center">
                    <div className="text-xl font-bold">{String(v)}</div>
                    <div className="mt-1 text-[10px] uppercase text-muted-foreground">{formatLabel(k)}</div>
                  </div>
                ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Background Tasks */}
      <Card>
        <CardHeader><CardTitle>Background Tasks</CardTitle></CardHeader>
        <CardContent className="flex flex-col gap-3">
          {!data ? (
            <Hint>Loading...</Hint>
          ) : (
            data.backgroundTasks.map((t) => (
              <div key={t.name} className="flex items-center justify-between gap-3 rounded-lg bg-background/60 p-3">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">{formatTaskName(t.name)}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {t.lastRun ? `Last: ${formatTime(t.lastRun)}` : 'Never run'}
                    {t.nextRun ? ` | Next: ${formatTimeUntil(t.nextRun)}` : ''}
                  </div>
                </div>
                <span className={statusClass(t.status)}>{t.status}</span>
              </div>
            ))
          )}
        </CardContent>
      </Card>
    </>
  );
}