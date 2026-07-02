'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { RefreshCw } from 'lucide-react';
import { useRefresh } from '@/lib/refresh-context';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';

export function DashboardHeader() {
  const { refreshAll } = useRefresh();
  const [auto, setAuto] = useState(true);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (intervalRef.current) clearInterval(intervalRef.current);
    if (auto) {
      intervalRef.current = setInterval(() => void refreshAll(), 10000);
    }
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [auto, refreshAll]);

  return (
    <header className="mb-6 flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-center sm:justify-between">
      <h1 className="text-2xl font-semibold text-foreground">Backend Monitoring Dashboard</h1>
      <div className="flex items-center gap-3">
        <label className="flex cursor-pointer items-center gap-2 text-sm text-muted-foreground">
          <Checkbox checked={auto} onCheckedChange={(v) => setAuto(v === true)} id="autoRefresh" />
          Auto-refresh (10s)
        </label>
        <Button onClick={() => void refreshAll()} variant="accent">
          <RefreshCw className="h-4 w-4" />
          Refresh Now
        </Button>
        <Link href="/addons/">
          <Button variant="outline">Manage addon status</Button>
        </Link>
        <Link href="/redis/">
          <Button variant="outline">Redis caches</Button>
        </Link>
      </div>
    </header>
  );
}