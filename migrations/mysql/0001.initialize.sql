-- +migrate Down
# DROP TABLE IF EXISTS tasks;
#
# DROP TABLE IF EXISTS token_public_keys;
#
# DROP TABLE IF EXISTS account_passwords;
#
# DROP TABLE IF EXISTS accounts;

-- +migrate Up
CREATE TABLE
    IF NOT EXISTS accounts (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        account_name VARCHAR(256) NOT NULL,
        PRIMARY KEY (id)
    );

CREATE TABLE
    IF NOT EXISTS account_passwords (
        of_account_id BIGINT UNSIGNED NOT NULL,
        hashed_password VARCHAR(128) NOT NULL,
        PRIMARY KEY (of_account_id),
        FOREIGN KEY (of_account_id) REFERENCES accounts (id) ON DELETE CASCADE
    );

CREATE TABLE
    IF NOT EXISTS token_public_keys (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        public_key TEXT NOT NULL,
        PRIMARY KEY (id)
    );

CREATE TABLE
    IF NOT EXISTS tasks (
        id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
        of_account_id BIGINT UNSIGNED NOT NULL,
        file_name TEXT NOT NULL,
        source_url TEXT NOT NULL,
        source_type VARCHAR(32) NOT NULL,
        headers JSON,
        source_auth JSON,
        storage_type VARCHAR(32) NOT NULL,
        storage_path TEXT NOT NULL,
        -- Checksum verification
        checksum_type TEXT, -- "md5", "sha1", "sha256"
        checksum_value TEXT,
        -- Download behavior
        concurrency INT DEFAULT 4,
        max_speed BIGINT, -- bytes/sec
        max_retries INT NOT NULL DEFAULT 3,
        timeout INT, -- seconds
        status VARCHAR(32) NOT NULL,
        progress FLOAT DEFAULT 0.00, -- percent, e.g., 45.67
        downloaded_bytes BIGINT DEFAULT 0,
        total_bytes BIGINT DEFAULT 0,
        error_message TEXT,
        metadata JSON,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        completed_at DATETIME,
        last_accessed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        expiration_days INT UNSIGNED DEFAULT 30, -- days
        INDEX (of_account_id),
        INDEX (status)
    );

