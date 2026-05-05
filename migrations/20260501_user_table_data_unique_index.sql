-- Ensure generic user table data cannot create duplicate logical records.
-- This script is idempotent for MySQL/MariaDB.
--
-- Important: if duplicate rows already exist for the same
-- (user_id, app_id, table_name, record_id), the CREATE UNIQUE INDEX statement
-- will fail. Resolve duplicates first.

SET @database_name = DATABASE();

SET @index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_table_data'
    AND INDEX_NAME = 'idx_user_table_data_unique_record'
);

SET @sql = IF(
  @index_exists = 0,
  'CREATE UNIQUE INDEX idx_user_table_data_unique_record ON user_table_data (user_id, app_id, table_name, record_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
