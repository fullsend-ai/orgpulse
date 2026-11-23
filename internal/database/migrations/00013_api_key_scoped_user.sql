-- +goose Up
ALTER TABLE api_keys ADD COLUMN scoped_user VARCHAR(255) NULL AFTER is_admin;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN scoped_user;
