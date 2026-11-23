const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '../..');
const workflowDir = path.join(root, 'workflows/ftpr');
const workflows = ['workflow-ftprkeymetrics.json', 'workflow-ftprpassfail.json', 'workflow-ftprovertime.json']
  .map(name => JSON.parse(fs.readFileSync(path.join(workflowDir, name), 'utf8')));

const catalog = JSON.parse(fs.readFileSync(path.join(root, 'schemas/dashboard-templates.json'), 'utf8'));
const ftpr = catalog.find(dashboard => dashboard.dashboard_id === 'ftpr');
assert.deepEqual(
  ftpr.diagrams.map(diagram => diagram.endpoint),
  [
    '/workflows/production/ftprkeymetrics',
    '/workflows/production/ftprovertime',
    '/workflows/production/ftprpassfail',
  ],
);

function runInput(workflow, input) {
  return vm.runInNewContext(workflow.input_script, {input});
}

function runOutput(workflow, rows) {
  return vm.runInNewContext(workflow.output_script, {results: {ftpr_data: rows}});
}

function pr(id, overrides = {}) {
  return {
    row_type: 'pr',
    id,
    status: 'MERGED',
    merged_date: '2025-06-01T12:00:00Z',
    author_name: `human-${id}`,
    title: 'Regular change',
    description: '',
    is_draft: '0',
    head_commit_sha: `sha-${id}`,
    ...overrides,
  };
}

function pipeline(rowType, sha, pipelineName, result, finishedDate) {
  return {
    row_type: rowType,
    head_commit_sha: sha,
    pipeline_name: pipelineName,
    pipeline_id: pipelineName,
    result,
    finished_date: finishedDate,
  };
}

for (const workflow of workflows) {
  assert.equal(workflow.version, 0);
  assert.equal(workflow.queries.length, 1);
  assert.equal(workflow.queries[0].label, 'ftpr_data');
  assert.equal(workflow.tests[0].name, 'Unknown repository returns an error instead of a zero chart');
  const sql = workflow.queries[0].sql_text;
  assert.match(sql, /CONCAT\(rs\.owner, '\/', rs\.name\)/);
  assert.match(sql, /UNIX_TIMESTAMP\(pr\.merged_date\) >= rs\.from_ts/);
  assert.doesNotMatch(sql, /team:/);
  assert.doesNotMatch(sql, /blueprint/);
  assert.doesNotMatch(sql, /userwhitelist/);
  const specs = JSON.parse(runInput(workflow, {
    owner: ['acme', 'redhat'],
    name: ['one'],
    from: 10,
    to: 20,
  }).repo_specs);
  assert.deepEqual(specs, [{owner: 'acme', name: 'one', from: 10, to: 20}]);
  assert.deepEqual(JSON.parse(runInput(workflow, {}).repo_specs), []);
}

const rows = [
  pr(1, {head_commit_sha: 'pass', merged_date: '2025-06-01T12:00:00Z'}),
  pr(2, {head_commit_sha: 'fail', merged_date: '2025-06-02T12:00:00Z', is_draft: 0}),
  pr(3, {head_commit_sha: 'no-ci', merged_date: '2025-06-03T12:00:00Z', is_draft: false}),
  pr(4, {head_commit_sha: 'pass-two', merged_date: '2025-06-09T12:00:00Z'}),
  pr(1, {head_commit_sha: 'pass', merged_date: '2025-06-01T12:00:00Z'}),
  pr(5, {author_name: 'dependabot[bot]', head_commit_sha: 'bot'}),
  pr(6, {is_draft: '1', head_commit_sha: 'draft-string'}),
  pr(7, {is_draft: 1, head_commit_sha: 'draft-number'}),
  pr(9, {is_draft: true, head_commit_sha: 'draft-bool'}),
  pr(10, {is_draft: 'true', head_commit_sha: 'draft-true-string'}),
  pr(11, {author_name: 'release-bot', head_commit_sha: 'bot-suffix'}),
  pr(8, {status: 'OPEN', merged_date: null, head_commit_sha: 'open'}),
  pipeline('pipeline', 'pass', 'lint', 'FAILURE', '2025-05-01T00:00:00Z'),
  pipeline('pipeline', 'pass', 'lint', 'SUCCESS', '2025-05-02T00:00:00Z'),
  pipeline('pipeline', 'pass', 'unit', 'SUCCESS', '2025-05-02T00:00:00Z'),
  pipeline('pipeline', 'fail', 'unit', 'SUCCESS', '2025-05-03T00:00:00Z'),
  pipeline('prow', 'fail', 'e2e', 'FAILURE', '2025-05-03T00:00:00Z'),
  pipeline('pipeline', 'pass-two', 'unit', 'SUCCESS', '2025-05-04T00:00:00Z'),
  pipeline('pipeline', 'ignored', 'lifecycle', '', '2025-05-05T00:00:00Z'),
];

const key = runOutput(workflows[0], rows);
assert.equal(key.type, 'stats');
assert.deepEqual(
  Object.fromEntries(key.data.metrics.map(metric => [metric.label, metric.value])),
  {'FTPR (%)': 66.7, 'Total Merged PRs': 4, 'First-Time Passes': 2, 'CI Coverage': '3 / 4'},
);

const passFail = runOutput(workflows[1], rows);
assert.equal(passFail.type, 'doughnut');
assert.deepEqual(Array.from(passFail.data.datasets[0].data), [2, 1]);
assert.deepEqual({...passFail.summary}, {passed: 2, failed: 1, noData: 1, total: 3, ftpr: 66.7});

const trend = runOutput(workflows[2], rows);
assert.equal(trend.type, 'line');
assert.deepEqual(Array.from(trend.data.datasets[0].data), [100, 0, 100]);
assert.deepEqual(Array.from(trend.data.datasets[1].data), [100, 50, 66.7]);

const noRows = [pr(20, {status: 'OPEN', merged_date: null})];
for (const workflow of workflows) {
  assert.equal(runOutput(workflow, noRows).error, 'No merged PRs found for this time period');
}

const noCI = [pr(21, {head_commit_sha: 'missing'}), pr(22, {head_commit_sha: null})];
assert.equal(runOutput(workflows[0], noCI).error, 'Merged PRs were found, but none had usable CI results.');
assert.equal(runOutput(workflows[0], noCI).data, undefined);
assert.equal(runOutput(workflows[1], noCI).error, 'No PRs with CI pipeline data found for this time period');
assert.equal(runOutput(workflows[2], noCI).error, 'No PRs with CI data found for this time period');

console.log('FTPR workflow tests passed');
