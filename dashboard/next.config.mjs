/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static HTML/JS/CSS export - the Go binary serves these files, no Node runtime in prod.
  output: 'export',
  // No server-side image optimization in a static export.
  images: { unoptimized: true },
  // App Router + static export: emit per-route index.html (e.g. addons/index.html)
  // so the Go root-catch-all file server serves /addons/ and / alike.
  trailingSlash: true,
  // The dashboard is served same-origin by the Go backend at '/', so assets resolve from '/_next'.
  // (assetPrefix / basePath left default so the export uses absolute '/_next/...' paths.)
  reactStrictMode: true,
};

export default nextConfig;