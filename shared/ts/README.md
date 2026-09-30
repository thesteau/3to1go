# Shared browser scripts

These classic TypeScript scripts are compiled separately into Central and Edge.
They share each application's global scope, so do not add `import` or `export`.
Each application's compiler checks them against that application's remaining
types and functions.

Share identical behavior here. Keep application-specific rendering, authentication
cleanup, settings payloads, and refresh behavior in the application's `static/ts`.
Shared feature functions can call local functions such as `renderCertificateFiles`
or `loadHookConfig`; both applications must supply those functions.

Run `npm run build` from the repository root, or `go generate ./...` from either
Go application. The existing watch commands also watch these shared sources.
No additional bundler or build script is needed.

The compiler uses the repository as its source root and preserves source paths
under each application's generated `static/js` directory:

- `shared/ts/users.ts` becomes `static/js/shared/ts/users.js`.
- `central/static/ts/app.ts` becomes `central/static/js/central/static/ts/app.js`.
- Edge follows the same layout under `edge/static/js`.

Each `index.html` lists shared scripts before its local scripts for that feature,
with application startup last. All scripts are deferred. Generated JavaScript
remains ignored and is built inside Docker's Node stage before Go embeds it.

Frontend tests use the script paths and order from `index.html`, so stale output
from an older build cannot substitute for the scripts used by the page.
