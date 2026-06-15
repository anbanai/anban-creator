---
name: live-slice
description: Use when working on live video slicing, 直播切片, 剪直播, 智能切片, 听悟 transcription, or live video slicing workflows that turn long livestream videos into transcript-backed short-video clips.
---

# 直播切片

Use this skill to convert a local livestream video into transcript-backed short-video clip plans and exports. Local media work uses direct `ffmpeg`/`ffprobe` commands only; MCP tools handle OSS upload, TingWu analysis, and LLM JSON planning.

## Default Artifacts

Use one output directory per source video:

| File | Purpose |
| --- | --- |
| `metadata.json` | `ffprobe` metadata |
| `cover.jpg` | first-frame cover |
| `audio.mp3` | extracted audio for TingWu |
| `analysis.json` | normalized TingWu result |
| `invalid-sentences.json` | unusable sentence indexes |
| `segments.json` | slicing plan |
| `clip-plan.json` | deterministic clip timings and ffmpeg commands |
| `subject-clip-plan.json` | deterministic topic-script clip parts and concat commands |
| `clip_results.json` | local ffmpeg execution results |
| `clip-manifest.json` | JSON array of delivered clips |
| `clip-draft-results.json` | CapCut draft creation results (optional) |
| `exports/` | cut videos and text exports |

## Workflow

1. Check media tools:
   `command -v ffmpeg && command -v ffprobe`

   If either command is missing, tell the operator to install FFmpeg. Python is not required.

2. Prepare artifacts:

   ```bash
   mkdir -p "$DIR" "$DIR/exports"
   ffprobe -v error -show_format -show_streams -of json "$VIDEO" > "$DIR/metadata.json"
   ffmpeg -y -i "$VIDEO" -vn -ac 1 -ar 16000 -codec:a libmp3lame -q:a 4 "$DIR/audio.mp3"
   ffmpeg -y -ss 0 -i "$VIDEO" -frames:v 1 -q:v 2 "$DIR/cover.jpg"
   ```

3. Upload audio:
   Call `upload_live_audio(file_path="$DIR/audio.mp3")`.
   If storage is local, ask the operator to configure OSS or provide a direct `audio_url`.

4. Create TingWu task:
   Call `create_live_analysis_task(audio_url=..., auto_chapters_enabled=true, summarization_enabled=true, meeting_assistance_enabled=true, diarization_enabled=false, script_template_enable=true)`.

5. Poll analysis:
   Call `query_live_analysis_task(task_id=...)` until `status` is `COMPLETED`, then save the JSON to `analysis.json`.

6. Plan cleanup and cuts:
   - Call `recognize_live_invalid_sentences(sentences=analysis.sentences)` and save `invalid-sentences.json`.
   - Remove invalid indexes from `analysis.sentences`.
   - Call `recognize_live_segments(sentences=valid_sentences, ask=optional_user_goal)` and save `segments.json`.
   - Call `build_live_clip_plan(sentences=analysis.sentences, segments=segments.segments, invalid=invalid.invalid, video_path="$VIDEO", output_dir="$DIR")` and save `clip-plan.json`.
   - Use `recognize_live_subjects` and `complete_live_subject` when the user wants topic-driven clips instead of broad segments, then call `build_live_subject_clip_plan(sentences=analysis.sentences, completions=subject_completions, invalid=invalid.invalid, video_path="$VIDEO", output_dir="$DIR")` and save `subject-clip-plan.json`.
   - Never hand-convert non-contiguous subject scripts into `segments.json`; use `build_live_subject_clip_plan`.

7. Cut clips:
   Read each item in the selected plan (`clip-plan.json` or `subject-clip-plan.json`). Before each command, create parent directories for planned `output`, `parts[].output`, and `concat_list_path`. For single-part clips, run `fast_cut_shell` first, then use `ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$OUT"` to read output duration. If the command fails, the output is missing or empty, `ffprobe` fails, or actual duration differs from planned duration by more than `max(1.0s, duration*0.05)`, run `accurate_cut_shell` and read duration again. For multi-part clips, run every `parts[].accurate_cut_shell`, write `concat_list_content` to `concat_list_path`, run `concat_shell`, then read the final duration. Save one execution result per clip to `clip_results.json` with `actual_duration_seconds`; include `part_results` only for multi-part clips.

