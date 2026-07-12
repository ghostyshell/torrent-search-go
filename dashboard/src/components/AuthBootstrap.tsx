'use client';

import { useEffect } from 'react';
import { ensurePassword, installAuthFetch } from '@/lib/auth';

// Mounts once app-wide in the root layout so every route gets the dashboard-password
// fetch override (X-Dashboard-Password on /api/monitoring/* calls) and the initial prompt.
export function AuthBootstrap() {
  useEffect(() => {
    installAuthFetch();
    ensurePassword();
  }, []);
  return null;
}