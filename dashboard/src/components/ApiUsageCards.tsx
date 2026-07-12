'use client';

import { useEffect } from 'react';
import { useCardData } from '@/hooks/use-card-data';
import { useRefresh } from '@/lib/refresh-context';
import { fetchApiUsage } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { methodClass, statusBandClass } from '@/lib/format';
import type { ApiUsageData } from '@/lib/types';

function Hint({ children }: { children: React.ReactNode }) {
  return <div className="text-muted-foreground py-4 text-center text-sm">{children}</div>;
}

export function ApiUsageCards() {
  const { data, refresh } = useCardData<ApiUsageData>(fetchApiUsage);
  const { register } = useRefresh();

  useEffect(() => {
    const unregister = register(refresh);
    return unregister;
  }, [register, refresh]);

  const top = data?.stats?.topEndpoints ?? [];
  const recent = data?.recentRequests ?? [];

  return (
    <>
      {/* Top Endpoints */}
      <Card>
        <CardHeader><CardTitle>Top Endpoints</CardTitle></CardHeader>
        <CardContent className="flex flex-col gap-2">
          {!data ? (
            <Hint>Loading...</Hint>
          ) : top.length > 0 ? (
            top.slice(0, 10).map((ep) => (
              <div key={ep.endpoint} className="flex items-center justify-between rounded-md bg-background/60 px-3 py-2 text-sm">
                <span className="truncate font-mono text-muted-foreground" title={ep.endpoint}>{ep.endpoint}</span>
                <span className="font-semibold text-accent">{ep.count}</span>
              </div>
            ))
          ) : (
            <Hint>No requests yet</Hint>
          )}
        </CardContent>
      </Card>

      {/* Recent Requests */}
      <Card>
        <CardHeader><CardTitle>Recent Requests</CardTitle></CardHeader>
        <CardContent>
          <div className="max-h-[300px] overflow-y-auto">
            {!data ? (
              <Hint>Loading...</Hint>
            ) : recent.length > 0 ? (
              recent.map((req, i) => (
                <div key={i} className="flex items-center gap-3 border-b border-border px-3 py-2 text-xs">
                  <span className={`shrink-0 font-semibold ${methodClass(req.method)}`}>{req.method}</span>
                  <span className="flex-1 truncate font-mono text-muted-foreground" title={req.path}>{req.path}</span>
                  <span className={`shrink-0 font-medium ${statusBandClass(req.status)}`}>{req.status}</span>
                  <span className="shrink-0 text-muted-foreground">{req.duration}ms</span>
                </div>
              ))
            ) : (
              <Hint>No requests yet</Hint>
            )}
          </div>
        </CardContent>
      </Card>
    </>
  );
}