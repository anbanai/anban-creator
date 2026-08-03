-- Submit every selected step as a separate DMS change item.
-- Run this one-time contract migration only after every Server pod using the legacy fields is retired.
-- The startup migration performs the repeatable expand/backfill phase.

-- Step 1: inspect the live schema before selecting either branch.
SELECT EXISTS (SELECT 1 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'templates' AND COLUMN_NAME = 'style_prompt') AS `has_style_prompt`, EXISTS (SELECT 1 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'projects' AND COLUMN_NAME = 'template_id') AS `has_template_id`;

-- Select the path matching the has_style_prompt,has_template_id result.
-- 0,0: stop after Step 1 because the contract is already applied.
-- 1,0: run T1 and T2, then run T3 only when T2 returned 0.
-- 0,1: run P1 and P2, then run P3 only when P2 returned 0.
-- 1,1: run T1, P1, T2, and P2, then run no DROP unless T2 and P2 both returned 0.

-- Step T1: perform the final template prompt backfill.
UPDATE `templates` SET `prompt` = `style_prompt` WHERE TRIM(COALESCE(`prompt`, '')) = '' AND TRIM(COALESCE(`style_prompt`, '')) <> '';

-- Step P1: preserve existing project styles and fill only empty values.
UPDATE `projects` p JOIN `templates` t ON t.`id` = p.`template_id` SET p.`style` = t.`prompt` WHERE TRIM(COALESCE(p.`style`, '')) = '' AND TRIM(COALESCE(p.`template_id`, '')) <> '' AND TRIM(COALESCE(t.`prompt`, '')) <> '';

-- Step T2: continue to T3 only when template_prompt_incomplete is 0.
SELECT COUNT(*) AS `template_prompt_incomplete` FROM `templates` WHERE TRIM(COALESCE(`prompt`, '')) = '' AND TRIM(COALESCE(`style_prompt`, '')) <> '';

-- Step P2: continue to P3 only when project_style_incomplete is 0.
SELECT COUNT(*) AS `project_style_incomplete` FROM `projects` p JOIN `templates` t ON t.`id` = p.`template_id` WHERE TRIM(COALESCE(p.`style`, '')) = '' AND TRIM(COALESCE(p.`template_id`, '')) <> '' AND TRIM(COALESCE(t.`prompt`, '')) <> '';

-- Step T3: run only after every selected validation step returned 0.
ALTER TABLE `templates` DROP COLUMN `style_prompt`;

-- Step P3: run only after every selected validation step returned 0.
ALTER TABLE `projects` DROP COLUMN `template_id`;
