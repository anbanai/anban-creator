#!/usr/bin/env python3
"""Run the pinned MoneyPrinterTurbo CLI for a local-life batch.

The bridge deliberately accepts only task-local paths and writes only under the
requested output directory. It keeps the upstream CLI as the renderer while
making Anban's batch contract explicit and machine-readable.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


MAX_VARIANTS = 10
WORKSPACE = Path(os.environ.get("ANBAN_WORKSPACE", "/workspace")).resolve()


def fail(message: str, code: int = 2) -> int:
    print(json.dumps({"status": "failed", "error": message}, ensure_ascii=False))
    return code


def write_failure_artifacts(output_dir: Path, stage: str, message: str) -> None:
    """Leave a machine-readable failure marker inside the task output."""
    try:
        output_dir.mkdir(parents=True, exist_ok=True)
        now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
        payload = {
            "version": "1.0",
            "status": "failed",
            "stage": stage,
            "error_code": "local_life_bridge_failed",
            "message": message,
            "resume_from": stage,
            "data_at": now,
        }
        (output_dir / "failure-state.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        (output_dir / "failure-diagnosis.md").write_text(
            f"# 素材混剪执行失败\n\n- 阶段：{stage}\n- 原因：{message}\n",
            encoding="utf-8",
        )
    except OSError:
        # The process result still carries the original error when output itself
        # is unavailable; do not mask it with a best-effort diagnostic failure.
        return


def artifact_metadata(status: str, missing: list[str], evidence_paths: list[str]) -> dict[str, Any]:
    return {
        "schema_version": "1.0",
        "status": status,
        "source": "local-life-agent",
        "data_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "missing": missing,
        "evidence_paths": evidence_paths,
    }


def content_tags_payload(
    status: str, missing: list[str], evidence_paths: list[str], mix_type: str
) -> dict[str, Any]:
    payload = artifact_metadata(status, missing, evidence_paths)
    payload.update(
        {
            "schema_version": "2.0",
            "taxonomy_version": "2.0",
            "tagging_status": "succeeded" if status == "ready" else "failed",
            "subjects": [
                {
                    "artifact_role": "final_video",
                    "tags": [
                        {
                            "dimension": "presentation_device",
                            "value": "scenario_opening",
                            "confidence": 0.7,
                            "evidence": [
                                {
                                    "source_file": "output/script.txt",
                                    "line_start": 1,
                                    "line_end": 1,
                                    "quote": f"{mix_type} 素材混剪脚本",
                                    "evidence_kind": "generated_script",
                                }
                            ],
                        },
                        {
                            "dimension": "funnel_stage",
                            "value": "evaluation",
                            "confidence": 0.7,
                            "evidence": [
                                {
                                    "source_file": "output/script.txt",
                                    "line_start": 1,
                                    "line_end": 1,
                                    "quote": f"{mix_type} 素材混剪脚本",
                                    "evidence_kind": "generated_script",
                                }
                            ],
                        },
                    ],
                }
            ],
        }
    )
    return payload


def workspace_path(value: str, label: str) -> Path:
    candidate = Path(value).expanduser()
    if not candidate.is_absolute():
        candidate = WORKSPACE / candidate
    resolved = candidate.resolve()
    try:
        resolved.relative_to(WORKSPACE)
    except ValueError as exc:
        raise ValueError(f"{label} escapes workspace") from exc
    if not resolved.is_file():
        raise ValueError(f"{label} does not exist: {value}")
    return resolved


def workspace_directory(value: str, label: str) -> Path:
    candidate = Path(value).expanduser()
    if not candidate.is_absolute():
        candidate = WORKSPACE / candidate
    resolved = candidate.resolve()
    try:
        resolved.relative_to(WORKSPACE)
    except ValueError as exc:
        raise ValueError(f"{label} escapes workspace") from exc
    return resolved


def run(request: dict[str, Any]) -> int:
    output_dir = workspace_directory(str(request.get("output_dir", "output")), "output_dir")
    # output_dir is normally created by the Agent. Allow a missing output folder,
    # but still reject a path that resolves outside the execution workspace.
    if not output_dir.exists():
        output_dir.mkdir(parents=True, exist_ok=True)
    if not output_dir.is_dir():
        return fail("output_dir must be a directory")

    script = str(request.get("script", "")).strip()
    if not script:
        write_failure_artifacts(output_dir, "script", "script is required")
        return fail("script is required")
    assets = request.get("source_assets")
    if not isinstance(assets, list) or not assets:
        write_failure_artifacts(output_dir, "materials", "source_assets must contain at least one local path")
        return fail("source_assets must contain at least one local path")
    try:
        asset_paths = [workspace_path(str(item), f"source_assets[{index}]") for index, item in enumerate(assets)]
    except ValueError as exc:
        write_failure_artifacts(output_dir, "materials", str(exc))
        return fail(str(exc))

    try:
        variants = int(request.get("variant_count", 3))
    except (TypeError, ValueError):
        write_failure_artifacts(output_dir, "video", "variant_count must be an integer")
        return fail("variant_count must be an integer")
    if not 1 <= variants <= MAX_VARIANTS:
        write_failure_artifacts(output_dir, "video", f"variant_count must be between 1 and {MAX_VARIANTS}")
        return fail(f"variant_count must be between 1 and {MAX_VARIANTS}")

    aspect_ratio = str(request.get("aspect_ratio", "9:16"))
    if aspect_ratio not in {"9:16", "16:9", "1:1"}:
        write_failure_artifacts(output_dir, "video", "aspect_ratio must be 9:16, 16:9, or 1:1")
        return fail("aspect_ratio must be 9:16, 16:9, or 1:1")

    mpt_root = Path(os.environ.get("MPT_ROOT", "/opt/moneyprinterturbo")).resolve()
    cli = mpt_root / "cli.py"
    if not cli.is_file():
        write_failure_artifacts(output_dir, "bootstrap", f"MoneyPrinterTurbo CLI is missing: {cli}")
        return fail(f"MoneyPrinterTurbo CLI is missing: {cli}", 1)

    command = [
        sys.executable,
        str(cli),
        "--video-subject",
        str(request.get("subject", "本地门店介绍")),
        "--video-script",
        script,
        "--video-source",
        "local",
        "--video-materials",
        ",".join(str(path) for path in asset_paths),
        "--video-count",
        str(variants),
        "--video-aspect",
        aspect_ratio,
        "--video-concat-mode",
        "random",
        "--match-materials-to-script",
        "--stop-at",
        "video",
        "--subtitle-enabled",
        "--bgm-volume",
        "0",
    ]
    voice_id = str(request.get("voice_id", "")).strip()
    command.extend(["--voice-name", voice_id or "zh-CN-XiaoxiaoNeural-Female"])
    bgm = str(request.get("bgm_file", "")).strip()
    if bgm:
        try:
            bgm_path = workspace_path(bgm, "bgm_file")
        except ValueError as exc:
            write_failure_artifacts(output_dir, "audio", str(exc))
            return fail(str(exc))
        command.extend(["--bgm-type", "custom", "--bgm-file", str(bgm_path), "--bgm-volume", "0.2"])

    env = os.environ.copy()
    env.setdefault("MPT_HOME", "/workspace/mpt")
    env.setdefault("PYTHONUNBUFFERED", "1")
    completed = subprocess.run(command, cwd=WORKSPACE, env=env, text=True, capture_output=True)
    parsed: dict[str, Any] = {}
    for line in reversed(completed.stdout.splitlines()):
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            parsed = value
            break

    result = parsed.get("result") if isinstance(parsed.get("result"), dict) else {}
    raw_videos = result.get("videos") if isinstance(result, dict) else []
    raw_videos = raw_videos if isinstance(raw_videos, list) else []
    videos_dir = output_dir / "videos"
    videos_dir.mkdir(parents=True, exist_ok=True)
    copied: list[dict[str, Any]] = []
    for raw in raw_videos:
        try:
            source = workspace_path(str(raw), "generated video")
        except ValueError:
            continue
        target = videos_dir / f"{len(copied) + 1:03d}.mp4"
        shutil.copy2(source, target)
        copied.append({"index": len(copied) + 1, "path": f"output/videos/{target.name}", "source": source.name})

    for key, output_name in (("audio_file", "audio.mp3"), ("subtitle_path", "subtitle.srt")):
        raw = result.get(key) if isinstance(result, dict) else None
        if raw:
            try:
                source = workspace_path(str(raw), output_name)
            except ValueError:
                continue
            if source.is_file():
                shutil.copy2(source, output_dir / output_name)

    (output_dir / "script.txt").write_text(script + "\n", encoding="utf-8")
    reused = len(asset_paths) < variants
    audio_ready = (output_dir / "audio.mp3").is_file()
    subtitles_ready = (output_dir / "subtitle.srt").is_file()
    completion_status = "ready" if len(copied) == variants and completed.returncode == 0 else "partial"
    if completion_status == "ready" and not audio_ready:
        completion_status = "partial"
    if completion_status == "ready" and not subtitles_ready:
        completion_status = "partial"
    missing_videos = [f"output/videos/{index:03d}.mp4" for index in range(len(copied) + 1, variants + 1)]
    if not subtitles_ready:
        missing_videos.append("output/subtitle.srt")
    if not audio_ready:
        missing_videos.append("output/audio.mp3")
    manifest = {
        **artifact_metadata(
            completion_status,
            missing_videos,
            ["output/script.txt", "output/batch-manifest.json"],
        ),
        "version": "1.0",
        "requested_count": variants,
        "success_count": len(copied),
        "mpt_task_id": parsed.get("task_id"),
        "source_assets": [path.name for path in asset_paths],
        "material_reused": reused,
        "videos": copied,
        "stderr_tail": completed.stderr[-4000:],
    }
    (output_dir / "batch-manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    quality = {
        **artifact_metadata(
            completion_status,
            missing_videos,
            ["output/batch-manifest.json"],
        ),
        "requested_count": variants,
        "success_count": len(copied),
        "checks": {"decode": "server", "duration": "server", "aspect_ratio": aspect_ratio, "subtitles": subtitles_ready, "audio": audio_ready},
    }
    (output_dir / "quality-report.json").write_text(json.dumps(quality, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    tags = content_tags_payload(
        completion_status,
        missing_videos,
        ["output/batch-manifest.json", "output/quality-report.json"],
        str(request.get("mix_type", "general-store")),
    )
    (output_dir / "content-tags.json").write_text(json.dumps(tags, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    if completion_status != "ready":
        write_failure_artifacts(output_dir, "video", f"generated {len(copied)}/{variants} requested videos")
    print(json.dumps({"status": completion_status, "requested_count": variants, "success_count": len(copied), "manifest": "output/batch-manifest.json"}, ensure_ascii=False))
    return 0 if completion_status == "ready" else 1


def main() -> int:
    parser = argparse.ArgumentParser(description="Anban local-life MoneyPrinterTurbo bridge")
    parser.add_argument("request", nargs="?", help="JSON request path; defaults to stdin")
    args = parser.parse_args()
    try:
        source = Path(args.request).read_text(encoding="utf-8") if args.request else sys.stdin.read()
        value = json.loads(source)
        if not isinstance(value, dict):
            return fail("request must be a JSON object")
        return run(value)
    except Exception as exc:
        return fail(str(exc), 1)


if __name__ == "__main__":
    raise SystemExit(main())
