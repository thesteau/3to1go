// Node 24 executes this TypeScript directly. Release Please owns versioning and
// changelog generation; this bridge stores those updates off the code branches.
const {execFileSync} = require('node:child_process');
const {readFileSync} = require('node:fs');
const {GitHub} = require('release-please');
const {buildStrategy} = require('release-please/build/src/factory');
const {parseConventionalCommits} = require('release-please/build/src/commit');
const {Version} = require('release-please/build/src/version');
const {TagName} = require('release-please/build/src/util/tag-name');

const STATE_BRANCH = 'release-state';
const PR_BRANCH = 'release-please--branches--release-state';
const STATE_FILES = ['.release-please-manifest.json', 'CHANGELOG.md', 'release.json'];
const SEMVER = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const SHA = /^[a-f0-9]{40}$/;

type ReleaseState = {
  version: string;
  tag: string;
  prodSha: string;
  previousTag: string | null;
  previousSha: string | null;
  notes: string;
};

function invariant(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}

function validateState(state: ReleaseState): void {
  invariant(state && SEMVER.test(state.version), 'Invalid stable version');
  invariant(state.tag === `v${state.version}`, 'Version/tag mismatch');
  invariant(SHA.test(state.prodSha), 'Invalid production SHA');
  invariant(typeof state.notes === 'string' && state.notes.trim(), 'Release notes are required');
  invariant((state.previousTag === null) === (state.previousSha === null), 'Incomplete previous release');
  if (state.previousTag !== null) {
    invariant(typeof state.previousTag === 'string' && state.previousTag.startsWith('v') &&
      SEMVER.test(state.previousTag.slice(1)) && SHA.test(state.previousSha!), 'Invalid previous release');
    const old = state.previousTag.slice(1).split('.').map(Number);
    const next = state.version.split('.').map(Number);
    const difference = next.map((value, index) => value - old[index]).find(value => value !== 0);
    invariant(difference !== undefined && difference > 0, 'Version must increase');
  } else {
    invariant(state.version === '1.0.0', 'First stable release must be v1.0.0');
  }
}

function validateTree(tree: any[]): void {
  invariant(tree.length === STATE_FILES.length && new Set(tree.map(entry => entry.path)).size === STATE_FILES.length && tree.every(entry =>
    STATE_FILES.includes(entry.path) && entry.type === 'blob' && entry.mode === '100644'),
  'release-state must contain only the three regular metadata files');
}

function git(...args: string[]): string {
  return execFileSync('git', args, {
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe']
  }).trim();
}

function commitsBetween(prodSha: string, previousSha: string | null): any[] {
  invariant(SHA.test(prodSha) && (!previousSha || SHA.test(previousSha)), 'Invalid history boundary');
  git('merge-base', '--is-ancestor', prodSha, 'origin/prod');
  if (previousSha) git('merge-base', '--is-ancestor', previousSha, prodSha);
  const range = previousSha ? `${previousSha}..${prodSha}` : prodSha;
  const fields = git('log', '--format=%H%x00%B%x00', range).split('\0');
  const commits = [];
  for (let index = 0; index + 1 < fields.length; index += 2) {
    commits.push({sha: fields[index].trim(), message: fields[index + 1].trim()});
  }
  return commits;
}

async function buildCandidate(github: any, prodSha: string, previous: ReleaseState | null): Promise<any> {
  const config = JSON.parse(readFileSync('release-please-config.json', 'utf8'));
  invariant(config['release-type'] === 'go' && config['initial-version'] === '1.0.0' &&
    config['include-component-in-tag'] === false && config['include-v-in-tag'] === true,
  'Unsupported bridge configuration');
  const strategy = await buildStrategy({
    github, targetBranch: 'prod', path: '.', releaseType: 'go',
    packageName: '3to1go', initialVersion: '1.0.0',
    includeComponentInTag: false, includeVInTag: true
  });
  const latest = previous ? {
    tag: new TagName(Version.parse(previous.version)), sha: previous.prodSha, notes: previous.notes
  } : undefined;
  return strategy.buildReleasePullRequest(
    parseConventionalCommits(commitsBetween(prodSha, previous?.prodSha ?? null)), latest
  );
}

