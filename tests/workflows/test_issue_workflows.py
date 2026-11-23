import datetime as dt
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
EXPORTS = sorted((ROOT / 'workflows/issues').glob('*.json'))


def evaluate(items):
    return json.loads(subprocess.check_output(['node', str(Path(__file__).with_name('evaluate.cjs'))], input=json.dumps(items).encode()))


def timestamp(value):
    return int(dt.datetime.fromisoformat(value).replace(tzinfo=dt.timezone.utc).timestamp()) if value else None


def fixture_date(day):
    return dt.datetime.fromtimestamp((day + 1) * 86400, dt.timezone.utc).strftime('%Y-%m-%d %H:%M:%S')


def database():
    db = sqlite3.connect(':memory:')
    db.row_factory = sqlite3.Row
    db.create_collation('utf8mb4_unicode_ci', lambda a, b: (a.casefold() > b.casefold()) - (a.casefold() < b.casefold()))
    db.execute("ATTACH DATABASE ':memory:' AS devlake")
    db.create_function('UNIX_TIMESTAMP', 1, timestamp)
    db.create_function('FROM_UNIXTIME', 1, lambda x: dt.datetime.fromtimestamp(x, dt.timezone.utc).strftime('%Y-%m-%d %H:%M:%S'))
    db.create_function('SUBSTRING_INDEX', 3, lambda s, sep, count: sep.join(s.split(sep)[:count] if count > 0 else s.split(sep)[count:]) if s else None)
    db.create_function('CONCAT', -1, lambda *values: ''.join(str(v) for v in values))
    db.create_function('REGEXP', 2, lambda pattern, value: bool(re.search(pattern, value or '')))
    db.executescript('''
      CREATE TABLE devlake.issues (
        id TEXT, issue_key TEXT, original_project TEXT, original_type TEXT,
        created_date TEXT, updated_date TEXT, resolution_date TEXT, status TEXT,
        lead_time_minutes REAL, url TEXT, type TEXT, epic_key TEXT, title TEXT,
        description TEXT, priority TEXT, creator_name TEXT, assignee_name TEXT,
        severity TEXT, component TEXT, urgency TEXT, is_subtask INT,
        due_date TEXT, fix_versions TEXT, story_point REAL
      );
      CREATE TABLE devlake.boards (id TEXT, name TEXT);
      CREATE TABLE devlake.board_issues (issue_id TEXT, board_id TEXT);
      CREATE TABLE devlake.repos (id TEXT, name TEXT);
      CREATE TABLE devlake.pull_requests (id TEXT, base_repo_id TEXT, url TEXT, title TEXT, closed_date TEXT, is_draft INT, author_name TEXT);
      CREATE TABLE devlake._tool_jira_issues (issue_id TEXT, issue_key TEXT, connection_id INT, resolution_date TEXT);
      CREATE TABLE devlake._tool_jira_remotelinks (issue_id TEXT, connection_id INT, url TEXT);
      CREATE TABLE devlake.issue_labels (issue_id TEXT, label_name TEXT);
      CREATE TABLE devlake._tool_jira_issue_changelogs (
        connection_id INT, issue_id INT, changelog_id INT, created TEXT);
      CREATE TABLE devlake._tool_jira_issue_changelog_items (
        connection_id INT, changelog_id INT, field TEXT, from_string TEXT, to_string TEXT);
      CREATE TABLE devlake.issue_changelogs (issue_id TEXT, original_from_value TEXT,
        original_to_value TEXT, from_value TEXT, to_value TEXT, created_date TEXT, field_name TEXT);
    ''')
    rows = [
        ('PROJECTA-1', 'Project A', fixture_date(1), fixture_date(3), 'DONE'),
        ('PROJECTB-1', 'Project B', fixture_date(2), fixture_date(3), 'DONE'),
        ('PROJECTB-2', 'Project B', fixture_date(2), fixture_date(4), 'DONE'),
        ('PROJECTA-2', 'Project A', fixture_date(2), None, 'TODO'),
        # A DONE issue resolved after the interval is excluded from the population.
        ('PROJECTA-3', 'Project A', fixture_date(17), fixture_date(20), 'DONE'),
        ('PROJECTC-1', 'Project C', fixture_date(1), fixture_date(3), 'DONE'),
        ('PROJECTC-2', 'Project C', fixture_date(2), None, 'IN_PROGRESS'),
    ]
    for key, project, created, resolved, status in rows:
        db.execute('INSERT INTO devlake.issues (id,issue_key,original_project,created_date,resolution_date,status,story_point,type,original_type,title,url,component,assignee_name) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)',
                   (key,key,project,created,resolved,status,3,'TASK','Task',key,'https://jira/'+key,'component','test-user'))
        board = 'jira:JiraBoard:1:' + {'PROJECTA':'10', 'PROJECTB':'20', 'PROJECTC':'30'}[key.split('-')[0]]
        db.execute('INSERT INTO devlake.board_issues VALUES (?,?)', (key,board))
        db.execute('INSERT INTO devlake.boards SELECT ?,? WHERE NOT EXISTS (SELECT 1 FROM devlake.boards WHERE id=?)', (board,project,board))
    for prefix in ['PROJECTA', 'PROJECTB', 'PROJECTC']:
        repo = 'org/' + prefix.lower()
        url = 'https://github.com/' + repo + '/pull/1'
        key = prefix + '-1'
        db.execute('INSERT INTO devlake.repos VALUES (?,?)', (repo,repo))
        db.execute('INSERT INTO devlake.pull_requests VALUES (?,?,?,?,?,?,?)', (repo,repo,url,'Fix ' + key,fixture_date(3),0,'test-user'))
        db.execute('INSERT INTO devlake._tool_jira_issues VALUES (?,?,?,?)', (key,key,1,fixture_date(3)))
        db.execute('INSERT INTO devlake._tool_jira_remotelinks VALUES (?,?,?)', (key,1,url))
    return db


