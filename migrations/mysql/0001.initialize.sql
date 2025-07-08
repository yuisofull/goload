-- +migrate Up
CREATE TABLE IF NOT EXISTS accounts
(
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    account_name VARCHAR(256)    NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS account_passwords
(
    of_account_id   BIGINT UNSIGNED NOT NULL,
    hashed_password VARCHAR(128)    NOT NULL,
    PRIMARY KEY (of_account_id),
    FOREIGN KEY (of_account_id) REFERENCES accounts (id) ON DELETE CASCADE
);


CREATE TABLE IF NOT EXISTS token_public_keys
(
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_key TEXT            NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS download_tasks
(
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    of_account_id   BIGINT UNSIGNED NOT NULL,
    download_type   SMALLINT        NOT NULL,
    url             TEXT            NOT NULL,
    download_status SMALLINT        NOT NULL,
    metadata        JSON            NOT NULL,
    PRIMARY KEY (id)
);

-- +migrate Down
DROP TABLE IF EXISTS download_tasks;

DROP TABLE IF EXISTS token_public_keys;

DROP TABLE IF EXISTS account_passwords;

DROP TABLE IF EXISTS accounts;