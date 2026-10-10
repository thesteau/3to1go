# Shared Go packages

Station and Scout depend on this local module through their existing `replace`
directives and the `app/go.work` file.

- `auth`: account types, password verification, account lifecycle, session
  middleware, and account HTTP handlers. SQL remains in each application's store.
- `certificates`: certificate files, trust-store installation, and TLS roots.
- `hooks`: hook files and command execution, including caller cancellation.
- `httpx`: JSON responses, validation, rate limiting, request logging, and route
  parameter adaptation. Applications supply route policies.
- `integrations`: encrypted destination storage, write-only admin APIs, bounded
  background HTTP notifications, templates, and generic payload formats.
- `keylock`: per-key mutex allocation for blocking and nonblocking callers.
- `configutil`: common configuration coercion and log-level parsing.
- `protocol`: Scout–Station wire types and constants.
- `webui`: embedded page assembly, caching, and index serving.

Application entry points, database implementations, backup workflows, and
platform-specific configuration paths remain in Station and Scout. Shared packages
must not import either application.

After `npm ci` and `npm run build`, run all Go tests from the `app/` directory:

```sh
go test -race ./shared/go/... ./station/... ./scout/...
```
