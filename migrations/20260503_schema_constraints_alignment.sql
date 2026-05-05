-- Align model-level uniqueness with existing product assumptions.
-- Run after resolving duplicate rows returned by the diagnostic SELECTs below.
-- These diagnostics intentionally include soft-deleted rows because MySQL unique indexes see them too.

SET @database_name = DATABASE();

SELECT tenant_id, email, COUNT(*) AS duplicate_count
FROM customers
GROUP BY tenant_id, email
HAVING COUNT(*) > 1;

SELECT tenant_id, license_id, machine_id, COUNT(*) AS duplicate_count
FROM devices
WHERE license_id IS NOT NULL
GROUP BY tenant_id, license_id, machine_id
HAVING COUNT(*) > 1;

SELECT tenant_id, subscription_id, machine_id, COUNT(*) AS duplicate_count
FROM devices
WHERE subscription_id IS NOT NULL
GROUP BY tenant_id, subscription_id, machine_id
HAVING COUNT(*) > 1;

SELECT user_id, app_id, table_name, record_id, COUNT(*) AS duplicate_count
FROM user_table_data
GROUP BY user_id, app_id, table_name, record_id
HAVING COUNT(*) > 1;

SET @index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'customers'
    AND INDEX_NAME = 'idx_customers_tenant_email'
);

SET @sql = IF(
  @index_exists = 0,
  'CREATE UNIQUE INDEX idx_customers_tenant_email ON customers (tenant_id, email)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

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

SET @index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'devices'
    AND INDEX_NAME = 'idx_devices_license_machine'
);

SET @sql = IF(
  @index_exists = 0,
  'CREATE UNIQUE INDEX idx_devices_license_machine ON devices (tenant_id, license_id, machine_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'devices'
    AND INDEX_NAME = 'idx_devices_subscription_machine'
);

SET @sql = IF(
  @index_exists = 0,
  'CREATE UNIQUE INDEX idx_devices_subscription_machine ON devices (tenant_id, subscription_id, machine_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
