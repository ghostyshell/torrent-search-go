'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRefresh } from '@/lib/refresh-context';
import { fetchJobLogList, searchJobLogFiles, triggerJobLogMaintenance } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import type { JobLogFile, JobLogListData, JobLogMatch } from '@/lib/types';

const JOBS = ['storageCleanup', 'streamUrlRefresh', 'descriptionImageCache', 'searchResultsCache', 'jobLogMaintenance'];

interface SearchPage {
  q: string;
  job: string;
  sort: 'asc' | 'desc';
  nextOffset: number;
  hasMore: boolean;
  loading: boolean;
}

export function JobLogsCard() {
  const [list, setList] = useState<JobLogListData | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [listLoading, setListLoading] = useState(false);

  const [q, setQ] = useState('');
  const [job, setJob] = useState('');
  const [sort, setSort] = useState<'asc' | 'desc'>('desc');

  const [matches, setMatches] = useState<JobLogMatch[]>([]);
  const [meta, setMeta] = useState('No search yet');
  const [page, setPage] = useState<SearchPage>({
    q: '',
    job: '',
    sort: 'desc',
    nextOffset: 0,
    hasMore: false,
    loading: false,
  });
  const [showLoadMore, setShowLoadMore] = useState(false);

  const resultsRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const observerRef = useRef<IntersectionObserver | null>(null);
  const pageRef = useRef(page);
  pageRef.current = page;
  const qRef = useRef({ q, job, sort });
  qRef.current = { q, job, sort };

  const refreshList = useCallback(async () => {
    setListLoading(true);
    try {
      const data = await fetchJobLogList();
      if (!data.success) throw new Error(data.error || '');
      setList(data);
      setListError(null);
    } catch (e: unknown) {
      setListError(e instanceof Error ? e.message : String(e));
    } finally {
      setListLoading(false);
    }
  }, []);

  const { register } = useRefresh();
  useEffect(() => {
    const unregister = register(refreshList);
    void refreshList();
    return unregister;
  }, [register, refreshList]);

  const disconnectObserver = useCallback(() => {
    if (observerRef.current) {
      observerRef.current.disconnect();
      observerRef.current = null;
    }
  }, []);

  const ensureObserver = useCallback(() => {
    const root = resultsRef.current;
    const sentinel = sentinelRef.current;
    if (!root || !sentinel) return;
    disconnectObserver();
    observerRef.current = new IntersectionObserver(
      (entries) => {
        const hit = entries[0];
        if (!hit || !hit.isIntersecting) return;
        if (!pageRef.current.hasMore || pageRef.current.loading) return;
        void search(false);
      },
      { root, rootMargin: '120px', threshold: 0 },
    );
    observerRef.current.observe(sentinel);
  }, [disconnectObserver]);

  const search = useCallback(
    async (reset: boolean) => {
      const cur = qRef.current;
      const query = cur.q.trim();
      if (!query) {
        disconnectObserver();
        setMeta('Enter a search string');
        setMatches([]);
        setShowLoadMore(false);
        return;
      }

      if (reset) {
        disconnectObserver();
        setPage({ q: query, job: cur.job, sort: cur.sort, nextOffset: 0, hasMore: false, loading: true });
        setMatches([]);
        setMeta('Searching...');
      } else {
        if (query !== pageRef.current.q || cur.job !== pageRef.current.job || cur.sort !== pageRef.current.sort) {
          return search(true);
        }
        if (!pageRef.current.hasMore || pageRef.current.loading) return;
        setPage((p) => ({ ...p, loading: true }));
      }

      setShowLoadMore(false);

      try {
        const data = await searchJobLogFiles({
          q: query,
          job: cur.job || undefined,
          sort: cur.sort,
          offset: pageRef.current.nextOffset,
        });
        if (!data.success) throw new Error(data.error || '');

        if (reset && data.matches.length === 0) {
          setMeta('No matches');
          setShowLoadMore(false);
          setPage((p) => ({ ...p, loading: false }));
          return;
        }

        setMatches((m) => m.concat(data.matches));
        const nextOffset = data.nextOffset;
        const hasMore = !!data.hasMore;
        setPage((p) => ({ ...p, nextOffset, hasMore, loading: false }));

        const sortLabel = cur.sort === 'desc' ? 'newest first' : 'oldest first';
        setMeta(
          `Showing ${nextOffset} hit(s) - sort: ${sortLabel}` +
            (hasMore ? ' - scroll down or use "Load more" for next page' : ' - end of results'),
        );
        setShowLoadMore(hasMore);

        if (hasMore) {
          // ensureObserver runs in an effect after the DOM updates
        } else {
          disconnectObserver();
        }
      } catch (e: unknown) {
        setMeta('Error: ' + (e instanceof Error ? e.message : String(e)));
        setShowLoadMore(false);
        disconnectObserver();
        setPage((p) => ({ ...p, loading: false }));
      }
    },
    [disconnectObserver],
  );

  // (Re)attach the observer whenever matches grow and there is more to load.
  useEffect(() => {
    if (page.hasMore && matches.length > 0) ensureObserver();
    return () => {};
  }, [page.hasMore, matches.length, ensureObserver]);

  const runMaintenance = async () => {
    try {
      const res = await triggerJobLogMaintenance();
      if (!res.success) throw new Error(res.error || '');
      window.alert(res.message || 'Started');
    } catch (e: unknown) {
      window.alert('Failed: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  return (
    <Card className="xl:col-span-full">
      <CardHeader>
        <CardTitle>Background job files (persistent)</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="mb-2 text-xs text-muted-foreground">
          Each scheduled/manual job run writes <code className="rounded bg-background/60 px-1">logs/background-jobs/v1/&lt;job&gt;/&lt;date&gt;/&lt;runId&gt;.log</code>.
          Maintenance gzips idle logs and deletes files older than 30 days (configurable via env).
        </p>

        <div className="mb-2 flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => void refreshList()}>Refresh file list</Button>
          <Button size="sm" variant="outline" onClick={() => void runMaintenance()}>Run maintenance now</Button>
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search substring..."
            className="min-w-[160px] flex-1"
            onKeyDown={(e) => {
              if (e.key === 'Enter') void search(true);
            }}
          />
          <Select value={job || 'all'} onValueChange={(v) => setJob(v === 'all' ? '' : v)}>
            <SelectTrigger className="w-[180px]"><SelectValue placeholder="All jobs" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All jobs</SelectItem>
              {JOBS.map((j) => (
                <SelectItem key={j} value={j}>{j}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={sort} onValueChange={(v) => setSort(v as 'asc' | 'desc')}>
            <SelectTrigger className="w-[170px]" title="File order by modified time; within each file, desc shows last matching lines first">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="desc">Newest first (desc)</SelectItem>
              <SelectItem value="asc">Oldest first (asc)</SelectItem>
            </SelectContent>
          </Select>
          <Button size="sm" onClick={() => void search(true)}>Search</Button>
        </div>

        <div className="mb-2 text-xs text-muted-foreground">
          {listLoading
            ? 'Loading...'
            : listError
              ? listError
              : list
                ? `logVersion=${list.logVersion} - ${list.count} file(s) - root: ${list.root}`
                : ''}
        </div>

        <div className="mb-4 max-h-[220px] overflow-y-auto font-mono text-xs">
          {!list ? (
            <div className="text-muted-foreground py-2">Click "Refresh file list"</div>
          ) : (list.files || []).length === 0 ? (
            <div className="text-muted-foreground py-2">No job log files yet</div>
          ) : (
            (list.files || []).slice(0, 80).map((f, i) => <JobLogFileRow key={i} f={f} />)
          )}
        </div>

        <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">Search hits</div>
        <div ref={resultsRef} className="relative max-h-[360px] overflow-y-auto">
          <div className="mb-2 text-[11px] text-muted-foreground">{meta}</div>
          <div>
            {matches.map((m, i) => (
              <div key={i} className="mb-2 break-all text-[11px]">
                <div className="mb-1 text-muted-foreground">{m.file}</div>
                <div>{m.line}</div>
              </div>
            ))}
          </div>
          <div ref={sentinelRef} className="mt-2 h-8" />
          {showLoadMore && (
            <Button size="sm" variant="outline" className="mt-1" onClick={() => void search(false)}>
              Load more
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function JobLogFileRow({ f }: { f: JobLogFile }) {
  const params = new URLSearchParams({ job: f.job, date: f.date, name: f.name });
  const streamUrl = `/api/monitoring/job-logs/file?${params}&mode=stream`;
  const matUrl = f.compressed ? `/api/monitoring/job-logs/file?${params}&mode=materialize` : null;
  return (
    <div className="mb-1.5">
      <span className="text-muted-foreground">{f.mtime}</span>{' '}
      <strong className="text-foreground">{f.job}</strong> / {f.date} / {f.name}
      {f.compressed ? <span className="text-muted-foreground"> (gzip)</span> : null}
      {' - '}
      <a href={streamUrl} target="_blank" rel="noopener noreferrer" className="text-accent hover:underline">
        Open (stream{f.compressed ? ' gunzip' : ''})
      </a>
      {matUrl ? (
        <>
          {' - '}
          <a href={matUrl} target="_blank" rel="noopener noreferrer" className="text-accent hover:underline">
            Download (temp decompress)
          </a>
        </>
      ) : null}
    </div>
  );
}