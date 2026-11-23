-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE workflow_queries (
    id            INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    workflow_id   INT           NOT NULL,
    stage         INT           NOT NULL DEFAULT 0,
    sort_order    INT           NOT NULL DEFAULT 0,
    label         VARCHAR(255)  NOT NULL,
    sql_text      TEXT          NOT NULL,
    param_keys    JSON          NULL,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE workflow_queries;
