-- Allow provider credential channel names to be reused after soft delete.
-- This script is idempotent for MySQL/MariaDB.
--
-- Important:
--   1. Run this after deploying code that writes active_channel_name.
--   2. If active duplicate channel names already exist, the unique index creation
--      will fail. Resolve duplicates first; old schema normally prevents this.

SET @database_name = DATABASE();

SET @column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND COLUMN_NAME = 'active_channel_name'
);

SET @sql = IF(
  @column_exists = 0,
  'ALTER TABLE provider_credentials ADD COLUMN active_channel_name VARCHAR(64) NULL AFTER channel_name',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE provider_credentials
SET active_channel_name = channel_name
WHERE deleted_at IS NULL
  AND (active_channel_name IS NULL OR active_channel_name = '');

UPDATE provider_credentials
SET active_channel_name = NULL
WHERE deleted_at IS NOT NULL;

SET @old_unique_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND INDEX_NAME = 'idx_prov_chan'
);

SET @sql = IF(
  @old_unique_exists > 0,
  'DROP INDEX idx_prov_chan ON provider_credentials',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @active_unique_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND INDEX_NAME = 'idx_prov_active_chan'
);

SET @sql = IF(
  @active_unique_exists = 0,
  'CREATE UNIQUE INDEX idx_prov_active_chan ON provider_credentials (provider, active_channel_name)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @lookup_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND INDEX_NAME = 'idx_prov_chan_lookup'
);

SET @sql = IF(
  @lookup_index_exists = 0,
  'CREATE INDEX idx_prov_chan_lookup ON provider_credentials (provider, channel_name)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
