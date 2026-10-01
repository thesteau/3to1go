const {test} = require('node:test');
const assert = require('node:assert/strict');
const {execFileSync} = require('node:child_process');
const {mkdtempSync, writeFileSync, rmSync} = require('node:fs');
const {tmpdir} = require('node:os');
const {join, resolve, dirname, basename} = require('node:path');
const bridge = require('../scripts/release-state.cts');

const initial = {
  version: '1.0.0', tag: 'v1.0.0', prodSha: 'a'.repeat(40),
  previousTag: null, previousSha: null, notes: 'First release'
};
const files = ['.release-please-manifest.json', 'CHANGELOG.md', 'release.json'];
const tree = files.map(path => ({path, mode: '100644', type: 'blob'}));

test('release state requires a stable increasing version and exact SHA', () => {
  bridge.validateState(initial);
  bridge.validateState({...initial, version: '1.1.0', tag: 'v1.1.0', previousTag: 'v1.0.0', previousSha: 'b'.repeat(40)});
  for (const invalid of [
    {...initial, tag: 'v2.0.0'}, {...initial, prodSha: 'main'},
    {...initial, version: '1.0.0-rc.1', tag: 'v1.0.0-rc.1'},
    {...initial, version: '2.0.0', tag: 'v2.0.0'},
    {...initial, previousTag: 'v1.0.0'},
    {...initial, previousTag: 'v1.1.0', previousSha: 'b'.repeat(40)}
  ]) assert.throws(() => bridge.validateState(invalid));
});

test('metadata branch rejects application files, symlinks, and directories', () => {
  bridge.validateTree(tree);
  assert.throws(() => bridge.validateTree([...tree, {path: 'app.go', mode: '100644', type: 'blob'}]));
  assert.throws(() => bridge.validateTree(tree.map(entry => ({...entry, mode: '120000'}))));
  assert.throws(() => bridge.validateTree(tree.slice(1)));
  assert.throws(() => bridge.validateTree([tree[0], tree[0], tree[2]]));
});

test('bootstrap creates only metadata in an orphan commit and is idempotent', async () => {
  const calls: any[] = [];
  let exists = false;
  const api = {git: {
    getRef: async () => { if (!exists) throw {status: 404}; return {data: {object: {sha: 'state'}}}; },
    createTree: async (args: any) => { calls.push(args); return {data: {sha: 'tree'}}; },
    createCommit: async (args: any) => { calls.push(args); return {data: {sha: 'state'}}; },
    createRef: async (args: any) => { calls.push(args); exists = true; }
  }};
  await bridge.bootstrap(api, {});
  assert.deepEqual(calls[0].tree.map((entry: any) => entry.path).sort(), [...files].sort());
  assert.deepEqual(calls[1].parents, []);
  assert.equal(calls[2].ref, 'refs/heads/release-state');
  await bridge.bootstrap(api, {});
  assert.equal(calls.length, 3);
});

test('publication refuses an existing version tag at another commit', async () => {
  const api = {
    repos: {getReleaseByTag: async () => {throw {status: 404};}},
    git: {getRef: async () => ({data: {object: {type: 'commit', sha: 'b'.repeat(40)}}})}
  };
  await assert.rejects(bridge.published(api, {}, initial), /different commit/);
  api.git.getRef = async () => ({data: {object: {type: 'commit', sha: initial.prodSha}}});
  assert.equal(await bridge.published(api, {}, initial), false);
});

test('metadata manifest must agree with the approved version', async () => {
  const content: Record<string, string> = {
    'release.json': JSON.stringify(initial), '.release-please-manifest.json': '{".":"1.1.0"}',
    'CHANGELOG.md': '# 1.0.0'
  };
  const api = {
    git: {getTree: async () => ({data: {tree}})},
    repos: {getContent: async ({path}: any) => ({data: {
      type: 'file', encoding: 'base64', content: Buffer.from(content[path]).toString('base64')
    }})}
  };
  await assert.rejects(bridge.readState(api, {}, 'state'), /Manifest mismatch/);
  content['.release-please-manifest.json'] = '{".":"1.0.0"}';
  assert.deepEqual(await bridge.readState(api, {}, 'state'), initial);
});

