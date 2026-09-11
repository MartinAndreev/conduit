# QA test cases

The local workspace, CLI, standalone, and Astro checks were recorded as passed
at delivery. The final Pages item remains open because this audit inspected the
workflow definition but did not perform a live GitHub Pages deployment.

- [x] Root lint, typecheck, tests, and builds execute workspace tasks.
- [x] `pnpm start --help` exposes the Conduit CLI from repository root.
- [x] The CLI standalone Linux build starts and resolves bundled role templates.
- [x] The Astro package builds and its preview serves the landing page.
- [ ] The Pages workflow deploys only `packages/website/dist`.
