# Easel Trigger Policy

Anban preserves Easel's contracts but moves expensive work to a scheduler:

- publish-log: immediate, deterministic, no LLM.
- data-tracker: daily low-peak scan, only new revision or mature content.
- publish-analytics/content-postmortem: weekly, mature and non-empty data only.
- performance-review/strategy-advisor: monthly closed period; strategy requires
  at least 10 valid contents.
- post-scorer: generation-local, reads the frozen strategy and never rebuilds it.

Skipped jobs retain a reason and fingerprint without creating an Agent task.
