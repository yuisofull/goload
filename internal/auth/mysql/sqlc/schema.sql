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