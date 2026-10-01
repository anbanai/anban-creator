-- One-time logical identity cutover. Apply after the schema migrations and
-- before serving task/project requests. Every statement is idempotent.
UPDATE `projects` SET `platform` = 'wechat' WHERE `platform` = 'article';
UPDATE `tasks` SET `type` = 'wechat-article' WHERE `type` = 'article';
UPDATE `plans` SET `type` = 'wechat-article' WHERE `type` = 'article';
UPDATE `tasks` SET `billing_sku_id` = REPLACE(`billing_sku_id`, 'task.article.', 'task.wechat_article.') WHERE `billing_sku_id` LIKE 'task.article.%';
UPDATE `billing_skus` SET `operation` = 'task.wechat_article' WHERE `operation` = 'task.article';
UPDATE `billing_skus` SET `sku_id` = REPLACE(`sku_id`, 'task.article.', 'task.wechat_article.') WHERE `sku_id` LIKE 'task.article.%';
UPDATE `billing_quotes` SET `sku_id` = REPLACE(`sku_id`, 'task.article.', 'task.wechat_article.') WHERE `sku_id` LIKE 'task.article.%';
UPDATE `billing_charges` SET `operation` = 'task.wechat_article' WHERE `operation` = 'task.article';
UPDATE `task_executions` SET `runtime_profile` = 'wechat' WHERE `runtime_profile` = 'article';
UPDATE `strategy_snapshots` SET `applicable_tasks` = REPLACE(REPLACE(`applicable_tasks`, '"article"', '"wechat-article"'), 'task.article', 'task.wechat_article') WHERE `applicable_tasks` LIKE '%article%';
