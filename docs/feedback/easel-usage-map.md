# Easel Usage Map

The verified dependency order is:

`publish-log -> data-tracker -> publish-analytics -> performance-review/content-postmortem -> strategy-advisor -> post-scorer -> next generation`.

Publication success records only identity, provider evidence, timestamp, and
observation windows. Analytics observations arrive later. The next generation
reads an explicit strategy snapshot; Easel does not perform online model
training or automatic fine-tuning.
