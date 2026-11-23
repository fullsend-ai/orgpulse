-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE canned_scripts (
    id          INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(255)  NOT NULL,
    description TEXT          NULL,
    category    VARCHAR(255)  NOT NULL DEFAULT '',
    js_text     TEXT          NOT NULL,
    version     INT           NOT NULL DEFAULT 1,
    created_at  TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE canned_scripts;
