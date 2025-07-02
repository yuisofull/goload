CREATE TABLE IF NOT EXISTS accounts
(
    id           BIGINT UNSIGNED AUTO_INCREMENT,
    account_name VARCHAR(256) NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS account_passwords
(
    of_account_id   BIGINT UNSIGNED AUTO_INCREMENT,
    hashed_password VARCHAR(128) NOT NULL,
    PRIMARY KEY (of_account_id)
);

CREATE TABLE IF NOT EXISTS token_public_keys
(
    id         BIGINT UNSIGNED AUTO_INCREMENT,
    public_key TEXT NOT NULL,
    PRIMARY KEY (id)
);