-- schema.sql
CREATE TABLE tasks
(
    id            BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    of_account_id BIGINT UNSIGNED NOT NULL,
    name          TEXT            NOT NULL,
    description   TEXT,
    source_url    TEXT            NOT NULL,
    source_type   VARCHAR(32)     NOT NULL,
    source_auth   JSON,
    storage_type  VARCHAR(32)     NOT NULL,
    storage_path  TEXT            NOT NULL,
    status        VARCHAR(32)     NOT NULL,
    file_info     JSON,
    progress      JSON,
    options       JSON,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    completed_at  DATETIME,
    error         TEXT,
    retry_count   INT             NOT NULL DEFAULT 0,
    max_retries   INT             NOT NULL DEFAULT 1,
    tags          JSON,
    metadata      JSON,
    INDEX (of_account_id),
    INDEX (status)
);
