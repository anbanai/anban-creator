-- Submit each numbered step as a separate DMS change item, in order.
-- Run this one-time contract migration only after every Server pod using the legacy fields is retired.
-- The startup migration performs the repeatable expand/backfill phase.

-- Step 1/6: perform the final template prompt backfill after the rollout.
UPDATE `templates` SET `prompt` = `style_prompt` WHERE TRIM(COALESCE(`prompt`, '')) = '' AND TRIM(COALESCE(`style_prompt`, '')) <> '';

-- Step 2/6: preserve existing project styles and fill only empty values.
UPDATE `projects` p JOIN `templates` t ON t.`id` = p.`template_id` SET p.`style` = t.`prompt` WHERE TRIM(COALESCE(p.`style`, '')) = '' AND TRIM(COALESCE(p.`template_id`, '')) <> '' AND TRIM(COALESCE(t.`prompt`, '')) <> '';

-- Step 3/6: continue only when template_prompt_incomplete is 0.
SELECT COUNT(*) AS `template_prompt_incomplete` FROM `templates` WHERE TRIM(COALESCE(`prompt`, '')) = '' AND TRIM(COALESCE(`style_prompt`, '')) <> '';

-- Step 4/6: continue only when project_style_incomplete is 0.
SELECT COUNT(*) AS `project_style_incomplete` FROM `projects` p JOIN `templates` t ON t.`id` = p.`template_id` WHERE TRIM(COALESCE(p.`style`, '')) = '' AND TRIM(COALESCE(p.`template_id`, '')) <> '' AND TRIM(COALESCE(t.`prompt`, '')) <> '';

-- Step 5/6: run only after Steps 3 and 4 both returned 0.
ALTER TABLE `templates` DROP COLUMN `style_prompt`;

-- Step 6/6: finish removing the obsolete project-template binding.
ALTER TABLE `projects` DROP COLUMN `template_id`;
