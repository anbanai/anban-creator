# 结构化验证方法论

## 验证原则

验证的目的是确保跨图颜色一致性。不是模糊的「看起来差不多」，而是逐实体逐部位的精确比对。

当前 `generate_image` 不是专用 img2img/colorize_lineart 工具。验证结论必须区分：
- **颜色一致性**：是否符合 Color Bible。
- **线稿保持风险**：是否出现线条重绘、构图偏移、比例变化或元素增删。

如果颜色问题需要“只改颜色不动线稿”才能修复，标记 `needs_img2img`，不要把重新生成说成严格修正。

## 图像分析方法

**所有图像视觉分析通过 `analyze_image` MCP 工具执行，不依赖 Read 的视觉能力。**

- 对 generate_image 生成的图：使用返回的 `file_path`（服务器端路径）传 `file_path` 参数
- 对已有 CDN URL 的图：传 `image_url` 参数
- 若 `file_path` 分析因 10MB 限制失败，先 `compress_image`，仍失败则 `upload_image` 后用 `image_url` 重试。
- analyze_image 一次只分析一张图片；同时传 `file_path` 和 `image_url` 时服务端只会使用 `file_path`，不会做双图比较。
- 线稿保持验证必须先为原始线稿生成线稿指纹，再分析上色图，将上色图审计结果与线稿指纹逐项比对。

## 三级验证

### Level 1 — 单图验证（每张上色图完成后）

每生成一张上色图（或选定最优候选后），立即执行单图验证。

**步骤**：

1. 调用 `analyze_image(channel_id="$CHANNEL_ID", file_path=上色图服务器端路径, prompt="逐实体逐部位描述颜色。对图中每个角色/物体，列出所有可见部位并描述每个部位的颜色。")` → 获取颜色描述
2. 对 Color Bible 中出现在此图的每个实体：
   - 逐部位比对返回的颜色描述与 Color Bible 定义
3. 对每个部位评级

**评级标准**：

| 级别 | 定义 | 示例 |
|------|------|------|
| PASS | 颜色与 Color Bible 定义一致 | Color Bible: "deep dark chocolate brown" → 观察: "deep dark brown" |
| MINOR | 色调正确但饱和度/明度有轻微偏差 | Color Bible: "bright cherry red" → 观察: "slightly darker red, still clearly red" |
| FAIL | 色调错误 | Color Bible: "deep navy blue" → 观察: "appears black" 或 "appears royal blue" |

4. 线稿完整性验证分两次单图分析：
   - 原始线稿：调用 `analyze_image(channel_id="$CHANNEL_ID", image_url=原始线稿CDN_URL, prompt="只描述这张原始线稿的可验证线稿指纹，不评论颜色。包括：画面宽高方向、主体数量、主体位置、姿态、轮廓关键线、服装/道具/背景线条、构图边界、容易被重绘或丢失的小线条。")` → 写入 `$DIR/lineart-fingerprints.md`
   - 上色图：调用 `analyze_image(channel_id="$CHANNEL_ID", file_path=上色图服务器端路径, prompt="只描述这张上色图的线条和构图状态，不评论颜色。按原始线稿指纹逐项检查：画面宽高方向、主体数量、主体位置、姿态、轮廓关键线、服装/道具/背景线条、构图边界、小线条是否存在。输出 PASS/MINOR/FAIL，并列出任何线条重绘、模糊、构图偏移、比例变化或元素增删。")`
   - 将上色图审计结果与线稿指纹逐项比对；不能确认时标记 `needs_img2img`

**产出**：更新 `$DIR/best-refs.md` 中的质量评估。

### Level 2 — 跨图一致性审计（全部上完后）

对每张已上色图调用 `analyze_image`，对 Color Bible 中每个跨图实体进行跨图比对。

**步骤**：

1. 对每个 `$DIR/colored_NN.png`：
   - 调用 `analyze_image(channel_id="$CHANNEL_ID", file_path=服务器端路径, prompt="逐实体逐部位描述颜色，与以下 Color Bible 规格比对并标注 PASS/MINOR/FAIL：[Color Bible 内容]")`
