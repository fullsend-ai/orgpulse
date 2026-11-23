-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE orgs ADD COLUMN auth_group VARCHAR(255) NULL AFTER name;
ALTER TABLE products ADD COLUMN auth_group VARCHAR(255) NULL AFTER contact_email;
ALTER TABLE teams ADD COLUMN auth_group VARCHAR(255) NULL AFTER contact_email;

-- +goose Down
ALTER TABLE teams DROP COLUMN auth_group;
ALTER TABLE products DROP COLUMN auth_group;
ALTER TABLE orgs DROP COLUMN auth_group;
