'use client';

import { RefreshProvider } from '@/lib/refresh-context';
import { DashboardHeader } from '@/components/DashboardHeader';
import { OverviewCards } from '@/components/OverviewCards';
import { ApiUsageCards } from '@/components/ApiUsageCards';
import { ApplicationLogsCard } from '@/components/ApplicationLogsCard';
import { JobStatusCard } from '@/components/JobStatusCard';
import { JobLogsCard } from '@/components/JobLogsCard';
import { IPTrafficCard } from '@/components/IPTrafficCard';
import {
  fetchDescCacheLogs,
  fetchStreamRefreshLogs,
  triggerDescriptionImageCache,
  triggerDescriptionImageCacheForceRefresh,
  triggerStreamUrlRefresh,
} from '@/lib/api';
import type { JobLog } from '@/lib/types';

const streamRefreshHistory = (log: JobLog): string =>
  `${log.manual ? '[MANUAL] ' : '[SCHEDULED] '}${log.totalFavorites || 0} favorites | ${log.usersProcessed || 0} users | ${log.refreshed || 0} refreshed | ${log.skipped || 0} skipped | ${log.failed || 0} failed${log.error ? ` | Error: ${log.error}` : ''}`;

const descCacheHistory = (log: JobLog): string =>
  `${log.manual ? '[MANUAL] ' : '[SCHEDULED] '}${log.totalSearches || 0} searches | ${log.totalTorrents || 0} torrents | ${log.imagesFound || 0} images found | ${log.cached || 0} cached | ${log.replaced || 0} replaced | ${log.skipped || 0} skipped | ${log.failed || 0} failed${log.error ? ` | Error: ${log.error}` : ''}`;

export default function DashboardPage() {
  return (
    <RefreshProvider>
      <div className="mx-auto max-w-[1600px] p-4 sm:p-6 lg:p-8">
        <DashboardHeader />
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
          <OverviewCards />
          <ApiUsageCards />
        </div>

        <div className="mt-5 grid grid-cols-1 gap-5">
          <JobStatusCard
            title="Stream URL Refresh Job"
            fetcher={fetchStreamRefreshLogs}
            stripPrefix="[Stream Refresh]"
            renderHistory={streamRefreshHistory}
            triggers={[
              { label: 'Trigger Refresh Now', variant: 'default', run: triggerStreamUrlRefresh, successMsg: 'Stream URL refresh job has been triggered. Check logs below for progress.' },
            ]}
          />

          <JobStatusCard
            title="Description & Image Cache Job"
            fetcher={fetchDescCacheLogs}
            stripPrefix="[DescImageCache]"
            renderHistory={descCacheHistory}
            triggers={[
              { label: 'Trigger Cache Now', variant: 'default', run: triggerDescriptionImageCache, successMsg: 'Description & Image cache job has been triggered. Check logs below for progress.' },
              { label: 'Force Refresh All', variant: 'destructive', run: triggerDescriptionImageCacheForceRefresh, successMsg: 'Description & Image cache FORCE REFRESH job has been triggered. All covers will be replaced.' },
            ]}
          />

          <JobLogsCard />
          <ApplicationLogsCard />
          <IPTrafficCard />
        </div>
      </div>
    </RefreshProvider>
  );
}