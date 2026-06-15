#!/usr/bin/env bash
# SubagentStop 机械闸门：seednote agent 完成前验证产物完整性
# 兼容 Claude Code 与 Codex（Codex 用 PLUGIN_ROOT，Claude Code 用 CLAUDE_PLUGIN_ROOT/CLAUDE_PROJECT_DIR）
#
# 检查清单（任一缺失则 block 强制 agent 继续）：
#   - $DIR/image-plan.md    证明走了 seednote-visual-design skill 的完整规划流程
#   - $DIR/image-prompts.md 证明每次 generate_image 后都追加了 prompt 记录
#   - $DIR/image-review.md  证明跑了 skill Step 6 质量验证
#   - ≥3 张图（cover.png + image_*.png + tail.png）满足平台最低数量

set -euo pipefail

INPUT=$(cat)

# 只对 seednote agent 生效（matcher 已过滤，脚本内再确认一次更稳）
AGENT_TYPE=$(echo "$INPUT" | jq -r '.agent_type // .agent_name // empty' 2>/dev/null || true)
[[ "$AGENT_TYPE" != "seednote" ]] && exit 0

# 找最近被引用的 $DIR：Codex 用 PLUGIN_ROOT/PWD；Claude Code 兼容 CLAUDE_PROJECT_DIR
WORKSPACE_ROOT="${CODEX_PROJECT_DIR:-${PLUGIN_ROOT:-${CLAUDE_PROJECT_DIR:-$PWD}}}"
# 若 PLUGIN_ROOT 指向的是插件缓存目录而非项目根，回退到 PWD
[[ "$WORKSPACE_ROOT" == */plugins/cache/* ]] && WORKSPACE_ROOT="$PWD"
SEEDNOTE_DIR=$(ls -td "$WORKSPACE_ROOT"/output/seednote/*/ 2>/dev/null | head -1 || true)
if [[ -z "$SEEDNOTE_DIR" ]]; then
  SEEDNOTE_DIR=$(find "$WORKSPACE_ROOT/data/workspace" -type d -path "*seednote*" -newermt "-2 hours" 2>/dev/null | head -1 || true)
fi
[[ -z "$SEEDNOTE_DIR" ]] && exit 0  # 没工作目录——可能是更早的运行，跳过

MISSING=()
[[ ! -f "$SEEDNOTE_DIR/image-plan.md" ]]    && MISSING+=("image-plan.md（说明没调用 seednote-visual-design skill 走完整流程）")
[[ ! -f "$SEEDNOTE_DIR/image-prompts.md" ]] && MISSING+=("image-prompts.md（说明 generate_image 调用后没记录 prompt）")
[[ ! -f "$SEEDNOTE_DIR/image-review.md" ]]  && MISSING+=("image-review.md（说明没跑 skill Step 6 质量验证）")

IMG_COUNT=$(find "$SEEDNOTE_DIR" -maxdepth 1 \( -name "cover.png" -o -name "image_*.png" -o -name "tail.png" \) -type f 2>/dev/null | wc -l | tr -d ' ')
[[ "$IMG_COUNT" -lt 3 ]] && MISSING+=("图片数量（当前 $IMG_COUNT 张，应 ≥3：cover + 内容图 + tail）")

if [[ ${#MISSING[@]} -gt 0 ]]; then
  REASON="种子笔记机械闸门未通过，缺失：
$(printf '  - %s\n' "${MISSING[@]}")

请按 seednote-visual-design skill 流程补齐：先生成 image-plan.md（含「必须出现文字」字段），再逐张调用 generate_image（每次追加 image-prompts.md），最后跑 Step 6 写 image-review.md。禁止跳过 skill 直接调 generate_image。"
  jq -nc --arg reason "$REASON" '{decision: "block", reason: $reason}'
fi

exit 0
