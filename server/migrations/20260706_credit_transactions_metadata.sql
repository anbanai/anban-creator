-- Persist operation billing snapshots such as provider/model, token usage, and
-- final credit calculation inputs for MCP/model/media charges.

ALTER TABLE credit_transactions
  ADD COLUMN metadata JSON NULL;
