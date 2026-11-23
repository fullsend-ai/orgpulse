-- +goose NO TRANSACTION
-- +goose Up
-- ============================================================
-- Dashboard Configuration Database Schema
-- MySQL 8.0+
--
-- Mirrors the JSON config from orgs.json, products.json,
-- and teams.json. Hierarchy: org -> product -> team.
-- Dashboards can be defined at any level.
-- ============================================================

-- ------------------------------------------------------------
-- Core entities
-- ------------------------------------------------------------

CREATE TABLE orgs (
    id          VARCHAR(100)  NOT NULL PRIMARY KEY,
    name        VARCHAR(255)  NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE products (
    id            VARCHAR(100)  NOT NULL PRIMARY KEY,
    name          VARCHAR(255)  NOT NULL,
    contact_email VARCHAR(255)  NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE teams (
    id              VARCHAR(100)  NOT NULL PRIMARY KEY,
    name            VARCHAR(255)  NOT NULL,
    contact_email   VARCHAR(255)  NULL,
    fetch_url       TEXT          NULL,
    fetch_check_url TEXT          NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Hierarchy relationships
-- ------------------------------------------------------------

CREATE TABLE org_products (
    org_id      VARCHAR(100)  NOT NULL,
    product_id  VARCHAR(100)  NOT NULL,
    sort_order  INT           NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, product_id),
    FOREIGN KEY (org_id)     REFERENCES orgs(id)     ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE product_teams (
    product_id  VARCHAR(100)  NOT NULL,
    team_id     VARCHAR(100)  NOT NULL,
    sort_order  INT           NOT NULL DEFAULT 0,
    PRIMARY KEY (product_id, team_id),
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
    FOREIGN KEY (team_id)    REFERENCES teams(id)     ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- teams.product[] stores display product names (not IDs).
-- Kept as a simple string list for backward compatibility.
CREATE TABLE team_product_labels (
    team_id       VARCHAR(100)  NOT NULL,
    product_name  VARCHAR(255)  NOT NULL,
    PRIMARY KEY (team_id, product_name),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Entity-level configuration
-- Stores config keys for orgs and products (cycleTimeSlowHours,
-- cycleTimeCriticalHours, analyticsprompt, etc.)
-- ------------------------------------------------------------

CREATE TABLE entity_config (
    id            INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    entity_type   ENUM('org', 'product', 'team') NOT NULL,
    entity_id     VARCHAR(100)  NOT NULL,
    config_key    VARCHAR(100)  NOT NULL,
    config_value  TEXT          NULL,
    UNIQUE KEY uq_entity_config (entity_type, entity_id, config_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Team data sources
-- ------------------------------------------------------------

CREATE TABLE sources (
    id        VARCHAR(100)  NOT NULL,
    team_id   VARCHAR(100)  NOT NULL,
    type      ENUM('gitrepos', 'workitems') NOT NULL,
    config    JSON          NULL,
    PRIMARY KEY (team_id, id),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Git repositories (children of a gitrepos source)
CREATE TABLE repos (
    id          VARCHAR(255)  NOT NULL,
    source_id   VARCHAR(100)  NOT NULL,
    team_id     VARCHAR(100)  NOT NULL,
    type        ENUM('github', 'gitlab') NOT NULL DEFAULT 'github',
    owner       VARCHAR(255)  NOT NULL,
    name        VARCHAR(255)  NOT NULL,
    PRIMARY KEY (team_id, source_id, id),
    FOREIGN KEY (team_id, source_id) REFERENCES sources(team_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Repos that are excluded from certain dashboard types
CREATE TABLE repo_excluded_dashboard_types (
    team_id         VARCHAR(100)  NOT NULL,
    source_id       VARCHAR(100)  NOT NULL,
    repo_id         VARCHAR(255)  NOT NULL,
    dashboard_type  VARCHAR(100)  NOT NULL,
    PRIMARY KEY (team_id, source_id, repo_id, dashboard_type),
    FOREIGN KEY (team_id, source_id, repo_id)
        REFERENCES repos(team_id, source_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Contributor whitelist for a repo
CREATE TABLE repo_whitelist (
    team_id     VARCHAR(100)  NOT NULL,
    source_id   VARCHAR(100)  NOT NULL,
    repo_id     VARCHAR(255)  NOT NULL,
    username    VARCHAR(255)  NOT NULL,
    PRIMARY KEY (team_id, source_id, repo_id, username),
    FOREIGN KEY (team_id, source_id, repo_id)
        REFERENCES repos(team_id, source_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Work-item projects (children of a workitems source)
CREATE TABLE workitem_projects (
    id              VARCHAR(100)  NOT NULL,
    source_id       VARCHAR(100)  NOT NULL,
    team_id         VARCHAR(100)  NOT NULL,
    project_name    VARCHAR(255)  NOT NULL,
    jira_project    VARCHAR(50)   NULL,
    type            VARCHAR(50)   NOT NULL DEFAULT 'jira',
    PRIMARY KEY (team_id, source_id, id),
    FOREIGN KEY (team_id, source_id) REFERENCES sources(team_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Product-level repo overrides
-- products.config.overrides.sources.repos[]
-- These remap repos to specific teams at the product level.
-- ------------------------------------------------------------

CREATE TABLE product_override_repos (
    id          INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    product_id  VARCHAR(100)  NOT NULL,
    repo_id     VARCHAR(255)  NOT NULL,
    type        ENUM('github', 'gitlab') NOT NULL DEFAULT 'github',
    owner       VARCHAR(255)  NOT NULL,
    name        VARCHAR(255)  NOT NULL,
    UNIQUE KEY uq_product_repo (product_id, repo_id),
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE product_override_repo_teams (
    override_repo_id  INT           NOT NULL,
    team_id           VARCHAR(100)  NOT NULL,
    PRIMARY KEY (override_repo_id, team_id),
    FOREIGN KEY (override_repo_id)
        REFERENCES product_override_repos(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Dashboards
-- Can belong to an org, product, or team.
-- ------------------------------------------------------------

CREATE TABLE dashboards (
    id                  INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    owner_type          ENUM('org', 'product', 'team') NOT NULL,
    owner_id            VARCHAR(100)  NOT NULL,
    dashboard_id        VARCHAR(100)  NOT NULL,
    type                VARCHAR(100)  NOT NULL,
    title               VARCHAR(255)  NOT NULL,
    description         TEXT          NULL,
    chat_url            TEXT          NULL,
    ai_analytics_url    TEXT          NULL,
    default_source      VARCHAR(255)  NULL,
    connection_id       VARCHAR(50)   NULL,
    jira_project        VARCHAR(50)   NULL,
    UNIQUE KEY uq_dashboard (owner_type, owner_id, dashboard_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Diagrams (children of dashboards)
-- ------------------------------------------------------------

CREATE TABLE diagrams (
    id              INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    dashboard_id    INT           NOT NULL,
    position        INT           NOT NULL DEFAULT 0,
    type            VARCHAR(50)   NOT NULL DEFAULT 'json',
    endpoint        TEXT          NOT NULL,
    span            INT           NULL,
    title           VARCHAR(255)  NULL,
    ai_analytics    BOOLEAN       NOT NULL DEFAULT FALSE,
    description     TEXT          NULL,
    FOREIGN KEY (dashboard_id) REFERENCES dashboards(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- API keys for REST API access control
-- ------------------------------------------------------------

CREATE TABLE api_keys (
    id            INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name          VARCHAR(255)  NOT NULL,
    key_hash      CHAR(64)      NOT NULL UNIQUE,
    is_admin      BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  TIMESTAMP     NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Report subscriptions
-- ------------------------------------------------------------

CREATE TABLE subscriptions (
    id            INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email         VARCHAR(255)  NOT NULL,
    product_id    VARCHAR(100)  NULL,
    team_id          VARCHAR(100)  NULL,
    ai_analytics_url TEXT          NULL,
    interval_days    INT           NULL,
    last_run_at      TIMESTAMP     NULL,
    created_at       TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE subscription_elements (
    id              INT   NOT NULL AUTO_INCREMENT PRIMARY KEY,
    subscription_id INT   NOT NULL,
    sort_order      INT   NOT NULL DEFAULT 0,
    url             TEXT  NOT NULL,
    payload         JSON  NULL,
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ------------------------------------------------------------
-- Alerts
-- Standalone alert definitions evaluated by the scheduler.
-- The rule is a JS expression evaluated against the metrics
-- endpoint response; when it returns true, the response is
-- sent to the AI analytics URL and the result is emailed.
-- ------------------------------------------------------------

CREATE TABLE alerts (
    id               INT          NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email            VARCHAR(255) NOT NULL,
    product_id       VARCHAR(100) NULL,
    team_id          VARCHAR(100) NULL,
    url              TEXT         NOT NULL,
    payload          JSON         NULL,
    ai_analytics_url TEXT         NOT NULL,
    rule             TEXT         NOT NULL,
    created_at       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
SET FOREIGN_KEY_CHECKS = 0;
DROP TABLE
    alerts,
    subscription_elements,
    subscriptions,
    api_keys,
    diagrams,
    dashboards,
    product_override_repo_teams,
    product_override_repos,
    workitem_projects,
    repo_whitelist,
    repo_excluded_dashboard_types,
    repos,
    sources,
    entity_config,
    team_product_labels,
    product_teams,
    org_products,
    teams,
    products,
    orgs;
SET FOREIGN_KEY_CHECKS = 1;
