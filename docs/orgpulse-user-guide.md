# OrgPulse Dashboard — Team Onboarding Guide

This guide is for teams onboarding to OrgPulse dashboards. Teams can onboard to any combination of dashboard types depending on their needs.

## Table of Contents

- [Onboarding](#onboarding)
  - [Prepare Your Credentials](#prepare-your-credentials)
  - [Use the Configuration Wizard](#use-the-configuration-wizard)
  - [Verify Your Dashboard](#verify-your-dashboard)
  - [Adding Dashboards Later](#adding-dashboards-later)
- [Dashboard Controls](#dashboard-controls)
- [Metric Reference](#metric-reference)
  - [General Notes](#general-notes)
  - [PR Cycle Time](#pr-cycle-time)
  - [Issue Cycle Time](#issue-cycle-time)
  - [First Time Pass Rate](#first-time-pass-rate)
  - [AI Commit Scanner](#ai-commit-scanner)
  - [Code Coverage](#code-coverage)
  - [AISDLC](#aisdlc)
- [Troubleshooting](#troubleshooting)
- [Resources](#resources)

---

## Onboarding

Onboarding is done through the **Configuration Wizard** in the dashboard UI. The wizard handles DevLake integration automatically — you do not need to create DevLake connections, projects, blueprints, or scope configs manually. All you need is your team information and access tokens.

### Prepare Your Credentials

Before starting the wizard, create the access tokens for the data sources your team uses. Record each token's expiry date — the wizard requires it.

<details>
<summary><strong>GitHub Personal Access Token</strong></summary>

1. Go to **GitHub → Settings → Developer Settings → Personal Access Tokens**
2. Choose **Classic tokens** (recommended for public repos)
3. Required permissions:
   - `public_repo` — for public repository data
   - `read:org` — for organization data
   - `read:user` — for user data
4. Set an expiration date and record it

**Fine-grained PATs:** Some GitHub orgs require fine-grained PATs instead of classic tokens. If your org enforces this, create a fine-grained token with these permissions: Actions, Contents, Discussions, Issues, Metadata, Pull Requests. Select only the specific repos you are onboarding (do not request access to all repos). The token will need admin approval from your org.

</details>

<details>
<summary><strong>GitLab Personal Access Token</strong></summary>

1. Go to **GitLab → Avatar → Preferences**
2. Select **Personal Access Tokens** in User Settings
3. Click **Add new token**
4. Configure:
   - **Token name**
   - **Expiration date:** Set appropriate expiration
   - **Scopes:** `read_user`, `read_repository`, `read_api`

**Permission Requirements:** You need at least **Developer** permission (access level 30 or higher) on the repositories you want to ingest. Each repository must have the **Environments** feature enabled in Settings → General → Visibility.

</details>

<details>
<summary><strong>Jira API Token</strong></summary>

1. Go to https://id.atlassian.com/manage-profile/security/api-tokens
2. Click **Create API token**
3. Give it a descriptive label
4. Copy the token

Token lifetime depends on your organization's policy. Record the expiration date and renew the token before it expires.

</details>

---

### Use the Configuration Wizard

**Navigation:** Dashboard → **Settings** → **My Teams** → **Create Team**

The wizard guides you through onboarding:

1. **Organization** — Select the org and product your team belongs to.
2. **Team Info** — Enter your team name, team ID, contact email, and authorization group. The **team ID** is auto-generated from the team name as a lowercase, URL-safe slug (e.g., "My Platform Team" becomes `my-platform-team`). **Remember your team ID** — it is used in dashboard URLs and API calls. You can always find it later in **Settings → My Teams**.
3. **Connections** — Enable and configure connections for each data source:
   - **GitHub** — endpoint defaults to `https://api.github.com/`, provide your PAT
   - **GitLab** — provide your GitLab instance endpoint and PAT
   - **Jira** — provide your Jira instance endpoint, username/email, and API token
   - **Codecov** — provide your Codecov API endpoint and organization name

   For each connection, provide the **token expiry date** (or mark as "Never expires"). Click **Test Connection** to verify credentials before proceeding.
4. **Sources** — Add the specific repositories and Jira boards to track. The wizard validates that each repo exists and is accessible with your token.
5. **Jira Settings** *(appears only if Jira boards were added)* — Configure issue type mappings (Requirement, Bug, Incident) and custom field mappings (Story Points, Epic Link).
6. **Dashboards** — Select which dashboard templates to enable from the catalog. Each template comes pre-configured with documentation links, AI analytics controls, and metric descriptions.
7. **Configuration** — Add optional settings such as sprint configuration (`sprintstart`, `sprintstartnumber`, `sprintduration`) or user whitelist.

When you click **Create**, the system automatically:

- Creates the team and links it to the selected product
- Creates DevLake connections for each enabled plugin
- Saves sources, dashboards, and configuration
- Triggers blueprint sync and initial data collection in the background

Initial data sync can take some time depending on the number and size of repositories.

#### Scope Deduplication

A repository or Jira project can only be **collected** by one team's blueprint. If a scope is already tracked by another team, collection will be **skipped** for your team — the wizard reports which scopes were skipped and which team owns them.

Skipping only affects data collection, not data access. Your team's workflows can still query and display data from any repository in DevLake, regardless of which team's blueprint collects it. Just select the repo in your sources and your dashboards will use it.

#### Forked Repositories

DevLake's scope search does not list **forked repositories** by default. Contact your OrgPulse administrator if your team needs to track a fork.

#### Optional: User Whitelist

If your team contributes to shared repositories where not all activity belongs to your team, you can scope metrics to specific contributors via the API:

```
PUT /api/v1/teams/{teamId}/sources
```

```json
{
  "id": "gitrepos",
  "type": "gitrepos",
  "repos": [
    {
      "id": "shared-repo",
      "type": "github",
      "owner": "myorg",
      "name": "shared-repo",
      "whitelist": ["github-user1", "github-user2"]
    }
  ]
}
```

There is currently no UI for editing the whitelist. Contact your OrgPulse administrator if you need help.

---

### Verify Your Dashboard

After the initial sync completes:

1. Navigate to your dashboard URL
2. Select your org → product → team
3. Verify that each dashboard tab shows data

Or use a direct deep link:

```
https://YOUR_DASHBOARD_URL/#/?org=YOUR_ORG&product=YOUR_PRODUCT&team=YOUR_TEAM&tab=prcycletime
```

If charts show "No data available," try a wider time range (Last 60 days) or wait for the initial sync to complete.

---

### Adding Dashboards Later

You can add dashboard types after initial onboarding through **Settings → My Teams → your team**:

1. Add the required data source (e.g. Jira connection for Issue Cycle Time)
2. Enable the new dashboard type from the catalog
3. Sync to start data collection

---

## Dashboard Controls

**Time Range** — The dropdown offers Last 30 days (default), Last 60 days, sprint options (if configured), and custom date range. When no data exists for the selected range, charts display "No data available for this time period."

**Metric Card Controls** — Each metric card includes:

- **Documentation link** (`?` icon) — Opens the documentation URL configured for that metric, if one is available
- **Actions pill** — Opens AI-powered insights and analysis for the metric
- **Source Data pill** — Where available, shows the underlying data (e.g. individual PRs, issues)
- **Expand** — Opens the metric in full-screen view
- **Reload** — Refreshes the metric data

**Data Freshness** — The "Last data collection (UTC)" timestamp at the top shows how current your data is. Collection runs automatically on a daily schedule.

---

## Metric Reference

The following sections describe each metric available on the dashboard, grouped by dashboard type. Click a section to expand it.

### General Notes

All metrics automatically filter bot accounts using DevLake's centralized [`accounts.is_bot`](https://github.com/konflux-ci/devlake/pull/133) flag. This excludes known bots, CI automation, and dependency update tools from your team's metrics. All PR-based metrics also exclude draft PRs.

When a **user whitelist** is configured, only contributions from listed users are included. See [User Whitelist](#optional-user-whitelist) for details.

If you notice bot activity appearing in your metrics, contact your OrgPulse administrator for assistance.

| Dashboard Type | Data Source Required |
|---------------|---------------------|
| **PR Cycle Time** | GitHub or GitLab |
| **Issue Cycle Time** | Jira |
| **First Time Pass Rate** | GitHub or GitLab (CI/CD data) |
| **AI Commit Scanner** | GitHub or GitLab |
| **Code Coverage** | Codecov |
| **AISDLC** | Jira (RFE/Strategy projects) |

---

### PR Cycle Time

<details>
<summary>Click to expand PR Cycle Time metrics</summary>

#### Data Scoping

PR Cycle Time metrics are driven by your team's GitHub or GitLab repositories. When multiple repos are configured, the REPOSITORY dropdown lets you filter to individual repos or view "All Repositories" for the combined total.

#### Metric: PR Key Metrics

This shows a set of key metrics for your PRs:

- **Throughput (total closed PRs)** — the total number of PRs that were merged or closed in the selected time period
- **Median Cycle Time (hours)** — the median time from PR creation to close, calculated from all closed PRs within the selected time period
- **Opened PRs** — the number of PRs created during the selected time period
- **Outlier PRs (>3x median)** — the count of closed PRs whose cycle time exceeds 3x the team's median cycle time. A high outlier count indicates distribution skew — a few PRs are taking disproportionately long.

When the selected time range is 30 days or less, each metric also shows a comparison against the previous period of equal length.

#### Metric: Where PRs Spend Their Time (averages)

This doughnut chart shows the percentage of average cycle time spent in each stage of the PR lifecycle. Only merged PRs are included:

- **Pickup** (Created → First Review) — average time from PR creation to the first non-bot, non-author review comment.
- **Review** (First Review → Last Approval) — average time from first review to the last approval. For Prow-enabled repositories, this is the later of the final `/approve` and `/lgtm` comments.
- **Integration** (Last Approval → Merged) — average time from the last approval to merge.

Note: This chart uses averages, not medians. Averages are more sensitive to outliers.

#### Metric: PR Productivity

This metric shows the percentage of closed PRs that were ultimately merged, compared to those closed without merging. A high percentage of non-productive work may suggest problems with team structure, collaboration, or scope complexity.

#### Metric: PR Stage Flow

This Sankey diagram shows how PRs move through their lifecycle. It tracks PRs that reached a final state (merged or closed) during the selected time period. Each bar represents one state; outgoing bars show state transfers with percentages on hover.

#### Metric: PR Cycle Time Z-Score

Instead of comparing PRs to an arbitrary time goal, this metric benchmarks each PR against your team's own recent historical performance (90-day lookback):

- **Fast** (green) — Roughly the bottom 16% of cycle times. High clarity, small scope, immediate reviewer attention.
- **Average** (gray) — The middle 68%. Expected range for your standard process.
- **Slow** (red) — The top 16%. May indicate blocked reviews, CI failures, or scope creep.

Click on a bar to see individual PRs with links, titles, z-scores, and cycle times.

#### Metric: PR Cycle Time Distribution

This chart shows the distribution of closed PRs across cycle time buckets (< 1h through > 4w), split by outcome (Merged vs Abandoned). A healthy distribution typically peaks in the shorter time ranges with a long tail. If the peak shifts rightward over time, it may signal increasing review bottlenecks.

#### Metric: Cross-Repo Comparison

A sortable table comparing PR metrics across all repositories in your team. Only visible when "All Repositories" is selected. Columns include Repository, Median Cycle Time, Opened PRs, Closed PRs, First Review, First Approval, Integration, Abandonment %, and Reviewers/PR.

</details>

---

### Issue Cycle Time

<details>
<summary>Click to expand Issue Cycle Time metrics</summary>

#### Data Scoping

Issue Cycle Time metrics are Jira-only. The issues included are determined by the Jira boards configured in your team's data sources. When a team's boards span multiple Jira projects, the PROJECT dropdown lets you filter metrics to individual projects or view "All Projects".

#### Metric: Issue Key Metrics

This shows a set of key metrics for your Jira issues:

- **Total Resolved Issues** — issues with a resolution date during the selected time period (all terminal statuses: Done, Won't Do, Duplicate, Obsolete, etc.)
- **Median Resolution Time (hours)** — the median time from issue creation to resolution
- **Created Issues** — issues created during the selected time period
- **Open Issues** — issues currently in TODO or IN_PROGRESS status
- **Story Points Completed** — total story points for all resolved issues in the time period

When the selected time range is 30 days or less, each metric also shows a comparison against the previous period.

#### Metric: Median Resolution Time (hours)

This scatter chart tracks the evolution of issue resolution time. Each dot represents the median resolution time for issues resolved on that specific day. A 7-day rolling average trend line is overlaid.

The Y-axis uses a logarithmic scale when the data range spans 10x or more, compressing extreme outliers so daily patterns remain readable.

#### Metric: Issue Z-Score (resolution time)

Each resolved issue is categorized based on how its resolution time compares to your team's historical baseline (90-day lookback):

- **Fast** (green) — Resolution time significantly below team average (z-score < -1)
- **Average** (gray) — Within the expected range (-1 ≤ z-score ≤ 1)
- **Slow** (red) — Resolution time significantly above team average (z-score > 1)

Click on a bar to see individual issues with links, titles, z-scores, and resolution times.

#### Metric: Issue Stage Breakdown

This doughnut chart shows how resolved issues spend their time across Jira workflow stages. Each slice represents a Jira status — its size shows the median time spent in that stage relative to the others.

Pre-commitment stages are excluded (New, Backlog, Open, Created, Triage, Groomed, Refinement, To Do, Draft, Planning, Rejection Pending) as are terminal statuses (Done, Closed, Resolved, etc.). Statuses are sorted by median time (biggest bottleneck first).

The center of the doughnut shows the total number of resolved issues. Hovering over a slice shows the median time.

#### Metric: Issue Throughput

This metric shows the cumulative flow of Created vs Closed issues over time. The Created line (pink) tracks the running total of issues added. The Closed line (teal) tracks all issues that were closed, regardless of resolution type.

When the gap widens, demand is outpacing capacity. When the lines converge, the team is catching up. Parallel lines mean the team is keeping pace.

#### Metric: Issue Type Breakdown

This horizontal bar chart shows the mix of resolved issues grouped by their original Jira issue type (Epic, Story, Task, Bug, Feature, Spike, Sub-task, etc.), sorted by count. It uses the original Jira issue type, not DevLake's normalised type, to preserve the actual work mix.

</details>

---

### First Time Pass Rate

<details>
<summary>Click to expand First Time Pass Rate metrics</summary>

First Time Pass Rate (FTPR) measures how often code passes all CI/CD checks on its very first commit in a pull request. A high FTPR means developers are running tests locally and writing code that passes automated gates without fix-up commits.

#### Data Scoping

FTPR uses the same GitHub and GitLab repositories configured in your team sources. Only merged, non-draft PRs authored by humans are included (see [General Notes](#general-notes) for bot and draft filtering).

For each qualifying PR, the workflow finds the first commit (the earliest commit authored on or before the PR creation date) and checks its CI pipeline results. The PR is classified as:

- **Pass** — all CI pipelines on the first commit returned SUCCESS
- **Fail** — at least one CI pipeline on the first commit returned FAILURE
- **No data** — the first commit had no CI pipeline results available

CI pipelines include both standard CI/CD pipelines (GitHub Actions, GitLab CI) and ProwCI/Tekton jobs. Pipelines named "coderabbit" or "codecov" are excluded as they are code review and coverage tools, not build/test pipelines. When multiple pipeline runs exist for the same pipeline name on the same commit, only the latest run is used.

#### Metric: FTPR Key Metrics

- **FTPR (%)** — the percentage of merged PRs (with CI data) where the first commit passed all CI pipelines. Higher is better.
- **Total Merged PRs** — the count of qualifying merged PRs in the selected time range.
- **First-Time Passes** — the raw count of PRs that passed CI on the first attempt.
- **CI Coverage** — PRs with CI pipeline data available out of total merged PRs (e.g. "45 / 52"). If significantly lower than Total Merged PRs, some repositories may not have CI pipelines configured.

#### Metric: FTPR Over Time (%)

This scatter chart tracks the weekly FTPR over the selected time period. Each green dot represents the FTPR percentage for PRs merged during that ISO week (Monday to Sunday). A blue 4-week rolling average trend line is overlaid.

The Y-axis ranges from 0% to 100%. Weeks where no PRs had CI data are omitted.

#### Metric: FTPR Pass/Fail Breakdown

This doughnut chart shows the proportion of merged PRs where the first commit passed all CI checks (green) versus those that failed at least one check (red). Only PRs with CI pipeline data are included.

- **Passed (1st attempt)** (green) — all CI pipelines on the first commit returned SUCCESS
- **Failed (1st attempt)** (red) — at least one CI pipeline returned FAILURE

The count of PRs without CI data is shown in the chart description for transparency.

</details>

---

### AI Commit Scanner

<details>
<summary>Click to expand AI Commit Scanner metrics</summary>

The AI Commit Scanner analyses git commit messages to detect AI-assisted code contributions across your repositories.

#### Commit Scoping

Only the following commits are included:

- Commits that are part of a pull request against the default branch (whether the PR was later merged or abandoned)
- Merge commits from merged PRs
- Direct commits to the default branch that are not part of any PR

#### AI Detection

The scanner examines each commit message for standardized markers that AI coding tools leave behind:

- **Co-Authored-By** — used by Claude Code, Cursor, and Copilot (e.g. `Co-Authored-By: Claude <noreply@anthropic.com>`)
- **Assisted-by** — an alternative trailer used by some tools
- **Made-with** — another alternative trailer format

For each trailer type, the scanner checks for known tool names (Claude, Cursor, Copilot, CodeRabbit) and known email domains (anthropic.com, cursor.com).

Each commit is classified using this priority:

- **AI-assisted** — the commit message contains a recognised AI co-authoring marker
- **Human** — a non-bot commit with no AI markers detected
- **Bot** — commits from known bot accounts, service accounts, and merge commits

AI-assisted bot commits (e.g. CodeRabbit) are excluded from all displayed metrics, ensuring AI% + Human% = 100% of non-bot commits.

**Bot detection:** Uses an extensive list of known automated accounts (54 patterns), matched by author name and email. This includes generic patterns (names containing `[bot]`, ending in `Bot`), named service accounts (Dependabot, Renovate, GitHub Actions, Mergify, Snyk, etc.), Red Hat ecosystem bots (red-hat-konflux, rh-tap-build-team, etc.), and merge commits.

**Important limitations:** Detection relies on commit message conventions. If a developer uses an AI tool but the tool does not leave a trailer, that commit will be classified as Human. If bot commits appear in your metrics, contact your OrgPulse administrator.

#### Metric: Commit AI Key Metrics

- **Total Non-Bot Commits** — total commits after excluding all bot, service account, and merge commits
- **Bot Commits** — the raw count of automated commits in the time period
- **AI** — percentage of non-bot commits with AI co-authoring markers
- **Human** — percentage of non-bot commits with no AI markers

#### Metric: AI Adoption Rate Trend

Daily AI adoption rate (percentage of AI-assisted commits out of all non-bot commits) with a 7-day rolling average trend line (red). Y-axis 0–100%. Days with zero non-bot commits are excluded.

#### Metric: AI Tool Usage

Doughnut chart showing the distribution of AI tools used across AI-assisted commits. Tools identified in priority order: Cursor, Claude, Copilot, CodeRabbit, Assisted-by (unknown), Made-with (unknown).

#### Metric: AI Tool Usage Trend (7-day Average)

Multi-line chart showing AI-assisted commits per tool per day, smoothed with a 7-day rolling average. Colour-coded: Cursor (blue), Claude (amber), Copilot (green), CodeRabbit (purple), Assisted-by unknown (brown), Made-with unknown (teal).

#### Metric: Developers Using AI

Doughnut chart showing the proportion of active human developers using AI tools:

- **Using AI** (blue) — developers with at least one AI-assisted commit
- **Not using AI** (grey) — developers with only human commits

Measures adoption (has this developer used AI at all?) rather than intensity.

</details>

---

### Code Coverage

<details>
<summary>Click to expand Code Coverage metrics</summary>

Code Coverage metrics track test coverage across your repositories using data from Codecov. Contact your OrgPulse administrator for Codecov onboarding.

#### Data Requirements

- **Codecov integration** configured in the onboarding wizard (organization name + API token)
- **CI pipelines** uploading coverage reports to Codecov after test runs
- `codecov.yml` in each repository for patch targets, flags, and informational settings (optional but recommended)

#### Metric: Coverage Key Metrics

- **Avg Coverage (%)** — mean overall line coverage across all tracked repositories (each repo weighted equally)
- **Avg Patch Coverage (%)** — mean coverage of new or changed code in merged PRs. A "Target" annotation indicates the configured gate from `codecov.yml`.
- **Bypass Rate (%)** — percentage of merged PRs where patch coverage fell below the configured target (default 80%). A "Show Bypassed PRs" list is available.
- **Trend** — qualitative indicator (Stable, Improving, Declining) from the slope of daily coverage values

When the time range is 30 days or less, the Bypass Rate shows the raw count (e.g. 3/4 PRs).

#### Metric: Coverage Trend (%)

Scatter chart tracking daily overall coverage percentage.

**Single-repo view** shows:

- **Overall Coverage (%)** — daily aggregate coverage (blue dots)
- **Per-flag lines** — trend lines for each test-type flag from `codecov.yml` (e.g. unit-tests, e2e-tests)
- **Patch Coverage (%)** — green dashed line for new/changed code coverage
- **Patch Goal** — horizontal reference line (green if configured in `codecov.yml`, gray dashed at 80% default)

**Multi-repo view** hides per-flag lines and shows averages only. Stale flag warnings (>7 days without data) appear in single-repo view only.

Y-axis: 0–100%. X-axis: calendar dates.

#### Metric: Line Coverage Breakdown

Doughnut chart showing the composition of all tracked lines:

- **Covered** (green) — lines fully executed by at least one test
- **Partial** (amber) — lines only partly executed (e.g. untaken branches)
- **Uncovered** (red) — lines with no test execution

Total line count is shown in the chart. Percentages are weighted by line count (larger repos contribute more).

</details>

---

### AISDLC

<details>
<summary>Click to expand AISDLC metrics</summary>

AISDLC (AI Software Development Lifecycle) metrics track the quality and progression of work items through an agentic development pipeline:

- **RFE Creator** — assesses Jira RFEs against a five-criterion rubric, assigning PASS or FAIL
- **Strategy Creator** — produces a strategy document from a source RFE, with a recommendation and quality score

Data is collected by a DevLake plugin (`aisdlc`). These metrics are not scoped to individual teams — they operate on the full set of assessed RFEs and strategies, optionally filtered by Jira project key.

Important: All verdicts and recommendations are **agent assessments**, not human approvals.

#### Data Requirements

AISDLC dashboards require team configuration entries for:

- `rfe_jira_project` — Jira project keys for RFEs (e.g. `DEMORFE`)
- `strat_jira_project` — Jira project keys for strategies (e.g. `DEMOSTRAT`)
- `rfe_station_id` — AISDLC station ID for RFE assessment (default: `rfe-creator`)
- `strategy_station_id` — AISDLC station ID for strategy creation (default: `strategy-creator`)

#### RFE Assessment Rubric

The RFE Creator scores each RFE against five criteria, each scored 0–2 (total 0–10):

| Criterion | Meaning |
|-----------|---------|
| **WHAT** | The requested capability is clearly described |
| **WHY** | The business or customer justification is stated |
| **Open to HOW** | The RFE does not prescribe the implementation |
| **Not a task** | It is a real requirement, not an engineering task |
| **Right-sized** | Its scope is appropriate for one RFE |

Verdict rule: **PASS if total ≥ 7 AND no criterion scored 0.** An RFE can score 8/10 and still FAIL if any single criterion is zero.

#### Metric: RFE Readiness Rate (Stats View)

- **RFE Readiness Rate (%)** — percentage of assessed RFEs whose latest assessment is PASS
- **RFEs Assessed** — total distinct RFEs with at least one assessment
- **Ready (PASS)** — count of RFEs whose latest assessment is PASS
- **Near-Miss Failures** — FAIL RFEs scoring ≥ 7 but blocked by exactly one criterion at 0
- **Top Blocking Criterion** — criterion most frequently scoring 0 among failures
- **Weakest Criterion** — criterion with the lowest average score across all assessed RFEs

A "Show not-ready RFEs" drill-down shows each failed RFE with its category: near_miss (score ≥ 7, one zero), blocked (multiple zeros or low score with zeros), low_score (no zeros but below 7), or rule_mismatch (anomaly).

#### Metric: RFE Blocking Criteria (Criteria View)

Horizontal bar chart showing for each rubric criterion:

- **FAILs blocked (score 0)** (red) — failed RFEs where this criterion scored 0
- **RFEs below full marks** (blue) — all assessed RFEs where this criterion scored less than maximum

#### Metric: RFE Readiness Trend (Trend View)

Line chart tracking the RFE readiness rate across data collection batches. Uses all assessment batches, not just the latest per RFE. Y-axis 0–100%. Assessments without a parseable batch time are excluded from the trend.

#### Metric: RFE → Strategy Conversion (Stats View)

- **Ready → Strategy Conversion (%)** — percentage of ready RFEs with at least one linked strategy
- **Ready RFEs with Approved Strategy (%)** — subset where at least one strategy has recommendation "approve"
- **Strategy Approve Rate (%)** — percentage of all linked strategies with "approve"
- **Mean Strategy Score, Ready RFEs (%)** — average normalized quality score of strategies linked to ready RFEs
- **Not-Ready RFEs with a Strategy** — FAIL RFEs with a linked strategy (governance signal)
- **Unmatched Strategies** — strategies that cannot be matched to the cohort (no source link, never assessed, or outside time window)

Important: The conversion rate is only as complete as strategy collection. The time filter applies only to the RFE cohort — strategies are counted regardless of when they were generated.

#### Metric: RFE → Strategy Flow (Flow View)

Sankey diagram showing the progression of RFEs:

1. **Assessed RFEs** → **Ready (PASS)** / **Not Ready (FAIL)**
2. **Ready** / **Not Ready** → **Strategy linked** / **No strategy**
3. **Strategy linked** → **Strategy approved** / **No approved strategy**

Each edge shows the count of distinct RFEs and percentage of the total assessed cohort. Zero-count edges are omitted.

#### Metric: RFE → Strategy Pairs (Pairs View)

Table showing every strategy with its linked source RFE:

- **Strategy** — the strategy Jira key
- **Source RFE** — the linked RFE key, or "—" if no source link
- **RFE Verdict** — PASS/FAIL from the latest assessment, or "—" if not in the cohort
- **RFE Score** — the assessment score (sortable by percentage)
- **Recommendation** — approve/revise/reject
- **Strategy Score** — quality score with percentage
- **Link Status** — "In cohort", "No source RFE", "Source RFE never assessed", or "Source RFE outside window"

</details>

---

## Troubleshooting

<details>
<summary><strong>Team not appearing in dashboard</strong></summary>

- Verify onboarding completed successfully in the Configuration Wizard
- Check that your team was added to a product and org
- Hard refresh the dashboard (Ctrl+Shift+R)
- Contact your OrgPulse administrator if the issue persists

</details>

<details>
<summary><strong>Charts show "No data available"</strong></summary>

This is expected when there are no closed PRs in the selected time range. Try a wider time range (e.g. Last 60 days). For newly onboarded teams, the initial sync may still be running — check the team settings page for sync status.

</details>

<details>
<summary><strong>Dashboard shows no data for any time range</strong></summary>

- Check the team settings page for sync errors
- Verify your access token has not expired (check the token expiry badge)
- Ensure repository names in your data sources match exactly (case-sensitive)
- Try triggering a manual sync from team settings

</details>

<details>
<summary><strong>Repository not appearing during onboarding</strong></summary>

- **Forked repos** may not appear in DevLake's scope search — contact your OrgPulse administrator
- Verify that the PAT has `read:org` scope and that the PAT owner has at least read access to the repo/org

</details>

<details>
<summary><strong>Repository already tracked by another team</strong></summary>

A scope can only be collected by one team's blueprint. If a repo you need is already tracked, collection will be reported as "skipped" during sync. This is expected — just add the repo to your sources and your dashboards will use the data. You don't need to move ownership.

</details>

<details>
<summary><strong>Token expired</strong></summary>

| Token Type | Typical Expiry | How to Renew |
|-----------|----------------|--------------|
| GitHub Classic PAT | Set by user | GitHub → Settings → Developer Settings → PATs |
| GitHub Fine-Grained PAT | Set by user | GitHub → Settings → Developer Settings → Fine-Grained PATs |
| GitLab PAT | Max 1 year | GitLab → User Settings → Access Tokens |
| Jira API Token | Depends on your organization's policy | https://id.atlassian.com/manage-profile/security/api-tokens |

After generating a new token, update it in team settings using **Update Token**, or via the API:

```
PUT /api/v1/devlake/teams/{teamId}/connections/{plugin}
{"token": "new_token", "expiry_date": "YYYY-MM-DD"}
```

</details>

<details>
<summary><strong>Fine-grained PAT issues</strong></summary>

The dashboard automatically configures `enableGraphql: false` on the DevLake connection for fine-grained tokens. If data collection still fails, contact your OrgPulse administrator.

</details>

<details>
<summary><strong>Large repository CI/CD pagination limit</strong></summary>

GitHub's API returns HTTP 422 when paginating beyond ~1,000 pages of CI/CD data. This affects repos with 100,000+ workflow runs. Contact your OrgPulse administrator to exclude CI/CD collection for affected repos. This does not affect PR metrics — only FTPR dashboards require CI/CD data.

</details>

---

## Resources

- [DevLake integration guide](devlake-integration.md)
- [REST API reference](rest-api.md)
- [Workflow documentation](workflows.md)
