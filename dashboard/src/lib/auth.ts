// Dashboard password auth - mirrors the cookie + fetch-override in the old
// static/dashboard.html. The password is stored in a cookie and sent as the
// X-Dashboard-Password header on every /api/monitoring/* call; a 401 re-prompts
// and retries once.

export function getDashboardPassword(): string {
  if (typeof document === 'undefined') return '';
  const entry = document.cookie
    .split(';')
    .map((c) => c.trim())
    .find((c) => c.startsWith('dashboard_auth='));
  return entry ? decodeURIComponent(entry.slice('dashboard_auth='.length)) : '';
}

export function setDashboardPassword(pw: string): void {
  if (typeof document === 'undefined') return;
  document.cookie =
    'dashboard_auth=' +
    encodeURIComponent(pw) +
    '; path=/; max-age=' +
    30 * 24 * 60 * 60 +
    '; SameSite=Strict';
}

export function promptDashboardPassword(message?: string): string {
  if (typeof window === 'undefined') return '';
  const pw = window.prompt(message || 'Enter dashboard password:', '');
  if (pw) setDashboardPassword(pw);
  return pw || '';
}

let installed = false;
const originalFetch: typeof fetch | null =
  typeof window !== 'undefined' ? window.fetch.bind(window) : null;

// Install the global fetch override that injects the dashboard password header
// on /api/monitoring/* calls and re-prompts on 401. Idempotent.
export function installAuthFetch(): void {
  if (typeof window === 'undefined' || installed || !originalFetch) return;

  window.fetch = (async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input?.url || '';
    if (!url.includes('/api/monitoring/')) {
      return originalFetch(input, init);
    }
    const withPw = (pw: string): RequestInit => ({
      ...init,
      headers: { ...(init.headers || {}), 'X-Dashboard-Password': pw },
    });
    let res = await originalFetch(input, withPw(getDashboardPassword()));
    if (res.status === 401) {
      const pw = promptDashboardPassword('Incorrect or missing dashboard password. Re-enter:');
      if (pw) res = await originalFetch(input, withPw(pw));
    }
    return res;
  }) as typeof fetch;

  installed = true;
}

// Prompt once on first load if no password is set (mirrors the old dashboard).
export function ensurePassword(): void {
  if (typeof window === 'undefined') return;
  if (!getDashboardPassword()) {
    promptDashboardPassword('Enter the dashboard password to access monitoring:');
  }
}