-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE entity_config MODIFY COLUMN entity_type ENUM('org', 'product', 'team', 'user') NOT NULL;

-- +goose Down
ALTER TABLE entity_config MODIFY COLUMN entity_type ENUM('org', 'product', 'team') NOT NULL;
