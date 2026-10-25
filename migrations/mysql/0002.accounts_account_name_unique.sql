-- Idempotent unique index on accounts.account_name.
-- 0001 is CREATE TABLE IF NOT EXISTS and will not alter existing tables, so this
-- migration adds the constraint for databases created before the unique key existed.
SET @exist := (
    SELECT COUNT(*)
    FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = 'accounts'
      AND column_name = 'account_name'
      AND non_unique = 0
);
SET @sqlstmt := IF(
    @exist = 0,
    'ALTER TABLE accounts ADD UNIQUE INDEX accounts_account_name_key (account_name)',
    'SELECT 1'
);
PREPARE stmt FROM @sqlstmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
