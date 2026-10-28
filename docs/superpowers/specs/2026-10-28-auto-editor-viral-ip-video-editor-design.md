# Auto-Editor Integration Design

## Intent

Integrate WyattBlue/auto-editor into the `viral-ip-video-editor` Agent so
audio-based detection supplies the first pass for removing dead air in recorded
talking-head footage. Keep the existing semantic editing, review, visual plan,
OpenMontage render, cover, and delivery workflow.

## Desired behavior

- Pin Auto-Editor 29.3.1 in an amd64 dedicated runtime image for the
  `viral-ip-video-editor` Pack. Materialize and verify its upstream Linux x86_64
  binary at image build time so managed executions do not need GitHub access to
  start.
- Run audio-threshold silence detection against the original speech media with
  Premiere XML export. The Agent reads the XML source in/out frame ranges and
  converts them into candidate edits in the existing `edit_decision_list.json`;
  it does not pass Premiere XML to OpenMontage.
- Keep EDL source ranges in original-media time. Compute each accepted segment's
  composition range cumulatively after rejected detections and other editorial
  cuts, then pass the reviewed EDL through the existing OpenMontage handoff.
- Preserve adjustable margins around speech. Review every candidate against the
  corrected transcript and performance: retain meaningful breaths, emphasis,
  hesitation, emotional pauses, and comprehension time; reject cuts that clip
  words, alter meaning, or create audible discontinuities.
- Keep original media read-only and record Auto-Editor version, effective
  threshold, margins, Premiere XML evidence path, accepted/rejected source
  ranges, composition-time mapping, and resulting duration in the existing task
  artifacts. OpenMontage remains the final renderer.
- Register the dedicated image consistently in Make, Compose, Server runtime
  configuration, Kubernetes rendering, and release manifests.

## Boundaries

Auto-Editor owns audio-based dead-air candidate detection and the initial cut
pass only. It does not choose semantic deletions or rearrange content, replace
Agent review, own the final render, or alter other Agent Packs. If the executable
is unavailable or analysis fails, the Agent follows its existing failure and
degradation contract rather than claiming automatic silence detection ran.

## Validation

Check the dedicated Dockerfile pins and materializes the executable for amd64,
profile-to-image mappings stay one-to-one, generated Pack surfaces match their
sources, and Agent guidance preserves original source timecodes while recording
composition-time offsets. Run the focused Go runtime/config tests, Pack
generation and checks, and build the dedicated image when the environment
permits.
