-- Additive and repeatable for deployments that apply SQL before AutoMigrate.
SET @designer_opacity_exists = (
 SELECT COUNT(*) FROM information_schema.COLUMNS
 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'image_generations'
   AND COLUMN_NAME = 'background_opacity'
);
SET @designer_opacity_sql = IF(@designer_opacity_exists = 0,
 'ALTER TABLE `image_generations` ADD COLUMN `background_opacity` bigint NULL',
 'SELECT 1');
PREPARE designer_opacity_stmt FROM @designer_opacity_sql;
EXECUTE designer_opacity_stmt;
DEALLOCATE PREPARE designer_opacity_stmt;
