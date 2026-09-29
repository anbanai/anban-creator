# Easel Reuse Matrix

| Easel capability | Anban reuse | Adaptation |
| --- | --- | --- |
| publish-log | immutable publication fact and evidence | Server publication state machine is authoritative |
| data-tracker | AnalyticsObservation, revision, projection buckets | daily eligibility queue replaces local files |
| publish-analytics | deterministic feature aggregation and evidence semantics | weekly Asynq job and tenant locks |
| performance-review | period review, sample/coverage warnings | monthly StrategySnapshot source |
| content-postmortem | mature-window item review | 7/30-day eligibility in Server |
| post-scorer | pre-publication historical scoring | reads frozen Bootstrap file |
| strategy-advisor | versioned recommendations and limitations | advisory snapshots, no automatic profile mutation |

Easel scripts calculate deterministic facts and reserve LLM use for
interpretation. Anban keeps the same separation while making Server persistence
the source of truth.
