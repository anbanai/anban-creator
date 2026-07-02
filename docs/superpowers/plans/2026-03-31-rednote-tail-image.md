# 种草笔记尾图独立规范实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将种草笔记尾图从内容图规范中拆分为独立文件 `references/tail.md`，支持三种尾图类型（关注/评论/引流），尾图单独生成为 `tail.png`。

**Architecture:** 新建 `tail.md` 与 `cover.md`/`content.md` 三者对称。尾图从 `--count` 批量生成中分离，改为单独一次调用输出 `tail.png`。Agent 在 image-plan 阶段根据内容主题自动匹配尾图类型。

**Tech Stack:** Markdown skill 文件，无代码变更

---

## 文件变更总览

| 文件 | 操作 | 职责 |
|------|------|------|
| `skills/seednote-visual-design/references/tail.md` | **新建** | 尾图设计规范（三种类型 + 匹配规则 + prompt 模板） |
| `skills/seednote-visual-design/SKILL.md` | 修改 | 新增尾图规范引用，更新 CLI 命令 |
| `skills/seednote-visual-design/references/content.md` | 修改 | 删除尾图规范，更新 `--count` 和 image-plan 模板 |
| `agents/seednote.md` | 修改 | 更新图片检查逻辑和命名 |

---

### Task 1: 新建尾图规范文件 tail.md

**Files:**
- Create: `skills/seednote-visual-design/references/tail.md`

- [ ] **Step 1: 创建 tail.md，包含完整的尾图设计规范**

文件内容包含以下章节：
1. 自动匹配规则表（内容类型 → 尾图类型）
2. 三种尾图类型的详细规范（关注/评论/引流），每种包含：目标、内容策略、视觉规范
3. 尾图 prompt 模板（通用结构 + 三种类型的 `tail_type_specific_instructions`）
4. CLI 命令（尾图单独生成）
5. image-plan.md 中的尾图规划模板

```markdown
# 种草笔记尾图设计规范

## 尾图的唯一使命

尾图是用户滑完所有内容后的最后一屏。它不做知识传递，只做一件事：**触发一个互动动作**（关注/评论/引流）。三类尾图策略完全不同，根据内容主题自动匹配。

---

## 自动匹配规则

| 内容类型 | 匹配尾图类型 | 判断依据 |
|----------|------------|----------|
| 知识干货、教程、合集 | **关注**（follow） | 用户想持续获取同类内容 |
| 测评、对比、好物推荐 | **评论**（comment） | 用户有观点想表达，评论自然度高 |
| 种草、好物分享、品牌推荐 | **引流**（traffic） | 内容本身带有商业属性，用户想了解更多 |

Agent 在 image-plan 阶段根据内容主题自动判断，无需用户指定。

---

## 类型一：关注引导（follow）

**目标**：让用户产生"这里还有更多好内容"的印象，自然点关注。

**内容策略**：
- 2-3 条前瞻性内容预告（"下期讲XX"、"收藏夹里还有XX"）
- 一句氛围收束语（营造"这只是开始"的感觉）
- 禁止直接说"关注我"、"点关注"、"去主页"

**视觉规范**：
- 文字占比 15-25%，留白 45-55%
- 上/中/下三段式布局
- 适度克制，营造"未完待续"的呼吸感
- 与内容页风格一致但不过度花哨

---

## 类型二：评论引导（comment）

**目标**：激发用户表达欲，产生真实评论互动（评论触发算法推荐）。

**内容策略**：
- 一个与笔记内容强相关的开放式问题
- 或一个有争议/讨论空间的观点
- 禁止泛泛的"你怎么看"类问题

**视觉规范**：
- 文字占比 25-35%
- 问题文字居中偏上，视觉焦点集中在问题本身
- 可用对话框/气泡等元素增加互动感
- 风格活泼，激发表达欲

---

## 类型三：引流引导（traffic）

**目标**：引导用户到站内搜索/主页或暗示性引导到私域。

**内容策略**：
- 站内引流：暗示性关键词引导（"搜XX能找到更多"、"主页有XX合集"）
- 私域引流：暗示性内容引导（"想知道在哪买？看图二"、"评论区发XX"）
- 引导语与笔记内容自然关联

**安全红线**：
- 禁止二维码、微信号、手机号、外部链接
- 禁止"加微信"、"私聊我"等直接引导词
- 用暗示和间接方式替代直接引导

**视觉规范**：
- 文字占比 20-30%，核心信息居中
- 可用暗示性图标（放大镜暗示搜索，信封暗示私信）
- 风格克制，避免广告感

---

## Prompt 模板

尾图单独生成，根据匹配类型使用对应的 prompt 结构：

```
页面类型：尾图（{tail_type}），种草笔记信息流最后一页
风格延续：{style}（与封面保持一致）