2. 对 Color Bible 中每个实体：
   - 列出该实体出现的所有图
   - 逐图逐部位比对
   - 生成一致性行

**产出**：`$DIR/consistency-report.md`

格式示例：

```markdown
# Consistency Report

## Entity: Little Girl

| Image | Hair | Skin | Cape | Dress | Shoes | Overall |
|-------|------|------|------|-------|-------|---------|
| colored_00 | ✅ PASS | ✅ PASS | ✅ PASS | ✅ PASS | ✅ PASS | ✅ PASS |
| colored_01 | ✅ PASS | ✅ PASS | ✅ PASS | ✅ PASS | — (not visible) | ✅ PASS |
| colored_02 | ✅ PASS | ⚠️ MINOR | ✅ PASS | ✅ PASS | ❌ FAIL | ❌ FAIL |

Details:
- colored_02/skin: appears slightly more pink than "warm peachy beige"
- colored_02/shoes: appears black instead of "warm chestnut brown"

## Entity: Big Wolf

| Image | Fur | Eyes | Nose | Overall |
|-------|-----|------|------|---------|
| colored_00 | ✅ PASS | ✅ PASS | ✅ PASS | ✅ PASS |
| colored_01 | ✅ PASS | ✅ PASS | ✅ PASS | ✅ PASS |

## Summary
- Total entity-appearances: 5
- PASS: 4
- MINOR: 1 (colored_02/Little Girl/skin)
- FAIL: 1 (colored_02/Little Girl/shoes)
- Overall pass rate: 80%
```

### Level 3 — 收敛修正后审计（每轮修正后）

与 Level 2 相同，但聚焦于：
- 本轮修正的实体是否改善了
- 是否引入了新的不一致
- best-ref 是否需要更新
- **线稿是否在修正过程中被修改**（模型有时修正颜色会连带改线）

---

## Per-Entity Best Reference 追踪

### 追踪表格式

`$DIR/best-refs.md`：

```markdown
# Best Reference Tracking

## Entity: Little Girl
- best_ref: colored_00.png
- quality: hair=PASS, skin=PASS, cape=PASS, dress=PASS, shoes=PASS
- appearances: colored_00, colored_01, colored_02
- notes: anchor image, all colors perfect

## Entity: Big Wolf
- best_ref: colored_01.png
- quality: fur=PASS, eyes=PASS, nose=PASS
- appearances: colored_00, colored_01
- notes: colored_01 has slightly better fur texture detail than colored_00
```

### 更新规则

1. **每完成一张上色图**：
   - 评估该图中每个实体的颜色质量
   - 如果某实体的颜色质量比当前 best_ref 更好 → 更新 best_ref
   - "更好"的定义：PASS 数量更多，或相同 PASS 数但颜色渲染更精确

2. **每轮修正后**：
   - 重新评估修正后的图
   - 如果修正产生了更好的版本 → 更新 best_ref

3. **回溯统一时**：
   - 检查 best_ref 是否在修正过程中发生了变化
   - 如果变了，标记需要回溯的图

### "更好"的判断标准

- **更精确**：颜色描述与 Color Bible 更一致
- **更稳定**：同一实体多个部位都 PASS
- **更清晰**：颜色边界清晰，无渗色

优先选：PASS 数量多的 > 部位渲染更精确的 > 构图更清晰的

---

## 一致性评级标准

### PASS

颜色与 Color Bible 定义一致。具体判断：
- 色调完全匹配（"cherry red" → 看起来确实是樱桃红）
- 饱和度一致（不偏暗也不偏亮）
- 跨图比较时，同一部位在两张图中颜色无可见差异

### MINOR

色调正确但有轻微偏差。具体判断：
- 色调正确（确实是红色，不是橙色）
- 但饱和度或明度有轻微偏差（稍暗/稍亮/稍淡）
- 跨图比较时能看出差异，但不影响识别

