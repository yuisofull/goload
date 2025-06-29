CREATE TABLE IF NOT EXISTS accounts
(
    account_id   BIGINT UNSIGNED PRIMARY KEY,
    account_name VARCHAR(256) NOT NULL
);

CREATE TABLE IF NOT EXISTS account_passwords
(
    of_account_id   BIGINT UNSIGNED PRIMARY KEY,
    hashed_password VARCHAR(128) NOT NULL
);

CREATE TABLE IF NOT EXISTS download_tasks
(
    task_id         BIGINT UNSIGNED PRIMARY KEY,
    of_account_id   BIGINT UNSIGNED,
    download_type   SMALLINT NOT NULL,
    url             TEXT     NOT NULL,
    download_status SMALLINT NOT NULL,
    metadata        TEXT     NOT NULL
);