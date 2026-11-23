-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE diagrams ADD COLUMN params JSON NULL AFTER description;

-- +goose Down
ALTER TABLE diagrams DROP COLUMN params;
