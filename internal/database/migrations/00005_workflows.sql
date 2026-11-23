-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE workflows (
    id            INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name          VARCHAR(255)  NOT NULL UNIQUE,
    description   TEXT          NULL,
    input_script  TEXT          NOT NULL,
    output_script TEXT          NOT NULL,
    test_input    TEXT          NOT NULL,
    draft         TEXT          NULL,
    version       INT           NOT NULL DEFAULT 0,
    created_at    TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE workflows;
