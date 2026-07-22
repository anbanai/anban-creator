# 身份锁提取模板

## Contents

- [调用语法](#调用语法)
- [12 维度身份锁提取 prompt 模板](#12-维度身份锁提取-prompt-模板)
- [1. 脸型](#1-脸型)
- [2. 五官比例](#2-五官比例)
- [3. 眼睛形状](#3-眼睛形状)
- [4. 鼻子形状](#4-鼻子形状)
- [5. 嘴型](#5-嘴型)
- [6. 眉毛](#6-眉毛)
- [7. 发型](#7-发型)
- [8. 发色](#8-发色)
- [9. 肤色](#9-肤色)
- [10. 年龄感](#10-年龄感)

本文档定义 Phase 1 步骤 3 提取身份锁时的 `analyze_image` prompt 模板，以及身份锁的输出格式。

## 调用语法

```
analyze_image(
  project_id="$PROJECT_ID",
  file_path="$PORTRAIT_SERVER_PATH",
  prompt=<下方完整模板>
)
```

## 12 维度身份锁提取 prompt 模板

直接复制粘贴这段作为 `prompt` 参数：

```
分析这张参考人像图，提取 12 个身份维度的具体描述。这份描述将作为"身份锁"，用于生成同一人物的不同姿态变体——所有变体必须严格匹配这 12 个维度。

输出时每个维度独立成段，提供具体、可观测、可复现的描述。**用语义描述而非数值**（如"暖白色"而非"#FFEEDD"）。

## 1. 脸型
描述脸的整体轮廓和比例：
- 整体形状（圆 / 方 / 长 / 瓜子 / 心形 / 椭圆）
- 颧骨宽度（宽 / 中等 / 窄）
- 下颌线条（清晰 / 柔和 / 方下颌 / 尖下巴）
- 脸长宽比（偏长 / 均衡 / 偏圆）

## 2. 五官比例
描述五官在脸部的相对位置和比例：
- 三庭比例（上庭/中庭/下庭是否均等）
- 五眼宽度（两眼间距、眼到脸部边缘的比例）
- 五官大小倾向（精致小巧 / 大气舒展）

## 3. 眼睛形状
- 眼型（杏眼 / 桃花眼 / 凤眼 / 圆眼 / 细长眼）
- 双/单眼皮
- 眼裂大小（大 / 中 / 小）
- 眼尾走向（上挑 / 平直 / 下垂）
- 瞳色（深棕 / 浅棕 / 黑）

## 4. 鼻子形状
- 鼻梁高度（高挺 / 中等 / 塌鼻梁）
- 鼻头形状（圆润 / 尖挺 / 略宽）
- 鼻翼宽度（窄 / 中等 / 宽）
- 整体鼻型（直鼻 / 水滴鼻 / 希腊鼻）

## 5. 嘴型
- 嘴唇厚度（薄唇 / 中等 / 厚唇）
- 嘴角走向（上扬 / 平直 / 下垂）
- 唇峰形状（明显 / 柔和）
- 整体嘴型（樱桃小嘴 / 大气厚唇 / 微笑唇）

## 6. 眉毛
- 眉形（柳叶眉 / 剑眉 / 平眉 / 弯月眉 / 野生眉）
- 浓淡（浓密 / 适中 / 稀疏）
- 长度（长眉 / 中等 / 短眉）
- 眉头与眉尾位置

## 7. 发型
- 长度（短发 / 中长发 / 长发 / 超长发）
- 刘海（无 / 齐刘海 / 斜刘海 / 空气刘海 / 中分）
- 卷直（直发 / 微卷 / 大波浪 / 紧卷）
- 层次（齐整 / 有层次 / 短发精灵感）

## 8. 发色
- 主色调（黑色 / 深棕 / 浅棕 / 栗色 / 染色）
- 用语义色名+实物类比（如"深巧克力色，像 70% 黑巧克力，不是纯黑也不是浅棕"）
- 是否有挑染或渐变

## 9. 肤色
- 主色调（白皙 / 偏黄 / 小麦 / 健康古铜 / 蜜色）
- 用语义色名+实物类比（如"暖白色带桃粉调，像奶盖上的浅桃色"）
- 肤质（哑光 / 水光 / 雾面 / 哑光带毛孔感）

## 10. 年龄感
- 视觉年龄范围（如 18-22 / 23-28 / 28-35 / 35+）
- 气质成熟度（少女感 / 轻熟 / 成熟知性 / 优雅）
- 皮肤弹性暗示（紧致 / 自然 / 略显成熟）

## 11. 气质
- 主导气质（甜美 / 御姐 / 文艺 / 元气 / 高冷 / 知性 / 性感 / 中性）
- 次要气质（如"主甜美，次元气"）
- 给人的整体感觉（用 2-3 个形容词）

## 12. 神态特征
- 标志性表情倾向（如"嘴角总带一点笑意"、"眼神总带一点忧郁"）
- 静态状态下的神态（宁静 / 警觉 / 慵懒 / 自信）
- 让这张脸"独特"的微小特征（如"左眼比右眼稍大"、"嘴角左侧略高"）

输出格式：每个维度用 ## 标题分节，每节 2-4 句话具体描述。重点：这些描述必须足够具体，使得生成变体时可以严格对照检查。
```

## 输出落地

`analyze_image` 返回的文本直接作为 `$DIR/identity-lock.md` 的正文，文件开头加：

```markdown
# Identity Lock

## Source

- file: $DIR/input-manifest.md 中的 reference_portrait
- analyzed_at: <时间戳>
- portrait_server_path: $PORTRAIT_SERVER_PATH

## Purpose

这份身份锁是所有姿态变体的"基准参照"。每张变体生成后必须**逐维度比对**——任一关键维度（脸型/五官比例/发型发色）FAIL 都需重试。

---

<analyze_image 返回的 12 维度描述>

## Locked Constants (Prompt 抄写版)

把上述 12 维度转成英文 prompt 友好格式，供 Phase 3 步骤 5a 构建变体 prompt 时直接抄写：

- Face shape: <oval with defined jawline, NOT round>
- Facial proportions: <balanced three-tenths proportions>
- Eyes: <almond-shaped, double eyelids, medium eye opening>
- Nose: <medium bridge, rounded tip>
- Mouth: <medium lips, slight upward corners>
- Eyebrows: <arched, medium thickness>
- Hair: <shoulder-length wavy curls with side-swept bangs, NOT straight>
- Hair color: <dark chocolate brown, NOT black>
- Skin: <fair with peach warmth, semi-matte>
- Age vibe: <mid-20s, youthful professional>
- Aura: <confident, slightly playful>
- Signature demeanor: <eyes carry subtle smile>
```

## 重要：身份锁的"完整性 > 简洁性"

身份锁的描述宁详不简。每个维度提供**多个观察角度**（如脸型同时描述形状 + 颧骨 + 下颌 + 长宽比），这样：

1. 生成变体时模型有足够锚点
2. 审计时可以多角度比对
3. 重试时知道具体哪个子维度需要加强

但**最终拼入变体 prompt 时**（Phase 3 步骤 5a），把每个维度压缩到 1 行核心描述，避免 prompt 超 500 词。

## 与一致性审计的关系

`identity-lock.md` 是 Phase 4 一致性审计的**比对基准**。每张变体的 12 维度审计结果与这份锁的描述逐项比对，得出 PASS/MINOR/FAIL 评级。

详见 [consistency-audit.md](consistency-audit.md)。
