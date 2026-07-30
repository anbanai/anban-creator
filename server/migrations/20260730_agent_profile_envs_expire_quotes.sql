-- Old schema-v2 Quotes cannot safely create schema-v3 tasks after the cutover.
UPDATE `billing_quotes`
SET `expires_at` = CURRENT_TIMESTAMP(3)
WHERE `consumed_at` IS NULL
  AND `expires_at` > CURRENT_TIMESTAMP(3)
  AND JSON_UNQUOTE(JSON_EXTRACT(`agent_profile_snapshot`, '$.schema_version')) = '2';
