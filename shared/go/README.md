# Shared Go packages

Station and Scout depend on this local module through their existing `replace`
directives and the root `go.work` file.

- `auth`: account types, password verification, account lifecycle, session
  middleware, and account HTTP handlers. SQL remains in each application's store.
- `certificates`: certificate files, trust-store installation, and TLS roots.
- `hooks`: hook files and command execution, including caller cancellation.
- `httpx`: JSON responses, validation, rate limiting, request logging, and route
  parameter adaptation. Applications supply route policies.
- `ntfy`: template rendering. Notification delivery stays local because timeouts,
  event headers, filters, and error handling differ.
- `keylock`: per-key mutex allocation for blocking and nonblocking callers.
- `configutil`: common configuration coercion and log-level parsing.
- `protocol`: Scout–Station wire types and constants.
- `webui`: embedded page assembly, caching, and index serving.

Application entry points, database implementations, backup workflows, and
platform-specific configuration paths remain in Station and Scout. Shared packages
must not import either application.

After `npm ci` and `npm run build`, run all Go tests from the repository root:

```sh
go test -race ./shared/go/... ./station/... ./scout/...
```
