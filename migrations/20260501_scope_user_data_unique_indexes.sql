-- Scope client-side data identifiers by user and application.
-- This script is idempotent for MySQL/MariaDB.
--
-- It replaces old global unique indexes with:
--   (user_id, app_id, workflow_id)
--   (user_id, app_id, task_id)
--   (user_id, app_id, material_id)
--   (user_id, app_id, voice_id)
--
-- Important: if duplicate rows already exist inside the same user/app scope,
-- the CREATE UNIQUE INDEX statements will fail. Resolve duplicates first.

SET @database_name = DATABASE();

SET @old_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_workflows'
    AND INDEX_NAME = 'idx_user_workflows_workflow_id'
);

SET @sql = IF(
  @old_index_exists > 0,
  'DROP INDEX idx_user_workflows_workflow_id ON user_workflows',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @new_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_workflows'
    AND INDEX_NAME = 'idx_user_workflow_key'
);

SET @sql = IF(
  @new_index_exists = 0,
  'CREATE UNIQUE INDEX idx_user_workflow_key ON user_workflows (user_id, app_id, workflow_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @old_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_batch_tasks'
    AND INDEX_NAME = 'idx_user_batch_tasks_task_id'
);

SET @sql = IF(
  @old_index_exists > 0,
  'DROP INDEX idx_user_batch_tasks_task_id ON user_batch_tasks',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @new_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_batch_tasks'
    AND INDEX_NAME = 'idx_user_batch_task_key'
);

SET @sql = IF(
  @new_index_exists = 0,
  'CREATE UNIQUE INDEX idx_user_batch_task_key ON user_batch_tasks (user_id, app_id, task_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @old_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_materials'
    AND INDEX_NAME = 'idx_user_materials_material_id'
);

SET @sql = IF(
  @old_index_exists > 0,
  'DROP INDEX idx_user_materials_material_id ON user_materials',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @new_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_materials'
    AND INDEX_NAME = 'idx_user_material_key'
);

SET @sql = IF(
  @new_index_exists = 0,
  'CREATE UNIQUE INDEX idx_user_material_key ON user_materials (user_id, app_id, material_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @old_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_voice_configs'
    AND INDEX_NAME = 'idx_user_voice_configs_voice_id'
);

SET @sql = IF(
  @old_index_exists > 0,
  'DROP INDEX idx_user_voice_configs_voice_id ON user_voice_configs',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @new_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @database_name
    AND TABLE_NAME = 'user_voice_configs'
    AND INDEX_NAME = 'idx_user_voice_config_key'
);

SET @sql = IF(
  @new_index_exists = 0,
  'CREATE UNIQUE INDEX idx_user_voice_config_key ON user_voice_configs (user_id, app_id, voice_id)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
