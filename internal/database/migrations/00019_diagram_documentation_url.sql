-- +goose Up
ALTER TABLE diagrams ADD COLUMN documentation_url VARCHAR(512) DEFAULT NULL AFTER description;

-- Backfill documentation_url from canonical metric-explanation anchors.
UPDATE diagrams SET documentation_url = CASE endpoint
  WHEN '/workflows/production/prkeymetrics'        THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time'
  WHEN '/workflows/production/prstages'             THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time'
  WHEN '/workflows/production/prproductivity'       THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time'
  WHEN '/workflows/production/prflow'               THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time'
  WHEN '/workflows/production/przscore'             THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time'
  WHEN '/workflows/production/prcycletimescatter'   THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time'
  WHEN '/workflows/production/issuekeymetrics'      THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time'
  WHEN '/workflows/production/issuemediancycletime' THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time'
  WHEN '/workflows/production/issuezscore'          THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time'
  WHEN '/workflows/production/issuestages'          THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time'
  WHEN '/workflows/production/issuetypebreakdown'   THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time'
  WHEN '/workflows/production/issuethroughput'      THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time'
  WHEN '/workflows/production/cskeymetrics'         THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#ai-commit-scanner'
  WHEN '/workflows/production/csaitools'            THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#ai-commit-scanner'
  WHEN '/workflows/production/csdevelopers'         THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#ai-commit-scanner'
  WHEN '/workflows/production/cstrend'              THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#ai-commit-scanner'
  WHEN '/workflows/production/cstooltrend'          THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#ai-commit-scanner'
  WHEN '/workflows/production/coveragekeymetrics'   THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#code-coverage'
  WHEN '/workflows/production/coveragetrend'        THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#code-coverage'
  WHEN '/workflows/production/coveragelinebreakdown' THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#code-coverage'
  WHEN '/workflows/production/ftprkeymetrics'        THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#first-time-pass-rate'
  WHEN '/workflows/production/ftprovertime'          THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#first-time-pass-rate'
  WHEN '/workflows/production/ftprpassfail'          THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#first-time-pass-rate'
  WHEN '/workflows/production/e2eissuestraceability' THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#metric-reference'
  WHEN '/workflows/production/e2eprtraceability'    THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#metric-reference'
  WHEN '/workflows/production/e2ecoveragetrend'     THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#metric-reference'
  WHEN '/workflows/production/e2etopanomalies'      THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#metric-reference'
  WHEN '/workflows/production/aisdlcrfereadiness'    THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#aisdlc'
  WHEN '/workflows/production/aisdlcstrategyconversion' THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#aisdlc'
  WHEN '/workflows/production/arfleetscorecard'     THEN 'https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#metric-reference'
  ELSE documentation_url
END
WHERE documentation_url IS NULL;

-- Enable AI analytics on all existing diagrams.
UPDATE diagrams SET ai_analytics = TRUE WHERE ai_analytics = FALSE;

-- +goose Down
UPDATE diagrams SET ai_analytics = FALSE WHERE ai_analytics = TRUE;
ALTER TABLE diagrams DROP COLUMN documentation_url;
