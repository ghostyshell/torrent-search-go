'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRefresh } from '@/lib/refresh-context';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { formatTime, formatNextRun } from '@/lib/format';
import type { JobStatusData, JobLog, TriggerAck } from '@/lib/types';

export interface JobTrigger {
  label: string;
  variant?: 'default' | 'accent' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link';
  run: () => Promise<TriggerAck>;
  successMsg: string;
}

interface Props {
  title: string;
  fetcher: () => Promise<JobStatusData>;
  triggers: JobTrigger[];
  renderHistory: (log: JobLog) => string;
  stripPrefix: string;
}

const STATUS_TEXT: Record<string, string> = {
  idle: 'Idle',
  running: 'Running...',
  completed: 'Completed',
  error: 'Error',
};

const STATUS_BADGE: Record<string, string> = {
  idle: 'status-idle',
  running: 'status-running',
  completed: 'status-completed',
  error: 'status-error',
};

export function JobStatusCard({ title, fetcher, triggers, renderHistory, stripPrefix }: Props) {
  const [data, setData] = useState<JobStatusData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [flash, setFlash] = useState<{ msg: string; ok: boolean } | null>(null);
  const [btnLabel, setBtnLabel] = useState<Record<number, string>>({});
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const refresh = useCallback(async () => {
    try {
      const d = await fetcherRef.current();
      if (!d.success) throw new Error(d.error || '');
      setData(d);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const { register } = useRefresh();
  useEffect(() => {
    const unregister = register(refresh);
    void refresh();
    return unregister;
  }, [register, refresh]);

  // 2s fast-poll while the job is running (mirrors the old setTimeout recursion).
  useEffect(() => {
    if (data?.status !== 'running') return;
    const t = setTimeout(() => void refresh(), 2000);
    return () => clearTimeout(t);
  }, [data?.status, refresh]);

  const runTrigger = async (idx: number, t: JobTrigger) => {
    try {
      setBtnLabel((s) => ({ ...s, [idx]: 'Starting...' }));
      const res = await t.run();
      if (!res.success) throw new Error(res.error || '');
      setBtnLabel((s) => ({ ...s, [idx]: 'Job Started!' }));
      setTimeout(() => setBtnLabel((s) => ({ ...s, [idx]: '' })), 2000);
      await refresh();
      setFlash({ msg: t.successMsg, ok: true });
      setTimeout(() => setFlash(null), 5000);
    } catch (e: unknown) {
      setBtnLabel((s) => ({ ...s, [idx]: 'Failed' }));
      setTimeout(() => setBtnLabel((s) => ({ ...s, [idx]: '' })), 2000);
      setFlash({ msg: e instanceof Error ? e.message : String(e), ok: false });
    }
  };

  const status = data?.status ?? 'idle';

  return (
    <Card className="xl:col-span-full">
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-3 normal-case">
          <span className="text-base font-semibold text-foreground">{title}</span>
          {triggers.map((t, i) => (
            <Button
              key={i}
              size="sm"
              variant={t.variant ?? 'default'}
              onClick={() => void runTrigger(i, t)}
            >
              {btnLabel[i] || t.label}
            </Button>
          ))}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {flash && (
          <div
            className={`mb-3 rounded-md px-3 py-2 text-sm ${
              flash.ok ? 'bg-emerald-900/40 text-emerald-300' : 'bg-red-900/40 text-red-300'
            }`}
          >
            {flash.msg}
          </div>
        )}
        <div className={`mb-3 rounded-md px-3 py-2 text-sm ${STATUS_BADGE[status] || 'status-idle'}`}>
          <span className="font-semibold">Status:</span> {STATUS_TEXT[status] || status}
          {data?.lastRun ? ` | Last Run: ${formatTime(data.lastRun)}` : ''}
          {data?.nextRun ? ` | Next Run: ${formatNextRun(data.nextRun)}` : ''}
        </div>

        <div className="max-h-[300px] overflow-y-auto font-mono text-xs">
          {error ? (
            <div className="text-muted-foreground px-3 py-4">Error loading logs: {error}</div>
          ) : !data ? (
            <div className="text-muted-foreground px-3 py-4 text-center">Loading...</div>
          ) : (
            <JobLogs data={data} stripPrefix={stripPrefix} renderHistory={renderHistory} />
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function JobLogs({
  data,
  stripPrefix,
  renderHistory,
}: {
  data: JobStatusData;
  stripPrefix: string;
  renderHistory: (log: JobLog) => string;
}) {
  let html: React.ReactNode[] = [];

  if (data.recentAppLogs && data.recentAppLogs.length > 0) {
    const isRunning = data.status === 'running';
    html.push(
      <div
        key="hdr"
        className={`mb-3 px-3 font-bold ${isRunning ? 'text-amber-400' : 'text-blue-400'}`}
      >
        {isRunning ? 'Live Progress:' : 'Recent Activity:'}
      </div>,
    );
    data.recentAppLogs.slice(0, 30).map((log, i) => {
      const msg = (log.message || log.raw || '').replace(stripPrefix, '').trim();
      const lvl = (log.level || 'info').toLowerCase();
      return (
        <div key={`a${i}`} className="flex gap-2 border-b border-border px-3 py-1.5 text-[11px]">
          <span className="shrink-0 text-muted-foreground">{log.timestamp ? formatTime(log.timestamp) : ''}</span>
          <span
            className={`shrink-0 rounded px-1.5 py-0.5 font-semibold ${
              lvl === 'error' ? 'text-red-400' : lvl === 'warn' ? 'text-amber-400' : 'text-blue-400'
            }`}
          >
            {(log.level || 'info').toUpperCase()}
          </span>
          <span className="min-w-0 break-words">{msg}</span>
        </div>
      );
    });
    if ((data.logs || []).length > 0) {
      html.push(<div key="sep" className="my-4 border-t border-border" />);
    }
  }

  if ((data.logs || []).length > 0) {
    html.push(
      <div key="hist-hdr" className="mb-3 px-3 font-bold text-foreground">
        Job History:
      </div>,
    );
    html = html.concat(
      (data.logs || []).map((log, i) => (
        <div key={`h${i}`} className="flex gap-3 border-b border-border px-3 py-2">
          <span className="shrink-0 text-muted-foreground">{formatTime(log.timestamp)}</span>
          <span
            className={`shrink-0 font-semibold ${
              log.success ? 'text-blue-400' : 'text-red-400'
            }`}
          >
            {log.success ? 'SUCCESS' : 'FAILED'}
          </span>
          <span className="min-w-0 break-words">{renderHistory(log)}</span>
        </div>
      )),
    );
  }

  if (html.length === 0) {
    return <div className="text-muted-foreground px-3 py-4 text-center">No logs available yet. Trigger a run to see logs.</div>;
  }
  return <>{html}</>;
}