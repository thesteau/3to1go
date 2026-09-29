# Project decisions

- Backup change detection intentionally fingerprints sorted file paths and sizes, not file contents or timestamps. Same-size edits require a forced fresh backup. Do not recommend content hashing or describe the current fingerprint as content-based detection.
- Restore intentionally replaces destination files. Do not recommend changing that overwrite behavior.
- Authentication is intended for one operator to sign in and keep unauthorized entrants out. Do not expand the account system into a multi-user or tenant authorization model without an explicit request.
