-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE workflows ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE AFTER tests;

-- +goose Down
ALTER TABLE workflows DROP COLUMN archived;