def query(db, sql, scope):
    # SQLite's JSON iterator substitutes only the MySQL table-valued JSON reader.
    # The original SQL is also exercised against MySQL by the local server checks.
    sql = re.sub(r"JSON_TABLE\(:boardScopes, '\$\[\*\]' COLUMNS \(.*?\)\) AS selected_scope",
                 "(SELECT json_extract(value, '$.project_key') AS project_key, "
                 "json_extract(value, '$.project_name') AS project_name, "
                 "json_extract(value, '$.board_id') AS board_id, "
                 "json_extract(value, '$.connection_id') AS connection_id "
                 "FROM json_each(:boardScopes)) AS selected_scope", sql, flags=re.S)
    args = []
    def bind(match):
        value = scope[match.group(1)]
        values = value if isinstance(value, list) else [value]
        args.extend(values or [None])
        return ','.join('?' for _ in values) if values else '?'
    sql = re.sub(r':([A-Za-z_]\w*)', bind, sql)
    return [dict(row) for row in db.execute(sql, args)]


class IssueWorkflowTests(unittest.TestCase):
    def test_stage_raw_history_survives_missing_domain_conversion(self):
        wf = json.loads((ROOT / 'workflows/issues/workflow-issuestages.json').read_text())
        db = database()
        issue_id = 'jira:JiraIssue:3:100'
        db.execute(f"INSERT INTO devlake.issues (id,issue_key,original_project,created_date,resolution_date,status) VALUES (?, 'PROJECTC-100','Project C','{fixture_date(0)}','{fixture_date(4)}','DONE')", (issue_id,))
        for board in ['jira:JiraBoard:3:30', 'jira:JiraBoard:3:31']:
            db.execute('INSERT INTO devlake.board_issues VALUES (?,?)', (issue_id, board))
        for log_id, before, after, at in [(1,'Open','In Progress',fixture_date(1)), (2,'In Progress','Review',fixture_date(2)), (3,'Review','In Progress',fixture_date(3)), (4,'In Progress','Done',fixture_date(4))]:
            db.execute('INSERT INTO devlake._tool_jira_issue_changelogs VALUES (3,100,?,?)', (log_id,at))
            db.execute("INSERT INTO devlake._tool_jira_issue_changelog_items VALUES (3,?,'status',?,?)", (log_id,before,after))
        # Repeated extraction, an unrelated field and colliding IDs in another
        # connection must not duplicate or contaminate the status timeline.
        db.execute("INSERT INTO devlake._tool_jira_issue_changelog_items VALUES (3,2,'status','In Progress','Review')")
        db.execute("INSERT INTO devlake._tool_jira_issue_changelog_items VALUES (3,2,'priority','Low','High')")
        db.execute(f"INSERT INTO devlake._tool_jira_issue_changelogs VALUES (99,100,1,'{fixture_date(1)}')")
        db.execute("INSERT INTO devlake._tool_jira_issue_changelog_items VALUES (99,1,'status','Foreign status','Done')")
        self.assertEqual(db.execute('SELECT COUNT(*) FROM devlake.issue_changelogs').fetchone()[0], 0)
        for board, connection, expected in [('30','3',1), ('999','3',0), ('30','99',0), ('30','3',1)]:
            scope = evaluate([{'script':wf['input_script'], 'globals':{'input':{
                'projects':[{'jiraproject':'PROJECTC','boardid':board,'connectionid':connection}],
                'from':timestamp(fixture_date(0)),'to':timestamp(fixture_date(18))}}}])[0]
            results = {q['label']:query(db,q['sql_text'],scope) for q in wf['queries']}
            self.assertEqual(len(results['issues']), expected)
            self.assertEqual(len(results['changelogs']), 4 * expected)
            output = evaluate([{'script':wf['output_script'], 'globals':{'input':scope,'results':results}}])[0]
            if expected:
                self.assertEqual(output['data']['resolvedCount'], 1)
                self.assertEqual(output['data']['stages'], [
                    {'label':'In Progress','category':'IN_PROGRESS','value':48},
                    {'label':'Review','category':'IN_PROGRESS','value':24}])
            else:
                self.assertIn('error', output)
        db.close()

    def test_board_scope_excludes_other_boards_and_connections(self):
        for path in EXPORTS:
            wf = json.loads(path.read_text())
            with self.subTest(workflow=wf['name']):
                request = {'from':timestamp(fixture_date(0)), 'to':timestamp(fixture_date(18)),
                           'projects':[{'jiraproject':'PROJECTC','boardid':'30','connectionid':'1'}],
                           'owner':['org'], 'name':['projectc']}
                scope = evaluate([{'script':wf['input_script'], 'globals':{'input':request}}])[0]
                db = database()
                def run():
                    return {q['label']:query(db,q['sql_text'],scope) for q in wf['queries']}
                baseline = run()
                for key, board, connection in [('PROJECTC-900','31',1),('PROJECTC-901','30',2)]:
                    db.execute(f"INSERT INTO devlake.issues (id,issue_key,original_project,created_date,resolution_date,status,story_point,type,original_type,title,url,component,assignee_name) VALUES (?,?,?,'{fixture_date(1)}','{fixture_date(3)}','DONE',99,'TASK','Task',?,'https://jira/other','component','test-user')", (key,key,'Project C',key))
                    board_id = f'jira:JiraBoard:{connection}:{board}'
                    db.execute('INSERT INTO devlake.boards VALUES (?,?)', (board_id,'Project C'))
                    db.execute('INSERT INTO devlake.board_issues VALUES (?,?)', (key,board_id))
                    db.execute(f"INSERT INTO devlake._tool_jira_issues VALUES (?,?,?,'{fixture_date(3)}')", (key,key,connection))
                    db.execute('INSERT INTO devlake._tool_jira_remotelinks VALUES (?,?,?)', (key,connection,'https://github.com/org/projectc/pull/1'))
                # One issue can belong to two boards: never count that issue twice.
                db.execute("INSERT INTO devlake.board_issues VALUES ('PROJECTC-1','jira:JiraBoard:1:31')")
                self.assertEqual(run(), baseline)
                request['projects'] = [{'jiraproject':'PROJECTC','boardid':'999','connectionid':'1'}]
                scope = evaluate([{'script':wf['input_script'], 'globals':{'input':request}}])[0]
                for label, rows in run().items():
                    if wf['name'] == 'issuetraceability':
                        if label != 'total_prs':
                            self.assertEqual(next(iter(rows[0].values())), 0)
                    else:
                        self.assertEqual(rows, [])
                db.close()

    def test_team_scope_and_output_contract(self):
        scenarios = [
            ('projecta-key', [{'projectname':'', 'jiraproject':'PROJECTA'}], {'PROJECTA'}),
            ('projectb-key', [{'projectname':'', 'jiraproject':'PROJECTB'}], {'PROJECTB'}),
            ('projecta-name', [{'projectname':'Project A'}], {'PROJECTA'}),
            ('projectc-key', [{'projectname':'', 'jiraproject':'PROJECTC', 'boardid':'30'}], {'PROJECTC'}),
            ('projectc-name', [{'projectname':'Project C'}], {'PROJECTC'}),
            ('uncollected', [{'projectname':'', 'jiraproject':'UNCONFIGURED'}], set()),
            ('no-jira-source', [], set()),
            ('missing', None, set()),
        ]
        definitions = [json.loads(path.read_text()) for path in EXPORTS]
        for name, projects, allowed in scenarios:
            request = {'from':timestamp(fixture_date(0)), 'to':timestamp(fixture_date(18))}
            request['owner'] = ['org'] if allowed else []
            request['name'] = [next(iter(allowed)).lower()] if allowed else []
            if projects is not None:
                request['projects'] = projects
            scopes = evaluate([{'script':wf['input_script'], 'globals':{'input':request}} for wf in definitions])
            db = database()
            for wf, scope in zip(definitions, scopes):
                with self.subTest(scenario=name, workflow=wf['name']):
                    results = {q['label']:query(db,q['sql_text'],scope) for q in wf['queries']}
                    for rows in results.values():
                        for row in rows:
                            if 'issue_key' in row:
                                self.assertIn(row['issue_key'].split('-')[0], allowed)
                    output = evaluate([{'script':wf['output_script'], 'globals':{'input':scope, 'results':results}}])[0]
                    self.assertIsInstance(output, dict)
                    if name == 'missing':
                        assertion = wf['tests'][0]['assert_script']
                        self.assertTrue(evaluate([{'script':assertion, 'globals':{'output':output,'result':output,'error':None}}])[0])
                    if wf['name']=='issuetraceability':
                        expected = 1 if name in ('projecta-key','projectb-key','projectc-key') else 0
                        self.assertTrue(all(rows[0][next(iter(rows[0]))] == expected for rows in results.values()))
                        if not allowed:
                            self.assertTrue(all(m['value']=='0 / 0 (0%)' for m in output['data']['metrics']))
                    elif not allowed:
                        self.assertTrue(all(not rows for rows in results.values()))
                    if wf['name']=='issuekeymetrics':
                        metrics = {m['label']:m['value'] for m in output['data']['metrics']}
                        count = 1 if allowed in ({'PROJECTA'}, {'PROJECTC'}) else 2 if allowed=={'PROJECTB'} else 0
                        self.assertEqual(metrics['Total Resolved Issues'], count)
                        if allowed=={'PROJECTA'}:
                            self.assertEqual(metrics['Created Issues'], 2)
                            self.assertEqual(metrics['Open Issues'], 1)
                            self.assertEqual(metrics['Median Resolution Time (hours)'], 48)
                        if allowed=={'PROJECTC'}:
                            self.assertEqual(metrics['Created Issues'], 2)
                            self.assertEqual(metrics['Open Issues'], 1)
                            self.assertEqual(metrics['Median Resolution Time (hours)'], 48)
                    if wf['name']=='issuemediancycletime':
                        if allowed:
                            self.assertEqual(output['type'],'line')
                            self.assertTrue(output['data']['datasets'][0]['data'])
                        else:
                            self.assertIn('error',output)
            db.close()

if __name__ == '__main__':
    unittest.main()
