# Project decisions

- Backup change detection intentionally fingerprints sorted file paths and sizes, not file contents or timestamps. Same-size edits require a forced fresh backup. Do not recommend content hashing or describe the current fingerprint as content-based detection.
- Restore intentionally replaces destination files. Do not recommend changing that overwrite behavior.
- The web UIs are written in TypeScript under `central/static/ts` and `edge/static/ts`. `*/static/js` is generated build output that Go embeds; it is gitignored and dockerignored and must not be committed or edited. After `npm ci`, generate it with `go generate ./...` (from `central/` or `edge/`) or `npm run build` before `go build`/`go test`; the Dockerfiles compile it in their own Node stage. The files are classic scripts that share one global scope per app (inline `onclick` handlers call their functions), so do not add `import`/`export`.
- Authentication is intended for one operator to sign in and keep unauthorized entrants out. Do not expand the account system into a multi-user or tenant authorization model without an explicit request.
