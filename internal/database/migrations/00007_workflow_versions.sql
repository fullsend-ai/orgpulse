-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE workflow_versions (
    id            INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    workflow_id   INT           NOT NULL,
    version       INT           NOT NULL,
    name          VARCHAR(255)  NOT NULL,
    description   TEXT          NULL,
    input_script  TEXT          NOT NULL,
    output_script TEXT          NOT NULL,
    test_input    TEXT          NOT NULL,
    queries       JSON          NOT NULL,
    publish_note  TEXT          NULL,
    production    BOOLEAN       NOT NULL DEFAULT FALSE,
    published_at  TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE,
    UNIQUE KEY uq_workflow_version (workflow_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE workflow_versions;