平台规范：
- 3:4 竖版构图
- 与内容页风格一致但适度克制
- 真实照片背景

{tail_type_specific_instructions}

内容主题：{topic}
全文概要：{full_outline}
```

### 关注型 tail_type_specific_instructions

```
尾图目的：关注引导
- 2-3 条前瞻内容预告，营造"还有更多"的感觉
- 一句氛围收束语，不说"关注"
- 文字占比 15-25%，留白 45-55%，呼吸感
- 上/中/下三段式布局
```

### 评论型 tail_type_specific_instructions

```
尾图目的：评论引导
- 一个与笔记内容强相关的开放式问题
- 问题居中偏上，视觉焦点集中在问题
- 文字占比 25-35%，可用对话框/气泡元素
- 风格活泼，激发表达欲
```

### 引流型 tail_type_specific_instructions

```
尾图目的：引流引导（{traffic_direction}）
- {traffic_direction}：站内搜索/主页合集 或 暗示性私域引导
- 引导语与笔记内容自然关联
- 文字占比 20-30%，核心信息居中
- 可用放大镜/信封等暗示性图标
- 绝对禁止：二维码、微信号、手机号、外部链接
```

---

## CLI 命令

```bash
# 单独生成尾图（与封面风格一致）
anban-creator image generate "{tail_prompt}" --mode xhs --ref ./cover.png -o ./tail.png
```

> **关键规则**：尾图单独生成，不使用 `--count`。输出固定命名为 `tail.png`。

---

## image-plan.md 尾图规划模板

```markdown
## tail [尾部] 类型：{follow|comment|traffic}

匹配依据：{为什么选这个类型}

- 内容点: （根据类型填充，关注型=预告+收束语，评论型=问题，引流型=引导语）
```
```

- [ ] **Step 2: 验证文件存在且内容完整**

Run: `cat skills/seednote-visual-design/references/tail.md | head -5`
Expected: 文件以 `# 种草笔记尾图设计规范` 开头

- [ ] **Step 3: Commit**

```bash
git add skills/seednote-visual-design/references/tail.md
git commit -m "feat(seednote): add tail image design spec with follow/comment/traffic types"
```

---

### Task 2: 修改 content.md — 删除尾图规范，更新引用

**Files:**
- Modify: `skills/seednote-visual-design/references/content.md`

- [ ] **Step 1: 删除三阶段目标表中的尾页行和尾页设计规范章节**

删除第 9 行（尾页行）：
```markdown
| **尾页** `[tail]` | image_0{N-1} | 互动率 | 隐晦关注引导 + 氛围收束 |
```

删除第 52-68 行（整个"## 尾页设计规范"章节，含前面的 `---` 分隔线）。

将三阶段目标表简化为只保留封面和内容页：

```markdown
## 两类图片目标

| 图片类型 | 优化目标 | 内容策略 |
|----------|----------|----------|
| **封面** | 点击率 | 见 cover.md |
| **内容页** | 完读率 | 每页 2-3 个结构化信息点 |

尾图设计规范见 [tail.md](tail.md)。
```

- [ ] **Step 2: 更新 image-plan.md 模板中的尾图部分**

将第 106-111 行：
```markdown
## image_0{N-1} [尾部]

- 引导点1: （隐含关注动机的内容，20-40字）
- 引导点2:
- 氛围收束句: （不直接说"关注"，而是营造"想看更多"的感觉）
```

替换为：
```markdown
## tail [尾部] 类型：{follow|comment|traffic}

匹配依据：{为什么选这个类型}
- 内容点: （见 tail.md 规范，根据类型填充）
```

- [ ] **Step 3: 更新 --count 基数说明和 CLI 命令**

将第 75 行：
```markdown
**图片总数**：N 张（奇数，3-7 张），N 即后续 `--count` 基数（内容图 = N-1 张，封面单独生成）。
```

替换为：
```markdown
**图片总数**：N 张（奇数，3-7 张），其中封面 1 张 + 内容图 N-2 张 + 尾图 1 张。封面和尾图单独生成，内容图使用 `--count N-2` 批量生成。
```

将第 139-141 行 CLI 命令：
```bash
anban-creator image generate "{paged_prompt}" --mode xhs --count N-1 --ref ./cover.png -o ./
# 输出自动命名为 image_01.png, image_02.png ... image_0{N-1}.png
```

替换为：
```bash
anban-creator image generate "{paged_prompt}" --mode xhs --count N-2 --ref ./cover.png -o ./
# 输出自动命名为 image_01.png, image_02.png ... image_0{N-2}.png
# 尾图单独生成见 tail.md
```

- [ ] **Step 4: 验证修改结果**

