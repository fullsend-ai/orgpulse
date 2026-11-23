-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE stored_procedures
    ADD COLUMN workflow_id INT NULL AFTER id,
    ADD CONSTRAINT fk_sp_workflow FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE,
    DROP INDEX name,
    ADD UNIQUE INDEX uq_sp_workflow_name (workflow_id, name);

-- +goose Down
ALTER TABLE stored_procedures
    DROP INDEX uq_sp_workflow_name,
    DROP FOREIGN KEY fk_sp_workflow,
    DROP COLUMN workflow_id,
    ADD UNIQUE INDEX name (name);
