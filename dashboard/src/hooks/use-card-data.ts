'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

// Generic fetch hook for a dashboard card: holds data/error/loading, exposes
// a stable `refresh`. The fetcher is kept in a ref so callers can pass an
// inline closure without re-triggering on every render.
export function useCardData<T>(fetcher: () => Promise<T>) {
  const fnRef = useRef(fetcher);
  fnRef.current = fetcher;

  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const d = await fnRef.current();
      setData(d);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  return { data, error, loading, refresh };
}

// Registers `refresh` with the dashboard refresh context and runs it on mount.
export function useRegisterAndLoad(refresh: () => Promise<void> | void, register: (fn: () => Promise<void> | void) => () => void) {
  const ref = useRef(refresh);
  ref.current = refresh;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => register(() => ref.current()), [register]);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { void ref.current(); }, []);
}