**MINOR 是否需要修正**：
- 如果 MINOR 只出现在 1 张图，其他图都 PASS → 建议修正
- 如果 MINOR 出现在多张图但程度相似 → 可能是模型对该颜色的默认倾向，可以接受
- 如果 MINOR 影响的是主要角色的主色 → 建议修正
- 如果 MINOR 影响的是次要部位/背景元素 → 可以接受

### FAIL

色调错误。具体判断：
- 颜色与定义明显不符（"navy blue" → 看起来是黑色）
- 跨图比较时差异明显
- **必须修正**

---

## 修正触发条件

| 条件 | 动作 |
|------|------|
| 任何 FAIL | 必须修正 |
| 主要角色主色 MINOR | 建议修正 |
| 次要部位 MINOR + 仅 1 张图 | 建议修正 |
| 次要部位 MINOR + 多张图类似 | 可接受 |
| 背景元素 MINOR | 可接受 |
| 线稿被修改 | 标记 `needs_img2img`；只有用户接受 best-effort 时才重新生成 |

## 修正策略

### FAIL 修正

1. 确定该实体的当前 best_ref
2. 用 best_ref 的服务器端 `file_path` 作 `ref_image_path`
3. 构建 correction prompt（明确指出偏差 + 正确值）
4. 默认生成 1 个候选；质量优先模式生成 2 个候选选最优
5. 验证修正结果

### MINOR 修正

1. 用当前图的原 prompt
2. 增加反面约束（"must NOT be slightly too dark, must be exact [color]"）
3. 用该实体 best_ref 作参考
4. 生成 1 个候选
5. 验证修正结果

### 修正 Prompt 模板

```
CORRECTION PASS for color inconsistency.

The reference image shows the CORRECT color scheme.

ENTITY: [Entity name]
SPECIFIC ISSUES TO FIX:
- [element] should be [语义色名] (currently appears [错误色描述])
  CORRECT: [详细色名 + 实物类比]
  WRONG: [反面约束]

Use the reference image's colors EXACTLY for [Entity].
The result must be visually indistinguishable from the reference
in terms of [Entity]'s colors.

CRITICAL LINE PRESERVATION: Every line, stroke, and proportion must remain
100% identical to the original line art. Do NOT modify, blur, redraw, add,
or remove any lines. Only change the COLOR of [Entity], nothing else.
```

上面的 line preservation 文案是 prompt 约束，不是能力承诺。若输出仍改变线稿，记录 `needs_img2img`。

### 修正后验证

修正后必须通过 `analyze_image` 重新验证：
1. 修正的实体是否颜色正确了
2. 其他实体是否被影响（模型有时改一个会影响其他）
3. 线稿构图是否保持完整；若被修改，标记 `needs_img2img`

如果修正引入了新问题（其他实体被影响），需要在新一轮修正中一并处理。

## 收敛判断

每轮修正后审计，与上一轮比较：

| 本轮结果 | 动作 |
|----------|------|
| 全部 PASS | 收敛成功，退出循环 |
| FAIL 数减少 | 继续下一轮修正 |
| FAIL 数不变但位置变化 | 继续下一轮（换修正策略） |
| FAIL 数不变且位置不变 | 停止循环，标记人工复核 |
| FAIL 数增加 | 回退本轮修正，停止循环 |
| 线稿风险升高 | 停止循环，标记 `needs_img2img` |

最大轮次：3 轮。超过后未解决项标记 `needs_manual_review`；需要局部保线稿换色的项标记 `needs_img2img`。

## 回溯触发

在以下情况下触发回溯：

1. 修正后某实体的 best_ref 从 `colored_XX` 变成了 `colored_YY`
2. 且 `colored_XX` 中也包含该实体
3. 那么 `colored_XX` 需要用 `colored_YY` 作参考重新上色

回溯范围：只回溯包含该实体、且非 best_ref 的图。

回溯后重新审计确认。若回溯会明显改变线稿，停止并标记 `needs_img2img`。
