CREATE TABLE accounts
(
    id           BIGINT UNSIGNED PRIMARY KEY,
    account_name VARCHAR(256) NOT NULL
);

CREATE TABLE account_passwords
(
    of_account_id   BIGINT UNSIGNED PRIMARY KEY,
    hashed_password VARCHAR(128) NOT NULL
);

CREATE TABLE token_public_keys
(
    id         BIGINT UNSIGNED PRIMARY KEY,
    public_key VARBINARY(512) NOT NULL
);