async function optional(call: () => Promise<any>): Promise<any | null> {
  try { return (await call()).data; }
  catch (error: any) { if (error.status === 404) return null; throw error; }
}

async function readState(api: any, repo: any, ref: string): Promise<ReleaseState | null> {
  const response = await api.git.getTree({...repo, tree_sha: ref, recursive: 'true'});
  invariant(!response.data.truncated, 'Metadata tree is truncated');
  validateTree(response.data.tree);
  async function contents(path: string): Promise<string> {
    const {data} = await api.repos.getContent({...repo, path, ref});
    invariant(data.type === 'file' && data.encoding === 'base64', 'Invalid metadata file');
    return Buffer.from(data.content, 'base64').toString('utf8');
  }
  const state = JSON.parse(await contents('release.json'));
  const manifest = JSON.parse(await contents('.release-please-manifest.json'));
  if (state === null) {
    invariant(Object.keys(manifest).length === 0, 'Bootstrap manifest must be empty');
  } else {
    validateState(state);
    invariant(Object.keys(manifest).length === 1 && manifest['.'] === state.version, 'Manifest mismatch');
    invariant((await contents('CHANGELOG.md')).includes(state.version), 'Changelog mismatch');
  }
  return state;
}

async function bootstrap(api: any, repo: any): Promise<void> {
  if (await optional(() => api.git.getRef({...repo, ref: `heads/${STATE_BRANCH}`}))) return;
  const files: Record<string, string> = {
    '.release-please-manifest.json': '{}\n',
    'CHANGELOG.md': '# Changelog\n',
    'release.json': 'null\n'
  };
  const {data: tree} = await api.git.createTree({...repo, tree: Object.entries(files).map(([path, content]) =>
    ({path, content, mode: '100644', type: 'blob'}))});
  // An orphan commit: no application files or application history on this branch.
  const {data: commit} = await api.git.createCommit({
    ...repo, message: 'chore: initialize release metadata', tree: tree.sha, parents: []
  });
  await api.git.createRef({...repo, ref: `refs/heads/${STATE_BRANCH}`, sha: commit.sha});
}

async function published(api: any, repo: any, state: ReleaseState): Promise<boolean> {
  const release = await optional(() => api.repos.getReleaseByTag({...repo, tag: state.tag}));
  const ref = await optional(() => api.git.getRef({...repo, ref: `tags/${state.tag}`}));
  if (ref) invariant(ref.object.type === 'commit' && ref.object.sha === state.prodSha,
    'Existing tag points to a different commit; refusing to reuse it');
  if (!release) return false;
  invariant(ref && !release.draft && !release.prerelease && release.body === state.notes &&
    (release.target_commitish === state.prodSha || release.target_commitish === 'prod'),
  'Existing release does not match approved metadata');
  return true;
}

async function verifyCandidate(github: any, api: any, repo: any, state: ReleaseState): Promise<void> {
  validateState(state);
  let previous = null;
  if (state.previousTag) {
    const {data: release} = await api.repos.getReleaseByTag({...repo, tag: state.previousTag});
    previous = {version: state.previousTag.slice(1), tag: state.previousTag,
      prodSha: state.previousSha!, notes: release.body, previousTag: null, previousSha: null};
    invariant(await published(api, repo, previous), 'Previous release must be published');
  }
  const candidate = await buildCandidate(github, state.prodSha, previous);
  // Release Please dates the heading when it generates notes. Approval/retries
  // may happen on another day; preserve the reviewed date and compare the rest.
  const withoutHeadingDate = (notes: string) => notes.replace(/^(.*) \(\d{4}-\d{2}-\d{2}\)(\r?\n|$)/, '$1$2');
  invariant(candidate && candidate.version.toString() === state.version &&
    withoutHeadingDate(candidate.body.notes()) === withoutHeadingDate(state.notes),
    'Approved metadata does not match Release Please calculation for the production commit');
}

function jsonUpdate(path: string, data: unknown): any {
  return {path, createIfMissing: true, updater: {updateContent: () => JSON.stringify(data, null, 2) + '\n'}};
}

