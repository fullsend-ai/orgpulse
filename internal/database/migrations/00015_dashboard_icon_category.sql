-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE dashboards
    ADD COLUMN icon     VARCHAR(100) NULL AFTER jira_project,
    ADD COLUMN category VARCHAR(255) NULL AFTER icon;

-- +goose Down
ALTER TABLE dashboards DROP COLUMN category;
ALTER TABLE dashboards DROP COLUMN icon;
