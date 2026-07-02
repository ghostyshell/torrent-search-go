'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRefresh } from '@/lib/refresh-context';
import { fetchLogs } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { formatTime } from '@/lib/format';
import type { LogsData, LogEntry } from '@/lib/types';

const LEVELS = ['all', 'info', 'warn', 'error'] as const;
type Level = (typeof LEVELS)[number];

function logLevelClass(level: string): string {
  switch (level) {
    case 'error':
      return 'text-red-400';
    case 'warn':
      return 'text-amber-400';
    case 'debug':
      return 'text-violet-400';
    default:
      return 'text-blue-400';
  }
}

export function ApplicationLogsCard() {
  const [level, setLevel] = useState<Level>('all');
  const [logs, setLogs] = useState<LogEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const levelRef = useRef(level);
  levelRef.current = level;

  const refresh = useCallback(async () => {
    try {
      const data = await fetchLogs(levelRef.current, 100);
      if (!data.success) throw new Error(data.error || '');
      setLogs(data.logs || []);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
      setLogs([]);
    }
  }, []);

  const { register } = useRefresh();
  useEffect(() => {
    const unregister = register(refresh);
    void refresh();
    return unregister;
  }, [register, refresh]);

  const onTabChange = (v: string) => {
    setLevel(v as Level);
    // fetch happens via the effect below watching `level`
  };

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [level]);

  return (
    <Card className="xl:col-span-full">
      <CardHeader>
        <CardTitle>Application Logs</CardTitle>
      </CardHeader>
      <CardContent>
        <Tabs value={level} onValueChange={onTabChange} className="mb-4">
          <TabsList>
            {LEVELS.map((l) => (
              <TabsTrigger key={l} value={l}>
                {l === 'all' ? 'All' : l.charAt(0).toUpperCase() + l.slice(1)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <div className="max-h-[400px] overflow-y-auto font-mono text-xs">
          {error ? (
            <div className="text-muted-foreground px-3 py-4">Error loading logs: {error}</div>
          ) : !logs ? (
            <div className="text-muted-foreground px-3 py-4 text-center">Loading...</div>
          ) : logs.length === 0 ? (
            <div className="text-muted-foreground px-3 py-4 text-center">No logs found</div>
          ) : (
            logs.map((log, i) => {
              const lvl = (log.level || 'info').toLowerCase();
              return (
                <div key={i} className="flex gap-3 border-b border-border px-3 py-2 hover:bg-secondary/40">
                  <span className="shrink-0 text-muted-foreground">
                    {log.timestamp ? formatTime(log.timestamp) : ''}
                  </span>
                  <span className={`w-12 shrink-0 font-semibold ${logLevelClass(lvl)}`}>
                    {(log.level || 'INFO').toUpperCase()}
                  </span>
                  <span className="min-w-0 break-words">
                    {log.message || ''}
                    {log.method ? ` ${log.method} ${log.url || log.path || ''}` : ''}
                  </span>
                </div>
              );
            })
          )}
        </div>
      </CardContent>
    </Card>
  );
}