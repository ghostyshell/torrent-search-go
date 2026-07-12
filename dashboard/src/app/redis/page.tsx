'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { ArrowLeft, RefreshCw, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { bustAllRedisCaches, bustRedisCache, fetchRedisCaches } from '@/lib/api';
import type { RedisCacheGroup } from '@/lib/types';

function formatTTL(seconds: number): string {
  if (seconds <= 0) return '-';
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86400)}d`;
}

export default function RedisCachesPage() {
  const [groups, setGroups] = useState<RedisCacheGroup[]>([]);
  const [redisEnabled, setRedisEnabled] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null); // prefix being busted, or 'all'

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetchRedisCaches();
      if (!res.success) throw new Error(res.error || 'Failed to load');
      setGroups(res.groups || []);
      setRedisEnabled(res.redisEnabled);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const bust = async (prefix: string, label: string) => {
    if (!window.confirm(`Bust the "${label}" cache (${prefix})? This deletes every key under that prefix.`)) return;
    setBusy(prefix);
    setMsg(null);
    try {
      const res = await bustRedisCache(prefix);
      if (!res.success) throw new Error(res.error || 'Bust failed');
      setMsg(`Busted ${res.deleted ?? 0} key(s) from ${label}.`);
      await load();
    } catch (e: unknown) {
      setMsg('Error: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setBusy(null);
    }
  };

  const bustAll = async () => {
    if (!window.confirm('Bust ALL Redis caches? This deletes every key under every registered prefix.')) return;
    setBusy('all');
    setMsg(null);
    try {
      const res = await bustAllRedisCaches();
      if (!res.success) throw new Error(res.error || 'Bust all failed');
      setMsg(`Busted ${res.deleted ?? 0} key(s) across all caches.`);
      await load();
    } catch (e: unknown) {
      setMsg('Error: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="mx-auto max-w-[1280px] p-4 sm:p-6 lg:p-8">
      <header className="mb-6 flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <Link href="/" className="text-muted-foreground transition hover:text-foreground">
            <ArrowLeft className="h-5 w-5" />
          </Link>
          <h1 className="text-2xl font-semibold text-foreground">Redis caches</h1>
        </div>
        <div className="flex items-center gap-2">
          <Link href="/">
            <Button variant="outline">Back to monitoring</Button>
          </Link>
          <Button variant="destructive" onClick={() => void bustAll()} disabled={busy !== null || !redisEnabled}>
            <Trash2 className="h-4 w-4" />
            Bust all
          </Button>
          <Button variant="accent" onClick={() => void load()} disabled={loading}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </header>

      {msg && (
        <div className="mb-4 rounded-md border border-border bg-secondary/40 px-4 py-2 text-sm text-foreground">{msg}</div>
      )}
      {error && (
        <div className="mb-4 rounded-md border border-destructive/40 bg-destructive/10 px-4 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      {!redisEnabled && (
        <Card className="mb-4">
          <CardContent className="py-6 text-sm text-muted-foreground">
            Redis is not configured on the backend. Caches are disabled and there is nothing to view or bust.
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Cache groups</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="text-sm text-muted-foreground">Loading...</p>
          ) : groups.length === 0 ? (
            <p className="text-sm text-muted-foreground">No cache groups registered.</p>
          ) : (
            <Table>
              <TableHeader>
                <tr>
                  <th className="text-xs font-medium uppercase tracking-wide">Label</th>
                  <th className="text-xs font-medium uppercase tracking-wide">Prefix</th>
                  <th className="text-xs font-medium uppercase tracking-wide">Description</th>
                  <th className="text-xs font-medium uppercase tracking-wide">TTL</th>
                  <th className="text-xs font-medium uppercase tracking-wide text-right">Keys</th>
                  <th className="text-xs font-medium uppercase tracking-wide text-right">Action</th>
                </tr>
              </TableHeader>
              <TableBody>
                {groups.map((g) => (
                  <TableRow key={g.prefix}>
                    <TableCell className="font-medium text-foreground">{g.label}</TableCell>
                    <TableCell>
                      <code className="text-xs text-foreground">{g.prefix}</code>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{g.description}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{formatTTL(g.ttlSeconds)}</TableCell>
                    <TableCell className="text-right tabular-nums text-foreground">{g.keyCount}</TableCell>
                    <TableCell className="text-right">
                      <Button
                        size="sm"
                        variant="destructive"
                        disabled={busy !== null || !redisEnabled}
                        onClick={() => void bust(g.prefix, g.label)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                        {busy === g.prefix ? 'Busting...' : 'Bust'}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}