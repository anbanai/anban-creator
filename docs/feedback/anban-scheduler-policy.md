# Anban Scheduler Policy

Daily scans run in the project timezone during 03:00-05:00; weekly scans run
Monday 04:00-06:00; monthly scans run the first business day 04:00-07:00.
Projects are ordered by user then project, with a per-user admission cap and
database fingerprint uniqueness. Project/account/operation locks and Asynq
unique task IDs prevent duplicates. Transient infrastructure failures retry;
identity, schema, permission, and insufficient-data failures are terminal.

No new revision, mature content, valid metric, sample threshold, or available
coverage means `skipped`, never an empty Agent/LLM execution.