Run: `grep -c "尾页" skills/seednote-visual-design/references/content.md`
Expected: `0`（所有"尾页"相关内容已删除）

- [ ] **Step 5: Commit**

```bash
git add skills/seednote-visual-design/references/content.md
git commit -m "refactor(seednote): extract tail image spec from content.md to tail.md"
```

---

### Task 3: 修改 SKILL.md — 新增尾图规范引用，更新 CLI 命令

**Files:**
- Modify: `skills/seednote-visual-design/SKILL.md`

- [ ] **Step 1: 在内容图设计规范下方新增尾图设计规范引用**

在第 37 行 `见 [references/content.md](references/content.md)` 和第 39 行 `---` 之间插入：

```markdown

---

## 尾图设计规范

见 [references/tail.md](references/tail.md)
```

- [ ] **Step 2: 更新 CLI 命令部分**

将第 47-52 行：
```markdown
# 批量生成内容图（N-1 张 + 尾图）
anban-creator image generate "{paged_prompt}" --mode xhs --count N-1 -o ./

# 带参考图（保持风格一致）
anban-creator image generate "{prompt}" --mode xhs --cover --ref ./cover.png -o ./cover.png
anban-creator image generate "{paged_prompt}" --mode xhs --count N-1 --ref ./cover.png -o ./
```

替换为：
```markdown
# 批量生成内容图（N-2 张，不含尾图）
anban-creator image generate "{paged_prompt}" --mode xhs --count N-2 -o ./

# 单独生成尾图
anban-creator image generate "{tail_prompt}" --mode xhs --ref ./cover.png -o ./tail.png

# 带参考图（保持风格一致）
anban-creator image generate "{prompt}" --mode xhs --cover --ref ./cover.png -o ./cover.png
anban-creator image generate "{paged_prompt}" --mode xhs --count N-2 --ref ./cover.png -o ./
```

- [ ] **Step 3: 更新关键规则说明**

将第 58 行：
```markdown
**关键规则**：内容图必须用 `--count`，不逐张生成。`--mode xhs` 自动使用 3:4:1K 规格。
```

替换为：
```markdown
**关键规则**：内容图必须用 `--count` 批量生成，尾图单独生成（`tail.png`），封面单独生成（`cover.png`）。`--mode xhs` 自动使用 3:4:1K 规格。
```

- [ ] **Step 4: 验证修改结果**

Run: `grep -c "tail" skills/seednote-visual-design/SKILL.md`
Expected: 至少 3 处引用（尾图规范链接 + CLI 命令 + 关键规则）

- [ ] **Step 5: Commit**

```bash
git add skills/seednote-visual-design/SKILL.md
git commit -m "refactor(seednote): add tail image reference and update CLI commands in SKILL.md"
```

---

### Task 4: 修改 agents/seednote.md — 更新图片检查逻辑和命名

**Files:**
- Modify: `agents/seednote.md`

- [ ] **Step 1: 更新原创模式步骤 5 的图片检查说明**

将第 59 行：
```markdown
   生成后检查每张图片：`$DIR/cover.png`（封面）、`$DIR/image_01.png` ... `$DIR/image_0{N-1}.png`
```

替换为：
```markdown
   生成后检查每张图片：`$DIR/cover.png`（封面）、`$DIR/image_01.png` ... `$DIR/image_0{N-2}.png`（内容图）、`$DIR/tail.png`（尾图）
```

- [ ] **Step 2: 更新成功标准中的内容图检查项**

将第 115 行：
```markdown
- [ ] 所有内容图 `$DIR/image_01.png` ... `$DIR/image_0{N-1}.png` 存在且可访问
```

替换为：
```markdown
- [ ] 所有内容图 `$DIR/image_01.png` ... `$DIR/image_0{N-2}.png` 存在且可访问
- [ ] 尾图 `$DIR/tail.png` 存在且可访问
```

- [ ] **Step 3: 更新文件组织中的图片命名说明**

将第 142 行：
```markdown
- 图片命名：`$DIR/cover.png`, `$DIR/image_01.png` ... `$DIR/image_0{N-1}.png`（N 由 image-plan.md 决定）
```

替换为：
```markdown
- 图片命名：`$DIR/cover.png`（封面）, `$DIR/image_01.png` ... `$DIR/image_0{N-2}.png`（内容图）, `$DIR/tail.png`（尾图）（N 由 image-plan.md 决定）
```

- [ ] **Step 4: 验证修改结果**

Run: `grep "image_0{N-1}" agents/seednote.md`
Expected: 无匹配（所有 `image_0{N-1}` 已替换为 `tail.png` 和 `image_0{N-2}`）

- [ ] **Step 5: Commit**

```bash
git add agents/seednote.md
git commit -m "refactor(seednote): update image naming and checks for separated tail.png"
```
