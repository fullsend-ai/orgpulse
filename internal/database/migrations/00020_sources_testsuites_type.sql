-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE sources MODIFY COLUMN type ENUM('gitrepos', 'workitems', 'testsuites') NOT NULL;

-- +goose Down
ALTER TABLE sources MODIFY COLUMN type ENUM('gitrepos', 'workitems') NOT NULL;
