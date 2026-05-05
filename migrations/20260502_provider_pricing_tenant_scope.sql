-- Scope provider credentials and pricing rules by tenant.
-- This script is idempotent for MySQL/MariaDB.
--
-- Existing rows are assigned to the first tenant so single-tenant deployments keep working.
-- Multi-tenant deployments should review the backfilled rows after running this migration.

SET @database_name = DATABASE();

SET @default_tenant_id = (
  SELECT id
  FROM tenants
  ORDER BY created_at ASC
  LIMIT 1
);

SET @column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND COLUMN_NAME = 'tenant_id'
);

SET @sql = IF(
  @column_exists = 0,
  'ALTER TABLE provider_credentials ADD COLUMN tenant_id VARCHAR(36) NULL AFTER id',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE provider_credentials
SET tenant_id = @default_tenant_id
WHERE (tenant_id IS NULL OR tenant_id = '')
  AND @default_tenant_id IS NOT NULL;

SET @old_unique_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND INDEX_NAME = 'idx_prov_active_chan'
);

SET @sql = IF(
  @old_unique_exists > 0,
  'DROP INDEX idx_prov_active_chan ON provider_credentials',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @tenant_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'provider_credentials'
    AND INDEX_NAME = 'idx_provider_credentials_tenant'
);

SET @sql = IF(
  @tenant_index_exists = 0,
  'CREATE INDEX idx_provider_credentials_tenant ON provider_credentials (tenant_id)',
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
  'CREATE UNIQUE INDEX idx_prov_active_chan ON provider_credentials (tenant_id, provider, active_channel_name)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'pricing_rules'
    AND COLUMN_NAME = 'tenant_id'
);

SET @sql = IF(
  @column_exists = 0,
  'ALTER TABLE pricing_rules ADD COLUMN tenant_id VARCHAR(36) NULL AFTER id',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE pricing_rules
SET tenant_id = @default_tenant_id
WHERE (tenant_id IS NULL OR tenant_id = '')
  AND @default_tenant_id IS NOT NULL;

SET @tenant_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'pricing_rules'
    AND INDEX_NAME = 'idx_pricing_rules_tenant'
);

SET @sql = IF(
  @tenant_index_exists = 0,
  'CREATE INDEX idx_pricing_rules_tenant ON pricing_rules (tenant_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
