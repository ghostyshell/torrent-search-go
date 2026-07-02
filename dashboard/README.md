# Backend Monitoring Dashboard

Next.js 15 + React + TypeScript + Tailwind + shadcn/ui rebuild of the legacy
`static/dashboard.html`. Static-exported (`output: 'export'`) and served by the
Go binary, so production stays a single process - no Node runtime needed.

## Develop

```sh
cd dashboard
npm install
npm run dev        # http://localhost:3000 (proxies API calls to the Go backend at :8080)
```

In dev the dashboard calls same-origin `/api/monitoring/*`, so run the Go
backend alongside it (`go run .` from the repo root) and either proxy or hit
the Go server directly. The password prompt + `X-Dashboard-Password` header
are wired in `src/lib/auth.ts` (mirrors the old `dashboard.html` cookie flow).

## Build (what the Go binary serves)

```sh
cd dashboard
npm ci
npm run build       # -> dashboard/out/  (index.html + _next/static/*)
```

The Dockerfile's `web-builder` stage runs this and copies `out/` into the Go
image at `/app/static/dashboard`. `main.go registerStaticRoutes` serves that
directory at `/` (see `next.config.mjs` for the export settings).

## Layout

- `src/app/` - root layout + the dashboard page (composes all cards).
- `src/lib/` - typed API client, auth/cookie+fetch override, formatters, types,
  refresh context (lets the header drive every card's fetcher).
- `src/hooks/` - `useCardData` generic fetch hook.
- `src/components/ui/` - shadcn/ui primitives (Button, Card, Tabs, Select,
  Table, Input, Checkbox, ScrollArea, Separator, Badge).
- `src/components/` - feature cards, one per section of the old dashboard.

Design system: `ui-ux-pro-max` "Dark Mode (OLED)" palette (see
`src/app/globals.css` semantic tokens), Fira Sans + Fira Code, Lucide icons,
visible focus rings, `prefers-reduced-motion` respected.