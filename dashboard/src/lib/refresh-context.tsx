'use client';

import { createContext, useContext, useRef, useCallback } from 'react';

// Lets the dashboard header's "Refresh Now" + auto-refresh tick call every
// card's fetcher, mirroring the old single `refreshAll()` that fired all fetch
// functions. Each card registers its fetcher on mount.

type Fetcher = () => Promise<void> | void;

interface RefreshCtx {
  register: (fn: Fetcher) => () => void;
  refreshAll: () => Promise<void>;
}

const Ctx = createContext<RefreshCtx | null>(null);

export function RefreshProvider({ children }: { children: React.ReactNode }) {
  const fetchers = useRef<Set<Fetcher>>(new Set());

  const register = useCallback((fn: Fetcher) => {
    fetchers.current.add(fn);
    return () => {
      fetchers.current.delete(fn);
    };
  }, []);

  const refreshAll = useCallback(async () => {
    await Promise.all(
      Array.from(fetchers.current).map(async (fn) => {
        try {
          await fn();
        } catch {
          /* individual card errors are handled inside each card */
        }
      }),
    );
  }, []);

  return <Ctx.Provider value={{ register, refreshAll }}>{children}</Ctx.Provider>;
}

export function useRefresh(): RefreshCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error('useRefresh must be used within RefreshProvider');
  return ctx;
}