test('real Release Please handles repeated promotions without release metadata on code branches', async (context: any) => {
  const folder = mkdtempSync(join(tmpdir(), '3to1go-release-'));
  const oldCwd = process.cwd();
  const config = resolve(oldCwd, 'release-please-config.json');
  const git = (...args: string[]) => execFileSync('git', ['-C', folder, ...args], {encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe']}).trim();
  const github = {repository: {owner: 'example', repo: 'backup', defaultBranch: 'prod'}};
  let approved: any = null;
  let proposed: any = null;
  const tags: Record<string, string> = {};
  const releases: Record<string, any> = {};
  const api = {
    git: {
      getRef: async ({ref}: any) => {
        if (ref === 'heads/release-state') return {data: {object: {sha: 'state'}}};
        const sha = tags[ref.replace('tags/', '')];
        if (!sha) throw {status: 404};
        return {data: {object: {type: 'commit', sha}}};
      },
      getTree: async () => ({data: {tree}}),
      createRef: async ({ref, sha}: any) => {tags[ref.replace('refs/tags/', '')] = sha;}
    },
    repos: {
      getContent: async ({path}: any) => ({data: {type: 'file', encoding: 'base64', content: Buffer.from(
        path === 'release.json' ? JSON.stringify(approved) :
        path === '.release-please-manifest.json' ? JSON.stringify(approved ? {'.': approved.version} : {}) :
        '# Changelog\n' + (approved?.version ?? '')
      ).toString('base64')}}),
      getReleaseByTag: async ({tag}: any) => {
        if (!releases[tag]) throw {status: 404};
        return {data: releases[tag]};
      }
    }
  };
  const client = {...github,
    createPullRequest: async (pr: any, base: string, message: string, updates: any[]) => {
      assert.equal(base, 'release-state');
      assert.equal(pr.headBranchName, 'release-please--branches--release-state');
      const update = updates.find(entry => entry.path === 'release.json');
      proposed = JSON.parse(update.updater.updateContent(''));
      const changelog = updates.find(entry => entry.path === 'CHANGELOG.md').updater.updateContent('# Changelog\n');
      assert.ok(changelog.includes(proposed.version));
    },
    createRelease: async (release: any) => {
      assert.equal(tags[release.tag.toString()], release.sha);
      releases[release.tag.toString()] = {body: release.notes, target_commitish: release.sha, draft: false, prerelease: false};
    }
  };
  try {
    git('init', '-b', 'main');
    git('config', 'user.name', 'Release validation');
    git('config', 'user.email', 'validation@example.invalid');
    writeFileSync(join(folder, 'app.txt'), 'first');
    git('add', '.'); git('commit', '-m', 'feat: initial application');
    git('branch', 'prod');
    // Config is automation code, not per-release metadata.
    writeFileSync(join(folder, 'release-please-config.json'), require('node:fs').readFileSync(config));
    process.chdir(folder);
    git('update-ref', 'refs/remotes/origin/prod', git('rev-parse', 'prod'));
    await bridge.plan(client, api, {});
    assert.equal(proposed.version, '1.0.0');
    approved = proposed;
    git('checkout', 'main');
    writeFileSync(join(folder, 'app.txt'), 'documentation while approval is pending');
    git('add', 'app.txt'); git('commit', '-m', 'fix: expand usage');
    git('checkout', 'prod'); git('merge', '--no-ff', 'main', '-m', 'chore: promote main to prod');
    git('update-ref', 'refs/remotes/origin/prod', git('rev-parse', 'prod'));
    assert.notEqual(approved.prodSha, git('rev-parse', 'prod'));
    context.mock.timers.enable({apis: ['Date'], now: Date.now() + 86400000});
    const createPullRequest = client.createPullRequest;
    client.createPullRequest = async () => {throw new Error('Next proposal PR creation failed');};
    // Publishing must complete independently of a failure in the next plan.
    await bridge.publish(client, api, {}, 'state');
    await assert.rejects(bridge.plan(client, api, {}), /Next proposal PR creation failed/);
    client.createPullRequest = createPullRequest;
    assert.equal(tags['v1.0.0'], approved.prodSha); // The recorded SHA, not the later prod head.
    await bridge.publish(client, api, {}, 'state'); // Retry does not move the tag or release.
    assert.equal(Object.keys(releases).length, 1);
    for (const [message, version] of [
      ['fix: repair backup', '1.0.1'], ['feat: another feature', '1.1.0'], ['feat!: breaking format change', '2.0.0']
    ]) {
      git('checkout', 'main');
      writeFileSync(join(folder, 'app.txt'), message);
      git('add', 'app.txt'); git('commit', '-m', message);
      git('checkout', 'prod'); git('merge', '--no-ff', 'main', '-m', 'chore: promote main to prod');
      git('update-ref', 'refs/remotes/origin/prod', git('rev-parse', 'prod'));
      await bridge.plan(client, api, {});
      assert.equal(proposed.version, version);
      assert.equal(proposed.prodSha, git('rev-parse', 'prod'));
      approved = proposed;
      await bridge.publish(client, api, {}, 'state');
    }
    git('checkout', 'main');
    writeFileSync(join(folder, 'app.txt'), 'docs only');
    git('add', 'app.txt'); git('commit', '-m', 'docs: explain deployments');
    git('checkout', 'prod'); git('merge', '--no-ff', 'main', '-m', 'chore: promote main to prod');
    git('update-ref', 'refs/remotes/origin/prod', git('rev-parse', 'prod'));
    assert.equal(await bridge.buildCandidate(github, git('rev-parse', 'prod'), approved), undefined);
    assert.equal(git('ls-tree', '--name-only', 'prod'), 'app.txt');
    assert.equal(git('ls-tree', '--name-only', 'main'), 'app.txt');
    assert.equal(Object.keys(releases).length, 4);
    // A wrong or non-production SHA cannot be released.
    await assert.rejects(bridge.buildCandidate(github, 'b'.repeat(40), approved));
  } finally {
    process.chdir(oldCwd);
    assert.equal(dirname(resolve(folder)), resolve(tmpdir()));
    assert.ok(basename(folder).startsWith('3to1go-release-'));
    rmSync(folder, {recursive: true, force: true});
  }
});
