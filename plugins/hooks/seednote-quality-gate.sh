#!/usr/bin/env bash
# Stop/SubagentStop mechanical gate for the seednote agent.
# Blocks a claimed success when trace or verification is incomplete.

set -euo pipefail

INPUT="$(cat)"
export HOOK_INPUT="$INPUT"
export WORKSPACE_ROOT="${CLAUDE_PROJECT_DIR:-$PWD}"

python3 - <<'PY'
import json
import os
import re
import sys
from pathlib import Path


def block(reason: str) -> None:
    print(json.dumps({"decision": "block", "reason": reason}, ensure_ascii=False))


try:
    payload = json.loads(os.environ.get("HOOK_INPUT", "") or "{}")
except json.JSONDecodeError:
    payload = {}

if payload.get("agent_type") not in ("seednote", "anban:seednote"):
    sys.exit(0)

root = Path(os.environ.get("WORKSPACE_ROOT") or os.getcwd())

failure_candidates = [root / "output" / "failure-state.json"]
for failure_path in failure_candidates:
    if not failure_path.is_file():
        continue
    try:
        failure = json.loads(failure_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        block(f"种子笔记失败态文件无效（{failure_path}）：{exc}")
        sys.exit(0)
    required_failure_fields = ("status", "stage", "error_code", "message", "resume_from")
    missing_failure_fields = [name for name in required_failure_fields if not failure.get(name)]
    if failure.get("status") != "recoverable_failure" or missing_failure_fields:
        block(
            f"种子笔记失败态文件不完整（{failure_path}）：status 必须为 recoverable_failure，"
            f"缺失字段={missing_failure_fields}"
        )
        sys.exit(0)
    # A structured recoverable failure is an honest terminal outcome. The
    # server rejects it as business success while preserving uploaded files.
    sys.exit(0)

output_dir = root / "output"
if not output_dir.is_dir():
    block(
        "种子笔记机械闸门：未找到规范任务输出目录 output/。\n"
        '请先执行 prepare_workspace(content_type="seednote", task_id=$TASK_ID)，'
        "并将所有交付物直接留在规范 output/ 目录。"
    )
    sys.exit(0)

seednote_dir = output_dir
missing: list[str] = []

required_artifacts = {
    "content.md": "最终正文",
    "request-analysis.json": "结构化需求分析",
    "request-analysis.md": "可读需求分析",
    "reference-analysis.json": "结构化参考素材分析",
    "reference-analysis.md": "可读参考素材分析",
    "image-plan.md": "视觉规划",
    "image-prompts.md": "生成记录",
    "image-review.md": "视觉核验记录",
    "reference-usage-summary.json": "参考素材与视觉核验汇总",
}
for name, purpose in required_artifacts.items():
    if not (seednote_dir / name).is_file():
        missing.append(f"{name}（缺少{purpose}）")

plan_path = seednote_dir / "image-plan.md"
if plan_path.is_file():
    plan = plan_path.read_text(encoding="utf-8", errors="replace")
    match = re.search(r"计划图片数量[:：]\s*(\d+)", plan)
    if not match:
        missing.append("image-plan.md 缺「计划图片数量」字段（说明 skill 步骤 3 未执行）")
    else:
        expected = int(match.group(1))
        images = [
            p
            for p in seednote_dir.iterdir()
            if p.is_file()
            and (p.name == "cover.png" or p.name == "tail.png" or re.fullmatch(r"image_.*\.png", p.name))
        ]
        image_count = len(images)
        if image_count != expected:
            missing.append(f"图片数量（当前 {image_count} 张，应等于 image-plan.md 声明的 {expected} 张）")
        if not (seednote_dir / "cover.png").is_file():
            missing.append("cover.png（封面必选）")
        content_count = len([p for p in images if p.name.startswith("image_")])
        if content_count > 3:
            missing.append(f"内容图超过 3 张上限（当前 {content_count} 张，应 ≤3）")

summary_path = seednote_dir / "reference-usage-summary.json"
if summary_path.is_file():
    try:
        summary = json.loads(summary_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        missing.append(f"reference-usage-summary.json 无法解析：{exc}")
    else:
        outputs = summary.get("outputs")
        if not isinstance(outputs, list) or not outputs:
            missing.append("reference-usage-summary.json.outputs 为空，未记录逐图核验结果")
        else:
            for output in outputs:
                filename = output.get("file_name") or "<unknown>"
                verification = output.get("verification") or {}
                if verification.get("passed") is not True:
                    missing.append(f"{filename} 视觉核验未通过（passed={verification.get('passed')!r}）")

if missing:
    block(
        f"种子笔记机械闸门未通过（{seednote_dir}），缺失：\n"
        + "".join(f"  - {item}\n" for item in missing)
        + "\n请完成 seednote-visual-design 规划和 generate_image 原子视觉核验，并将全部产物留在 output/。"
        + "若依赖不可用，写入结构化 failure-state.json 后停止；禁止用 prompt 质量或文件尺寸代替视觉核验。"
    )

sys.exit(0)
PY