async function plan(github: any, api: any, repo: any): Promise<void> {
  await bootstrap(api, repo);
  const previous = await readState(api, repo, STATE_BRANCH);
  invariant(!previous || await published(api, repo, previous),
    'Approved release is not published yet. Retry publish before planning another release.');
  const prodSha = git('rev-parse', 'origin/prod');
  const candidate = await buildCandidate(github, prodSha, previous);
  if (!candidate) { console.log('No releasable production changes.'); return; }
  const state: ReleaseState = {
    version: candidate.version.toString(), tag: `v${candidate.version}`, prodSha,
    previousTag: previous?.tag ?? null, previousSha: previous?.prodSha ?? null,
    notes: candidate.body.notes()
  };
  validateState(state);
  invariant(candidate.updates.every((update: any) => update.path === 'CHANGELOG.md'), 'Unexpected strategy updates');
  await github.createPullRequest({
    headBranchName: PR_BRANCH, baseBranchName: STATE_BRANCH, number: 0,
    title: `chore(release-state): release ${state.version}`,
    body: candidate.body.toString() + `\n\nProduction commit: \`${prodSha}\`.\nMerging approves tagging this exact production commit. No code is merged into prod.`,
    labels: [], files: STATE_FILES
  }, STATE_BRANCH, `chore: prepare ${state.tag}`, [
    ...candidate.updates, jsonUpdate('.release-please-manifest.json', {'.': state.version}),
    jsonUpdate('release.json', state)
  ]);
  console.log(`Prepared ${state.tag} at ${prodSha} on ${STATE_BRANCH}.`);
}

async function publish(github: any, api: any, repo: any, ref: string): Promise<void> {
  const state = await readState(api, repo, ref);
  invariant(state, 'No approved release metadata');
  const current = await readState(api, repo, STATE_BRANCH);
  invariant(JSON.stringify(state) === JSON.stringify(current), 'Release metadata has been superseded');
  await verifyCandidate(github, api, repo, state);
  if (!await published(api, repo, state)) {
    const tag = await optional(() => api.git.getRef({...repo, ref: `tags/${state.tag}`}));
    if (!tag) await api.git.createRef({...repo, ref: `refs/tags/${state.tag}`, sha: state.prodSha});
    await github.createRelease({
      name: state.tag, tag: new TagName(Version.parse(state.version)), sha: state.prodSha, notes: state.notes
    });
    console.log(`Published ${state.tag} at approved production commit ${state.prodSha}.`);
  }
  // A prod push could have arrived while the metadata PR was awaiting approval.
  await plan(github, api, repo);
}

async function main(): Promise<void> {
  const [owner, repoName] = (process.env.GITHUB_REPOSITORY ?? '').split('/');
  invariant(owner && repoName && process.env.RELEASE_PLEASE_TOKEN, 'Repository and release token are required');
  const github = await GitHub.create({owner, repo: repoName, defaultBranch: 'prod', token: process.env.RELEASE_PLEASE_TOKEN});
  const api = github.getGitHubApi().octokit;
  const repo = {owner, repo: repoName};
  git('fetch', 'origin', 'prod');
  const operation = process.env.RELEASE_OPERATION;
  if (operation === 'plan') await plan(github, api, repo);
  else if (operation === 'publish') await publish(github, api, repo, process.env.RELEASE_STATE_REF ?? STATE_BRANCH);
  else if (operation === 'check') {
    const state = await readState(api, repo, process.env.RELEASE_STATE_REF!);
    invariant(state, 'Release PR must propose a release');
    const base = await readState(api, repo, STATE_BRANCH);
    invariant(state.previousTag === (base?.tag ?? null) && state.previousSha === (base?.prodSha ?? null),
      'Release proposal must follow the current release-state');
    await verifyCandidate(github, api, repo, state);
    console.log('Metadata tree, version, notes, and production commit validated.');
  } else throw new Error('Unknown release operation');
}

module.exports = {validateState, validateTree, commitsBetween, buildCandidate, readState, bootstrap, published, plan, publish};
if (require.main === module) main().catch((error: Error) => { console.error(error.message); process.exitCode = 1; });
