const assert = require('node:assert/strict');
const {test} = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {createRequire} = require('node:module');
const root = path.resolve(__dirname, '../..');
const frontendRequire = createRequire(path.join(root, 'web/patternfly/package.json'));
const ts = frontendRequire('typescript');
const source = fs.readFileSync(path.join(root, 'web/patternfly/src/charts/diagramRequest.ts'), 'utf8');
const compiled = ts.transpileModule(source, {compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;
const exportsObject = {};
vm.runInNewContext(compiled, {exports:exportsObject, Date});
const build = (...args) => JSON.parse(JSON.stringify(exportsObject.buildBody(...args)));
const dashboard = {type:'issuecycletime'};
const projecta = {id:'projecta', projectname:'', jiraproject:'PROJECTA', type:'jira'};
const projectb = {id:'projectb', projectname:'Project B', jiraproject:'PROJECTB', type:'jira'};
const repos = [{owner:'example-org', name:'sample-repo'}];
const fixtureDate = day => new Date((day + 1) * 86400000).toISOString().slice(0, 10);
const request = (projects, selected=[]) => build(dashboard, repos, [], projects, selected, 'custom', fixtureDate(0), fixtureDate(18));

test('All Projects includes only the current team sources, preserving Jira keys', () => {
  assert.deepEqual(request([projecta]).projects, [projecta]);
  assert.deepEqual(request([projectb]).projects, [projectb]);
  assert.notDeepEqual(request([projecta]), request([projectb]));
});
test('Project selection reaches chart requests and changes the cache key', () => {
  assert.deepEqual(request([projecta,projectb], ['projectb']).projects, [projectb]);
  assert.notDeepEqual(request([projecta,projectb], ['projectb']), request([projecta,projectb], ['projecta']));
});
test('Empty and obsolete selections send empty scope', () => {
  assert.deepEqual(request([]).projects, []);
  assert.deepEqual(request([projecta], ['obsolete']).projects, []);
});
test('Custom dates are UTC Unix seconds', () => {
  assert.equal(request([projecta]).from, 86400);
  assert.equal(request([projecta]).to, 19 * 86400);
});
test('PR charts still receive selected repository arrays', () => {
  const result = build({type:'prcycletime'}, [...repos, {owner:'other',name:'repo'}], ['example-org/sample-repo'], [projecta], [], 'days30', '', '');
  assert.deepEqual(result.owner, ['example-org']);
  assert.deepEqual(result.name, ['sample-repo']);
  assert.equal(result.projects, undefined);
});

test('Team sources API fields reach the chart request as Jira project scope', async () => {
  const apiSource = fs.readFileSync(path.join(root, 'web/patternfly/src/api.ts'), 'utf8');
  const apiCompiled = ts.transpileModule(apiSource, {compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;
  const apiExports = {};
  const apiRow = {id:'jira',type:'workitems',jira_connection_id:'3',projects:[{
    id:'projectc',project_name:'',jira_project:'PROJECTC',board_id:'30',type:'jira',
  }]};
  const urls = [];
  vm.runInNewContext(apiCompiled, {
    exports:apiExports,
    fetch:async url => {
      urls.push(url);
      return {ok:true,json:async()=>[apiRow]};
    },
  });
  const sources = await apiExports.fetchTeamSources('team-c');
  const body = request(sources.projects);
  assert.deepEqual(urls, ['/api/v1/teams/team-c/sources']);
  assert.deepEqual(body.projects, [{id:JSON.stringify(['team-c','jira','projectc']),projectname:'',jiraproject:'PROJECTC',boardid:'30',connectionid:'3',type:'jira'}]);
});

test('Team-local project IDs remain independently selectable across connections', async () => {
  const apiSource = fs.readFileSync(path.join(root, 'web/patternfly/src/api.ts'), 'utf8');
  const apiCompiled = ts.transpileModule(apiSource, {compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;
  const apiExports = {};
  vm.runInNewContext(apiCompiled, {
    exports:apiExports,
    fetch:async url => ({ok:true,json:async()=>[{
      id:'jira',type:'workitems',jira_connection_id:url.includes('/projectc/') ? '3' : '4',
      projects:[{id:'shared-id',project_name:'',jira_project:'PROJECTC',board_id:'30',type:'jira'}],
    }]}),
  });
  const first = await apiExports.fetchTeamSources('projectc');
  const second = await apiExports.fetchTeamSources('other');
  const projects = [...first.projects,...second.projects];
  assert.notEqual(projects[0].id, projects[1].id);
  assert.equal(request(projects).projects.length, 2);
  const selected = request(projects, [projects[1].id]).projects;
  assert.equal(selected.length, 1);
  assert.equal(selected[0].connectionid, '4');
});
