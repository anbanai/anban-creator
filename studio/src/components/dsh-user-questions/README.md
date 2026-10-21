# DSH User Questions Adapter

This directory adapts the question UX from DeepSeek Harness for Anban Studio.

## Upstream

- Package: `@deepseek-ai/dsh-client-ui-user-questions`
- Runtime protocol target: `0.2.1-rc.2`
- The checked-out upstream package currently declares `0.2.1-rc.2`; verify
  the published UI package before moving this adapter to a direct dependency.
- Source checkout: `/Users/medivh/WORKSPACE/deepseek-harness/packages/client/ui-user-questions`
- Primary references: `src/client/QuestionComposer.tsx`, `QuestionReplyView.tsx`, and `PlanReviewPanel.tsx`

## Why this is an adapter

The upstream components are Cordis slot consumers. They require DSH session
projection state, remote-event answer channels, locale injection, and the DSH
conversation host. Studio owns those concerns through its Server SSE stream and
HTTP APIs, so importing the upstream React component directly would embed the
DSH Web Shell and couple Studio to its runtime.

`DshQuestionComposer` keeps the upstream interaction contract at the boundary:

- one question is visible at a time;
- radio and checkbox semantics match DSH;
- recommended options are draft selections only;
- custom single-select answers are mutually exclusive;
- all answers in a batch are submitted together;
- skip, paging, Markdown details, read-only answered state, and retry behavior
  follow DSH semantics.
- `intent.kind = "plan-review"` renders the plan detail in a dedicated review
  panel while preserving the same answer contract.
- custom answers follow the Server's 20,000 Unicode code-point limit and can be
  opened in an expanded editor for long document pastes; expired,
  closed, or unavailable sessions disable unanswered cards with a restart hint.
- answered batches remain in the conversation when the Agent opens later batches.

When upgrading DSH, compare the upstream composer tests and README against the
adapter tests before changing the event or answer contract. A future direct
reuse should introduce a DSH Web Host adapter first; it should not import
`QuestionComposer` into Studio without providing its Cordis/session contracts.
