# 身份一致性审计模板

本文档定义 Phase 3 步骤 5d（逐张审计）和 Phase 4 步骤 6（全量汇总）使用的 `analyze_image` prompt 模板。

## 调用语法

```
analyze_image(
  channel_id="$CHANNEL_ID",
  file_path="$VARIANT_SERVER_PATH",
  prompt=<下方完整比对模板，基准是 identity-lock.md>
)
```

## 12 维度比对 prompt 模板

直接复制粘贴这段作为 `prompt` 参数。**把 `<identity-lock.md 内容>` 替换为实际的身份锁描述**：

```
审计这张刚生成的人像变体图，与参考人像的身份锁逐维度比对。

## Identity Lock（参考人像的基准身份）

<identity-lock.md 的 12 维度描述完整粘贴在这里>

## 审计任务

对当前变体图的每个维度，与上述身份锁比对，给出评级：

- ✅ PASS：维度与身份锁一致
- ⚠️ MINOR：基调正确但有轻微偏差（如色调对但饱和度略不同）
- ❌ FAIL：明显不一致（如脸型变化、发型从卷变直、发色变化）

## 评级维度（12 项）

### 1. 脸型
观察当前变体的脸型，与身份锁的"<脸型描述>"比对。
- PASS / MINOR / FAIL
- 观察依据：<具体差异>

### 2. 五官比例
<同样格式>

### 3. 眼睛形状
<同样格式>

### 4. 鼻子形状
<同样格式>

### 5. 嘴型
<同样格式>

### 6. 眉毛
<同样格式>

### 7. 发型
<同样格式>

### 8. 发色
<同样格式>

### 9. 肤色
<同样格式>

### 10. 年龄感
<同样格式>

### 11. 气质
<同样格式>

### 12. 神态特征
<同样格式>

## 表情/手势符合度（不计入身份一致性，但记录质量）

### 表情符合度
当前变体的表情是否符合本姿态的目标表情（如震惊/自信/疑惑/大笑/严肃/惊喜）？
- PASS / MINOR / FAIL
- 观察依据：

### 手势符合度
当前变体的手势是否符合本姿态的目标手势？特别注意手指数量、关节方向、双手协调。
- PASS / MINOR / FAIL
- 观察依据：<特别注意畸形、多指、扭曲情况>

## 综合评级

- 身份一致性 Overall: PASS / MINOR / FAIL
- 关键维度（脸型/五官比例/发型发色）状态: <PASS/MINOR/FAIL 汇总>
- 是否建议重试: <yes/no>
- 重试原因（若 yes）: <最严重的关键 FAIL 项>
```

## 关键维度判定

身份锁的 12 个维度不是同等重要。**关键维度决定是否重试**：

| 维度 | 关键性 | FAIL 时是否重试 |
|------|--------|----------------|
| 1. 脸型 | 🔴 关键 | 是 |
| 2. 五官比例 | 🔴 关键 | 是 |
| 3. 眼睛形状 | 🟡 重要 | 看严重程度 |
| 4. 鼻子形状 | 🟡 重要 | 看严重程度 |
| 5. 嘴型 | 🟢 一般 | 否（表情会改变嘴型） |
| 6. 眉毛 | 🟢 一般 | 否（表情会改变眉型） |
| 7. 发型 | 🔴 关键 | 是 |
| 8. 发色 | 🔴 关键 | 是 |
| 9. 肤色 | 🟡 重要 | 看严重程度 |
| 10. 年龄感 | 🟡 重要 | 看严重程度 |
| 11. 气质 | 🟢 一般 | 否（气质本身就是综合感） |
| 12. 神态特征 | 🟢 一般 | 否 |

**重试触发条件**：
- 任一 🔴 关键维度 FAIL
- 3 项以上 🟡 重要维度 FAIL
- 5 项以上任意维度 FAIL

**接受不再重试**：
- 全部 PASS
- 关键维度全 PASS，仅 🟢 一般维度 MINOR
- 重试 1 次后仍 FAIL（接受当前最佳并标记 `needs_img2img`）

## 手势畸形判定

手势是 AI 生图的高失败率区域。审计时**特别关注**：

| 畸形类型 | 描述 | 处理 |
|---------|------|------|
| 多指 | 一只手出现 6 个或更多手指 | FAIL，必须重试 |
| 缺指 | 一只手只有 3 个或更少手指 | FAIL，必须重试 |
| 关节扭曲 | 手指弯向不可能的方向 | FAIL，必须重试 |
| 双手融合 | 两只手部分融合在一起 | FAIL，必须重试 |
| 手部模糊 | 手部严重模糊不可辨认 | MINOR，看是否遮挡关键信息 |
| 手部过小 | 手相对身体比例过小 | MINOR，可接受 |

手势 FAIL 重试 1 次仍失败 → 标记 `needs_manual_edit`，建议用户用 PS 修复或换姿态模板。

## 输出落地（逐张）

每张变体的审计结果直接附加到 `$DIR/consistency-report.md` 的对应行（步骤 5d 时追加），或独立保存为 `$DIR/audit_variant_0N.md`（步骤 5d 内部使用）。

## 输出落地（全量汇总）

步骤 6 把所有变体的审计结果汇总到 `$DIR/consistency-report.md`，格式见 SKILL.md 步骤 6 的模板。

汇总表必须包含：
- 每张变体的 12 维度评级
- 表情符合度和手势符合度
- 重试历史
- 能力边界声明

## 重试 prompt 构建方向

针对不同 FAIL 项的 prompt 调整方向：

| FAIL 项 | 重试 prompt 调整 |
|---------|----------------|
| 脸型 FAIL | 身份锁段落加强："face shape MUST be <具体描述>, NOT <当前错误>"; 增加反面约束 |
| 五官比例 FAIL | 加强"三庭五眼比例必须严格保持"约束；增加反面约束"DO NOT change facial proportions" |
| 发型 FAIL | 发型维度加更多细节（卷度、长度、刘海样式）；加强"hair MUST be <具体>, NOT <常见错误>" |
| 发色 FAIL | 用更具体的实物类比"dark chocolate brown like 70% dark chocolate, NOT pure black, NOT milk brown" |
| 肤色 FAIL | 用更具体的实物类比"fair skin with peach warmth like fresh cream, NOT pale white, NOT tanned" |
| 表情不够夸张 | 姿态段落用更具体的描述"eyes WIDE OPEN showing whites, eyebrows raised HIGH" |
| 手势畸形 | 手势段落加更多细节"five fingers visible, palm facing camera, fingers naturally spread"；加强反面约束"DO NOT add extra fingers, DO NOT distort joints" |

重试时 `output_path` 改为 `/tmp/anbanwriter-portrait-pose/$TASK_ID/variant_0N_v2.png`，重试结果走相同审计；若仍 FAIL 则接受当前最佳并标注 `needs_img2img` 或 `needs_manual_edit`。

## 能力边界声明

最终的一致性报告必须包含能力边界声明：

```
## Capability Boundary

当前 generate_image 是参考图生成，不是专用 ID-lock 工具（如 InstantID、PhotoMaker）：
- ref_image_path 能提高身份一致性，不能 100% 锁定人脸
- 同一人物生成 6 张姿态变体时，每张的身份漂移风险独立存在
- 关键维度 FAIL 标记 needs_img2img，建议用户使用专用 ID-lock 工具
- 手势畸形重试后仍失败标记 needs_manual_edit，建议用户用 PS 修复或换姿态模板
```
