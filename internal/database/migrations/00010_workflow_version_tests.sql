-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE workflow_versions ADD COLUMN tests TEXT NULL AFTER queries;

-- +goose Down
ALTER TABLE workflow_versions DROP COLUMN tests;
