-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE workflows ADD COLUMN tests TEXT NULL AFTER test_input;

-- +goose Down
ALTER TABLE workflows DROP COLUMN tests;
