-- Forward-only migration for the fixed-SKU task billing snapshot.
-- Stop application traffic before running this migration, then restart the
-- server so requireTaskBillingSchema verifies the complete contract.

ALTER TABLE `tasks`
  ADD COLUMN `billing_quote_id` char(36) DEFAULT NULL,
  ADD COLUMN `billing_catalog_id` varchar(128) DEFAULT NULL,
  ADD COLUMN `billing_sku_id` varchar(128) DEFAULT NULL,
  ADD COLUMN `billing_pricing_tier` varchar(20) DEFAULT NULL,
  ADD COLUMN `billing_charge_id` char(36) DEFAULT NULL,
  ADD COLUMN `billing_price_credits` bigint NOT NULL DEFAULT 0,
  ADD COLUMN `billing_terminal_reason` varchar(64) DEFAULT NULL,
  ADD KEY `idx_tasks_billing_quote_id` (`billing_quote_id`),
  ADD KEY `idx_tasks_billing_catalog_id` (`billing_catalog_id`),
  ADD KEY `idx_tasks_billing_sku_id` (`billing_sku_id`),
  ADD KEY `idx_tasks_billing_pricing_tier` (`billing_pricing_tier`),
  ADD UNIQUE KEY `idx_tasks_billing_charge_id` (`billing_charge_id`),
  ADD KEY `idx_tasks_billing_terminal_reason` (`billing_terminal_reason`);