8. Build delivery files:
   Call `build_live_clip_manifest(source_video="$VIDEO", tingwu_task_id="$TINGWU_TASK_ID", analysis_title=analysis.title, sentences=analysis.sentences, invalid=invalid.invalid, warnings=plan.warnings, rejected=plan.rejected, clips=plan.clips, clip_results=clip_results)` and write:
   - `clip_manifest` to `clip-manifest.json` as a JSON array with each clip transcript.
   - `transcript_markdown` to `transcript.md`.
   - `summary_markdown` to `summary.md`.
   - `clip_notes_markdown[].markdown` to `clip_notes_markdown[].markdown_path`.

9. Export to CapCut (optional):
   For each successful clip in `clip_results.json`, create a CapCut/JianYing draft using the `capcut-draft` skill. Each clip becomes its own draft with:
   - Video segment: the clip's MP4 file (`type: "video"`)
   - Subtitles: from the clip's `transcript` array, with time offsets relative to the clip's start (`subtitle_start = (sentence.start - clip.start) × 1,000,000` microseconds)
   - Cover: `$DIR/cover.jpg`
   - Canvas: vertical 9:16 (1080×1920) for short videos
   - No effects, transitions, or background music added automatically
   Save results to `clip-draft-results.json`. Skip this step entirely if CapCut draft root directory is not found.

## Cutting Commands

Fast cut:

```bash
ffmpeg -y -ss "$START" -i "$VIDEO" -t "$DURATION" -c copy "$OUT"
```

Accurate fallback when copy-mode cuts poorly or fails:

```bash
ffmpeg -y -ss "$START" -i "$VIDEO" -t "$DURATION" -c:v libx264 -c:a aac "$OUT"
```

Single clip example:

```bash
START="12.300"
DURATION="38.700"
OUT="$DIR/exports/01-product-proof.mp4"
ffmpeg -y -ss "$START" -i "$VIDEO" -t "$DURATION" -c copy "$OUT"
```

For automated batches, prefer the `fast_cut_shell` and `accurate_cut_shell` fields returned by `build_live_clip_plan`; do not hand-calculate durations.

Multi-part subject clips use planned part commands and concat fields:

```bash
# for each part
ffmpeg -y -ss "$START" -i "$VIDEO" -t "$DURATION" -c:v libx264 -c:a aac "$PART_OUT"

# after writing concat_list_content to concat_list_path
ffmpeg -y -f concat -safe 0 -i "$CONCAT_LIST" -c copy "$OUT"
```

Duration probe:

```bash
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$OUT"
```

## Text Export

For Markdown transcript exports, write the content directly from `analysis.sentences`:

```markdown
# {analysis.title}

- [1] 0.00-3.20s 字幕
- [2] 3.20-7.80s 字幕
```

For clip notes, write the `clip_notes_markdown` returned by `build_live_clip_manifest`; do not invent note content by hand.

## JSON Shapes

`analysis.json` uses:
`chapters`, `sentences`, `subjects`, `segments`, `invalid`, `qas`, `topics`, `words`, `silents`, `keywords`, `key_sentences`, `templates`, `audio_info`.

Sentence objects:
`{"index":1,"start":0.0,"end":3.2,"text":"字幕"}`

Segment objects:
`{"title":"片段标题","description":"说明","thoughts":"剪辑思路","start":1,"end":8}`

Invalid objects:
`{"index":1,"reason":"直播间欢迎语"}`

Clip plan objects:
`{"index":1,"title":"片段标题","sentence_start":1,"sentence_end":8,"start":12.3,"end":51.0,"duration":38.7,"output":"...mp4","fast_cut_shell":"ffmpeg ...","accurate_cut_shell":"ffmpeg ...","parts":[...],"transcript":[...]}`

Multi-part subject clip fields:
`parts`, `concat_list_path`, `concat_list_content`, `concat_shell`, `concat_args`, `script_notes`.

Clip manifest is a JSON array of delivered clip objects with status, method, output, duration, actual_duration_seconds, transcript, and optional error fields. `clip_notes_markdown` contains ready-to-write Markdown notes for each clip: `{"index":1,"title":"片段","markdown_path":"...md","markdown":"..."}`.

## Cutting Rules

- Keep transcript index boundaries intact; never invent timestamps.
- Prefer valid sentences after removing `invalid` indexes.
- Add small tail padding manually only when it improves speech naturalness.
- Use `-c copy` first for speed, then retry with re-encoding if timestamp accuracy is poor.
- For subject clips, preserve selected sentence order unless the LLM explicitly provides a better narrative order.
- Do not cut live-only greetings, thanks, countdowns, real-time stock claims, or room-specific promos into short videos unless the user explicitly asks.
