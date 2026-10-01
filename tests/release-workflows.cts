const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {resolve} = require('node:path');
const AsyncFunction = Object.getPrototypeOf(async function() {}).constructor;

function scripts(name: string): string[] {
  const source = readFileSync(resolve(__dirname, '..', '.github/workflows', name + '.yml'), 'utf8');
  return [...source.matchAll(/script: \|\r?\n((?: {12}[^\r\n]*(?:\r?\n|$))*)/g)]
    .map((match: any) => match[1].replace(/^ {12}/gm, ''));
}

const repo = {owner: 'example', repo: 'backup'};

test('release automation dispatches metadata validation and images using trusted prod workflows', async () => {
  for (const operation of ['plan', 'publish']) {
    const calls: any[] = [];
    const github = {rest: {
      repos: {getContent: async (args: any) => {
        assert.equal(args.ref, 'approved-merge-sha');
        return {data: {content: Buffer.from('{"tag":"v1.2.3"}').toString('base64')}};
      }},
      pulls: {list: async (args: any) => {
        assert.equal(args.base, 'release-state');
        assert.equal(args.head, 'example:release-please--branches--release-state');
        return {data: [{number: 42}]};
      }},
      actions: {createWorkflowDispatch: async (args: any) => calls.push(args)}
    }};
    await new AsyncFunction('github', 'context', 'process', 'Buffer', scripts('release-please')[0])(
      github, {repo}, {env: {RELEASE_OPERATION: operation, RELEASE_STATE_REF: 'approved-merge-sha'}}, Buffer
    );
    assert.deepEqual(calls, [
      ...(operation === 'publish' ? [{...repo, workflow_id: 'stable-docker-images.yml', ref: 'prod', inputs: {tag: 'v1.2.3'}}] : []),
      {...repo, workflow_id: 'release-state-check.yml', ref: 'prod', inputs: {pr: '42'}}
    ]);
  }
});

test('metadata dispatch resolves only open same-repository automation PRs into release-state', async () => {
  const valid = {state: 'open', base: {ref: 'release-state'}, head: {
    sha: 'metadata-head', ref: 'release-please--branches--release-state', repo: {full_name: 'example/backup'}
  }};
  const run = new AsyncFunction('github', 'context', 'process', 'core', scripts('release-state-check')[0]);
  for (const pr of [valid, {...valid, state: 'closed'}, {...valid, base: {ref: 'prod'}},
    {...valid, head: {...valid.head, ref: 'main'}},
    {...valid, head: {...valid.head, repo: {full_name: 'fork/backup'}}}]) {
    const outputs: any[] = [];
    const invoke = () => run({rest: {pulls: {get: async (args: any) => {
      assert.equal(args.pull_number, 42);
      return {data: pr};
    }}}}, {repo}, {env: {PR_NUMBER: '42'}}, {setOutput: (...args: any[]) => outputs.push(args)});
    if (pr === valid) {
      await invoke();
      assert.deepEqual(outputs, [['sha', 'metadata-head']]);
    } else {
      await assert.rejects(invoke(), /Expected an open same-repository/);
      assert.deepEqual(outputs, []);
    }
  }
});

test('metadata status reports pending and validation outcomes on the resolved PR head', async () => {
  const workflowScripts = scripts('release-state-check');
  for (const [index, result, expected] of [[1, 'success', 'pending'], [2, 'success', 'success'],
    [2, 'failure', 'failure'], [2, 'cancelled', 'failure']]) {
    const calls: any[] = [];
    await new AsyncFunction('github', 'context', 'process', workflowScripts[index as number])(
      {rest: {repos: {createCommitStatus: async (args: any) => calls.push(args)}}},
      {repo, sha: 'base-commit', serverUrl: 'https://github.com', runId: 12},
      {env: {PR_HEAD_SHA: 'metadata-head', VALIDATION_RESULT: result}}
    );
    assert.deepEqual(calls, [{...repo, sha: 'metadata-head', context: 'Validate release metadata',
      state: expected, target_url: 'https://github.com/example/backup/actions/runs/12'}]);
  }
});
