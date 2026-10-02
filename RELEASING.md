# Releasing

Maintainer notes for the release pipeline. The contributor-facing overview is in [docs/releases.mdx](docs/releases.mdx).

## Workflows

| Workflow | Trigger | Does |
|---|---|---|
| Promote main to prod | Push to `main` | Opens or updates the single `main` → `prod` PR |
| Stable release | Push to `prod`, merge into `release-state`, manual | `plan`: opens or updates the release PR. `publish`: tags the approved prod SHA and creates the GitHub Release |
| Stable Docker Images | Dispatched by Stable release, manual | Builds `vX.Y.Z` images from a published release tag. Skips tags that already exist |
| Release metadata | Release PRs | Posts the `Validate release metadata` status required on `release-state` |

Release Please (pinned in `package.json`) runs through `scripts/release-state.cts` instead of the standard action. The action can't tag a branch other than the one it stores metadata on. `release-please-config.json` uses the `go` strategy, unprefixed `vX.Y.Z` tags, and `initial-version: 1.0.0`.

Workflows use the built-in `GITHUB_TOKEN`. Events created by that token don't trigger other workflows, so Stable release dispatches the validation and image workflows explicitly. Checks on automation-created PRs may show **Approve workflows to run**.

## Repository settings

**Settings → General → Pull Requests**
- Allow merge commits and squash merging.
- Set the squash commit message default to **Pull request title**.
- Turn off automatic head-branch deletion, because `main` is the promotion PR's head.

**Settings → Actions → General**
- Enable **Allow GitHub Actions to create and approve pull requests**.

**Settings → Actions → Policies**
- Allow `pull_request_target` and `workflow_dispatch` for `release-please.yml` and `release-state-check.yml`.
- Allow `push` for `release-please.yml`.

**Settings → Rules → Rulesets**

| Target | Rules |
|---|---|
| `main` | PR, 1 approval, dismiss stale approvals, resolve conversations, no force push or deletion. Checks: `Run central unit tests`, `Run edge unit tests`, `Type-check, compile and run UI tests`, `Edge to Central backup and recovery`, `Validate Conventional Commit PR title` |
| `prod` | Same as `main`, without the title check, plus `Validate production PR source`. Require up-to-date branches. Allow merge commits and don't require linear history |
| `release-state` | PR, 1 approval, dismiss stale approvals, resolve conversations, no force push or deletion, up to date. Check: `Validate release metadata` (the commit status, not `Run release metadata validation`) |
| Tags `v*` | Restrict creation, update, and deletion, but let the release workflow create tags. Enable immutable releases if available |

Leave `release-please--branches--release-state` unprotected, because Release Please force-pushes it.

## Bootstrapping

1. Create `prod` from `main`.
2. Apply the settings above.
3. Run **Stable release** on `prod` with `plan`. This creates the orphan `release-state` branch and the first `1.0.0` release PR. `prod` needs at least one `feat`, `fix`, `perf`, `revert`, or `!` commit for this to work.
4. Protect `release-state`, then review and merge the release PR.

## Failed publishes

- **Tag or GitHub Release missing:** run **Stable release** on `prod` with `publish`. It completes the approved release without moving existing tags.
- **Image missing:** run **Stable Docker Images** on `prod` with the release tag. Images that already exist are skipped.

Never move or reuse a version tag.

## Recovering `release-state`

The branch is an orphan containing exactly three files:

- `.release-please-manifest.json`, for example `{".": "1.0.0"}`
- `CHANGELOG.md`
- `release.json`, which records `version`, `tag`, `prodSha`, `previousTag`, `previousSha`, and `notes`. It is `null` before the first release.

If the branch is deleted:

1. Disable **Stable release** first. Running `plan` against an empty branch would propose `v1.0.0` again.
2. Find the last approved state. Use the `merge_commit_sha` of the most recently merged release PR, or a local clone that still has it. Don't use a version tag, because tags point at prod code.
3. Recreate the branch without overwriting anything:
   ```sh
   git push --force-with-lease=refs/heads/release-state: origin <SHA>:refs/heads/release-state
   ```
4. Reapply the ruleset, then re-enable the workflow. Run `publish` if that release was never published. Otherwise run `plan`.

If the commit is gone, rebuild the three files by hand from the latest published GitHub Release. Use its tag, its commit SHA, and the previous release's tag and SHA. Commit them on an orphan branch and push them the same way.
