# API Compatibility

This document describes the REST contract that torrent-search-go implements and notes any compatibility considerations for clients.

## REST Contract

torrent-search-go implements a clean, self-contained REST API. All responses are JSON. The API is designed so that any frontend or addon that speaks JSON over HTTP can integrate against it.

See [api-reference.md](api-reference.md) for the complete endpoint reference.

## Route Aliases

Several route prefixes are provided as aliases for backward compatibility. Both prefixes behave identically:

| Alias A | Alias B |
|---------|---------|
| `/api/cache/stats` | `/api/storage/stats` |
| `/api/cache/stream-url/*` | `/api/storage/stream-url/*` |
| `/api/cache/cover-image/*` | `/api/storage/cover-image/*` |
| `/api/cache/cover-image/favorite/:id` | `/api/storage/cover-image/favorite/:id` |
| `/api/cache/cover-image/cached-link/:id` | `/api/storage/cover-image/cached-link/:id` |
| `/api/cache/cover-image/torrent-details/:favoriteId/:source` | `/api/storage/cover-image/torrent-details/:favoriteId/:source` |
| `/api/cache/favorites` | `/api/storage/favorites` |
| `/api/cache/cached-links/*` | `/api/storage/cached-links/*` |
| `/api/cache/set` | `/api/storage/set` |
| `/api/cache/get/:key` | `/api/storage/get/:key` |
| `/api/cache/delete/:key` | `/api/storage/delete/:key` |
| `/api/google-images/*` | `/api/images/google-images/*` |
| `/api/images/search` | `/api/images/google-images/search` |
| `/api/images/suggestions` | `/api/images/google-images/suggestions` |
| `/api/proxy/search` | `/api/images/google-images/search` |
| `/api/proxy/suggestions` | `/api/images/google-images/suggestions` |
| `/api/pixhost/*` | `/api/images/pixhost/*` |
| `/api/torrents/:website/:query/:page?` | `/api/torrents/search/:website/:query/:page?` |
| `/api/torrent-details/:website/:torrentUrl` | `/api/torrents/torrent-details/:website/:torrentUrl` |

Prefer the canonical (`/api/images/`, `/api/storage/`) prefixes for new integrations.

## Authentication Compatibility

Sessions are created via Google OAuth. The session token is issued as:
- A `session_token` HttpOnly cookie for browser clients
- A JSON body field `sessionToken` for the `/api/auth/exchange` code exchange flow (for SPAs and mobile clients)

Once obtained, the session token can be sent as:
- `Cookie: session_token=<token>` (browser default)
- `Authorization: Bearer <token>` (programmatic / mobile clients)

Both mechanisms are equivalent.

## Torrent Field Names

The normalized `Torrent` object returned by all search and browse endpoints:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Torrent display name |
| `size` | string | Human-readable size ("2.1 GB") |
| `seeders` | int | Current seeder count |
| `leechers` | int | Current leecher count |
| `magnetLink` | string | Full magnet URI |
| `source` | string | Indexer name (e.g. `"piratebay"`) |
| `category` | string | Category string (indexer-specific) |
| `uploadDate` | string | ISO date or relative string as provided by the indexer |
| `coverImage` | string | Cover image URL if available, otherwise `""` |
| `torrentUrl` | string | Source page URL (used for detail fetches) |

## Known Limitations

Recent work closed the previous NodeJS parity gaps for the browse UI contract:

- Rate limiting is active for `/api/auth/*` and `/api/*`.
- Security headers (Helmet equivalent) are applied to all responses.
- `X-Request-Id` propagation is implemented.
- Redis catalog cache, search query cache, and S3 cover storage maintenance jobs are implemented.
- The `pornrips` scraper is included.
- Magnet cache (`/api/cache/magnet`) uses the Node `source`/`url`/`magnet` contract.
- Torrent details endpoints return the Node wire shape (`description`, `files`, `comments`, `images`, optional `magnet`/`hash`).
- Auth user payloads include `createdAt` and `lastLoginAt` where available.
- Image route aliases (`/api/images/search`, `/api/proxy/search`, etc.) are registered.

Remaining minor divergences:

- `POST /api/cache/cover-image/torrent` returns `200 {found:false}` on miss (matches Node); key lookup via `GET /api/cache/cover-image/:key` returns `404` (matches Node).
- `DELETE /api/cache/cover-image` with `{ torrent }` body removes the stored cover image (mirrored under `/api/storage/cover-image`).

## Pagination

- Page numbers start at **1** (not 0).
- The default page is 1 when the `:page?` path segment is omitted.
- There is no server-enforced page size cap - use `maxResults` in `POST /api/torrents/advanced-search` to limit results.

## Error Responses

All error responses follow the shape:

```json
{ "success": false, "error": "<human-readable message>" }
```

HTTP status codes:
- `400` - malformed request / missing required fields
- `401` - authentication required
- `403` - forbidden (allowlist / permissions)
- `404` - resource not found
- `405` - method not allowed
- `500` - internal server error
- `404` - route not found
