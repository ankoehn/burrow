# Burrow Web Dashboard

React + TypeScript + Vite + Tailwind + shadcn/ui frontend for the Burrow relay.

- Build: `npm run build` → `dist/` (committed; embedded by the Go server via `//go:embed`).
- Test: `npm test` (Vitest + React Testing Library).
- Lint / types: `npm run lint`, `npx tsc -b --noEmit`.
- Dev: `npm run dev` (proxies `/api` to the local server).

Note: the dashboard is served same-origin by `burrowd`; SSE (`/api/v1/events`)
requires the SPA and API on the same origin.
