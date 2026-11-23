-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE workitem_projects ADD COLUMN board_id VARCHAR(50) NULL AFTER jira_project;

-- +goose Down
ALTER TABLE workitem_projects DROP COLUMN board_id;
