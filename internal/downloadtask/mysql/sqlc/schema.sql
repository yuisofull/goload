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