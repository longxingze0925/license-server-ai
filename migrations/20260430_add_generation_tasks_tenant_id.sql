-- Add tenant_id to generation_tasks and backfill existing rows.
-- This script is idempotent for MySQL/MariaDB.

SET @database_name = DATABASE();

SET @column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'generation_tasks'
    AND COLUMN_NAME = 'tenant_id'
);

SET @sql = IF(
  @column_exists = 0,
  'ALTER TABLE generation_tasks ADD COLUMN tenant_id VARCHAR(36) NULL AFTER id',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE generation_tasks gt
LEFT JOIN applications a ON a.id = gt.app_id
LEFT JOIN tenants t ON t.id = gt.app_id
SET gt.tenant_id = COALESCE(NULLIF(a.tenant_id, ''), NULLIF(t.id, ''), gt.tenant_id)
WHERE (gt.tenant_id IS NULL OR gt.tenant_id = '')
  AND (a.tenant_id IS NOT NULL OR t.id IS NOT NULL);

SET @index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'generation_tasks'
    AND INDEX_NAME = 'idx_generation_tasks_tenant_id'
);

SET @sql = IF(
  @index_exists = 0,
  'CREATE INDEX idx_generation_tasks_tenant_id ON generation_tasks (tenant_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
