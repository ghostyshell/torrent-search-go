'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRefresh } from '@/lib/refresh-context';
import { blockIP, fetchBlockedIPs, fetchIPTraffic, unblockIP } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHeader, TableRow } from '@/components/ui/table';
import type { BlockedIPRow, IPTrafficRow } from '@/lib/types';

export function IPTrafficCard() {
  const [traffic, setTraffic] = useState<IPTrafficRow[] | null>(null);
  const [blocked, setBlocked] = useState<BlockedIPRow[] | null>(null);
  const [tError, setTError] = useState<string | null>(null);
  const [bError, setBError] = useState<string | null>(null);
  const [blockIPVal, setBlockIPVal] = useState('');
  const [blockNotes, setBlockNotes] = useState('');

  const loadTraffic = useCallback(async () => {
    try {
      const data = await fetchIPTraffic();
      setTraffic(data.traffic || []);
      setTError(null);
    } catch (e: unknown) {
      setTError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const loadBlocked = useCallback(async () => {
    try {
      const data = await fetchBlockedIPs();
      setBlocked(data.blocked || []);
      setBError(null);
    } catch (e: unknown) {
      setBError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const refresh = useCallback(async () => {
    await Promise.all([loadTraffic(), loadBlocked()]);
  }, [loadTraffic, loadBlocked]);

  const { register } = useRefresh();
  useEffect(() => {
    const unregister = register(refresh);
    void refresh();
    return unregister;
  }, [register, refresh]);

  const doBlock = async (ip: string, notes?: string) => {
    const res = await blockIP(ip, notes);
    if (!res.success) throw new Error(res.error || '');
    await refresh();
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const ip = blockIPVal.trim();
    if (!ip) return;
    try {
      await doBlock(ip, blockNotes.trim() || undefined);
      setBlockIPVal('');
      setBlockNotes('');
    } catch (e: unknown) {
      window.alert('Block failed: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  const quickBlock = async (ip: string) => {
    if (!window.confirm(`Block ${ip}?`)) return;
    try {
      await doBlock(ip, 'manual from dashboard');
    } catch (e: unknown) {
      window.alert('Block failed: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  const doUnblock = async (ip: string) => {
    if (!window.confirm(`Unblock ${ip}?`)) return;
    try {
      const res = await unblockIP(ip);
      if (!res.success) throw new Error(res.error || '');
      await refresh();
    } catch (e: unknown) {
      window.alert('Unblock failed: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  return (
    <Card className="xl:col-span-full">
      <CardHeader>
        <CardTitle className="flex items-center justify-between normal-case">
          <span className="text-base font-semibold text-foreground">IP Traffic &amp; Block Management</span>
          <Button size="sm" variant="outline" onClick={() => void refresh()}>Refresh</Button>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid gap-5 lg:grid-cols-2">
          {/* Live traffic */}
          <div>
            <div className="mb-2 text-sm font-semibold text-muted-foreground">
              Live Traffic (top 50 by req/min)
            </div>
            <div className="text-xs">
              {tError ? (
                <div className="text-red-400">{tError}</div>
              ) : !traffic ? (
                <div className="text-muted-foreground">Loading...</div>
              ) : traffic.length === 0 ? (
                <div className="text-muted-foreground">No traffic recorded yet.</div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <th>IP</th>
                      <th className="text-right" title="last 1 minute">1m</th>
                      <th className="text-right" title="last 1 hour">1h</th>
                      <th className="text-right" title="last 6 hours">6h</th>
                      <th className="text-right" title="last 24 hours">1d</th>
                      <th className="text-right" title="last 7 days">1w</th>
                      <th className="text-right" title="last 30 days">1mo</th>
                      <th className="text-center">Status</th>
                      <th />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {traffic.map((r) => (
                      <TableRow key={r.ip}>
                        <TableCell className="font-mono">{r.ip}</TableCell>
                        <TableCell className="text-right">{r.requestCount ?? 0}</TableCell>
                        <TableCell className="text-right">{r.count1h ?? 0}</TableCell>
                        <TableCell className="text-right">{r.count6h ?? 0}</TableCell>
                        <TableCell className="text-right">{r.count1d ?? 0}</TableCell>
                        <TableCell className="text-right">{r.count1w ?? 0}</TableCell>
                        <TableCell className="text-right">{r.count1mo ?? 0}</TableCell>
                        <TableCell className="text-center">
                          <span
                            className={`rounded px-2 py-0.5 text-[11px] ${
                              r.isBlocked ? 'bg-red-900/50 text-red-300' : 'bg-emerald-900/50 text-emerald-300'
                            }`}
                          >
                            {r.isBlocked ? 'Blocked' : 'Active'}
                          </span>
                        </TableCell>
                        <TableCell className="text-right">
                          {r.isBlocked ? (
                            <Button size="sm" variant="secondary" onClick={() => void doUnblock(r.ip)}>Unblock</Button>
                          ) : (
                            <Button size="sm" variant="destructive" onClick={() => void quickBlock(r.ip)}>Block</Button>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          </div>

          {/* Blocked IPs */}
          <div>
            <div className="mb-2 text-sm font-semibold text-muted-foreground">Blocked IPs</div>
            <form onSubmit={onSubmit} className="mb-3 flex flex-wrap gap-2">
              <Input
                value={blockIPVal}
                onChange={(e) => setBlockIPVal(e.target.value)}
                placeholder="IP address"
                required
                className="min-w-[120px] flex-1"
              />
              <Input
                value={blockNotes}
                onChange={(e) => setBlockNotes(e.target.value)}
                placeholder="Notes (optional)"
                className="min-w-[160px] flex-[2]"
              />
              <Button type="submit" variant="destructive">Block</Button>
            </form>
            <div className="text-xs">
              {bError ? (
                <div className="text-red-400">{bError}</div>
              ) : !blocked ? (
                <div className="text-muted-foreground">Loading...</div>
              ) : blocked.length === 0 ? (
                <div className="text-muted-foreground">No blocked IPs.</div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <th>IP</th>
                      <th>Reason</th>
                      <th>Req count</th>
                      <th>Blocked at</th>
                      <th />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {blocked.map((r) => (
                      <TableRow key={r.ip}>
                        <TableCell className="font-mono">{r.ip}</TableCell>
                        <TableCell>
                          <span
                            className={`rounded px-1.5 py-0.5 text-[11px] ${
                              r.reason === 'auto'
                                ? 'bg-amber-900/50 text-amber-300'
                                : 'bg-indigo-900/50 text-indigo-300'
                            }`}
                          >
                            {r.reason}
                          </span>
                          {r.notes ? <span className="ml-1 text-muted-foreground">{r.notes}</span> : null}
                        </TableCell>
                        <TableCell className="text-right">{r.requestCount && r.requestCount > 0 ? r.requestCount : '-'}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {r.blockedAt ? new Date(r.blockedAt).toLocaleString() : '-'}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button size="sm" variant="secondary" onClick={() => void doUnblock(r.ip)}>Unblock</Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}