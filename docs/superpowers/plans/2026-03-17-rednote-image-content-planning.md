# 小红书图片内容规划步骤 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `agents/rednote.md` 的原创和复刻流程中各插入一个"图片内容规划"步骤（步骤 4.5 / 步骤 5.5），使图片内容由独立的知识规划驱动，而非依赖简洁的文案正文。

**Architecture:** 纯 Markdown 文件修改，不涉及 Go 代码。修改 `agents/rednote.md` 的三个区块：原创流程（插入步骤 4.5，更新步骤 5）、复刻流程（步骤 3 新增第 6 维、插入步骤 5.5、更新步骤 6）、工作规范（文件组织、任务追踪）。步骤编号约定：**保留分数编号**（4.5、5.5），不对现有步骤重新排序。

**Tech Stack:** Markdown agent definition（Claude Code agent file）

**Spec:** `docs/superpowers/specs/2026-03-17-rednote-image-content-planning-design.md`

**嵌套代码块约定：** `agents/rednote.md` 中的 image-plan.md 格式示例使用 `~~~` 围栏（而非 ` ``` `），避免与外层 Markdown 上下文冲突。

---

### Task 1：原创模式 — 插入步骤 4.5，更新步骤 5

**Files:**
- Modify: `agents/rednote.md`（原创模式流程区块，约第 50–68 行）

#### 变更说明

**A. 步骤编号**：不修改现有步骤 5 的编号。新步骤插在步骤 4 和步骤 5 之间，编号为 **4.5**。

**B. 插入步骤 4.5**（在步骤 4 末尾的空行之后、`5. **生成图片**` 之前）：

~~~markdown
4.5. **图片内容规划**：基于选题方向、最终标题和目标受众，独立规划每张图的具体内容，写入 `$DIR/image-plan.md`。

   **规划原则**：
   - 规划时不依赖文案正文的叙述，围绕主题自主展开知识内容
   - 每个信息点 20-60 字，包含具体数字/场景/可操作细节（例：不写"介绍技巧"，而写"🔍 看评分：大众点评≥4.5，且近30天有评价"）
   - 情感/体验类主题须知识化扩展（打卡体验 → 选址技巧；旅行感受 → 行程攻略）；纯情感/美学类账号（account info 中无干货/教程标签）可酌情减少，但每页仍需至少 2 个有具体细节的信息点
   - 每张图主题不重叠

   **图片总数**：确定 N 张（奇数，5-7 张），N 即后续 `--count` 基数（内容图 = N-1 张，封面单独生成）

   **image-plan.md 格式**：

   ~~~markdown
   # 图片内容规划

   ## 总体策略
   - 主题方向: xxx
   - 目标受众: xxx
   - 图片内容定位: xxx（如：从情感种草扩展到实用干货，增加收藏价值）
   - 计划图片数量: N 张（此数值决定后续 --count 参数）

   ---

   ## cover 封面
   - 钩子: （≤10字，吸引点击的一句话）
   - 辅助信息: （1个数字或悬念，≤15字）

   ---

   ## image_01 [内容] 主题：xxx
   - 信息点1: （具体内容，20-60字，含数字/场景/细节）
   - 信息点2: （具体内容）
   - 信息点3: （具体内容）
   - 推荐布局: 编号清单 / 对比双栏 / 步骤流程 / 标注图解

   ---

   ## image_02 [内容] 主题：xxx
   （同上结构）

   ---

   ## image_0{N-1} [尾部]
   - 总结条目1: （核心 takeaway，20-40字）
   - 总结条目2:
   - 总结条目3:
   - 一句话核心观点: （全文最重要的结论）
   ~~~
~~~

**C. 更新步骤 5（生成图片）**：

替换原"**图片数量**"段落（以下为原文 → 新文）：

原文：
```
   **图片数量**：根据内容规划，一般 5-7 张（奇数效果更好）：
   - 封面图: 只放标题+1个钩子信息，文字≤30%，留白≥50%，目标是吸引点击
   - 内容图: 每页2-4个结构化信息点，文字35-50%，用编号清单/对比双栏/步骤流程等布局，高密度知识卡片风格，与封面必须有明显的信息密度差异
   - 收尾图: 全文核心要点汇总页，提炼前面所有内容图的精华，用精简条目列出关键 takeaway
```

新文：
```
   **图片数量**：由步骤 4.5 的 `image-plan.md` 中"计划图片数量: N 张"决定，`--count` 使用 N-1（封面单独生成）。
```

替换原文件检查行并在其前追加 prompt 构建说明（以下为原文 → 新文）：

原文：
```
   生成后检查每张图片：`$DIR/cover.png`（封面）、`$DIR/image_02.png` ... `$DIR/image_0N.png`
```

新文：
```
   **内容图 prompt 构建**：将 `image-plan.md` 中所有内容页（image_01 至 image_0{N-2}）和尾部页（image_0{N-1}）合并，构建统一分页描述（每页列出主题、信息点和推荐布局），作为 `--count N-1` 批量生成的 prompt。封面 `{page_content}` 使用 image-plan.md 封面规划：`{钩子} | {辅助信息}`；封面的 `{full_outline}` 仍使用 `content.md` 正文（不替换）。

   生成后检查每张图片：`$DIR/cover.png`（封面）、`$DIR/image_01.png` ... `$DIR/image_0{N-1}.png`
```

- [ ] **Step 1：插入步骤 4.5**

在 `agents/rednote.md` 步骤 4 末尾的空行（第 50 行）之后、步骤 5 之前，插入上述 B 段内容（使用 `~~~` 围栏包裹 image-plan.md 格式示例）。

- [ ] **Step 2：替换步骤 5 的图片数量段落**

按上述 C 段第一个替换，删除原 3 行图片数量说明，替换为 1 行引用 image-plan.md 的说明。

- [ ] **Step 3：替换步骤 5 的文件检查行，追加 prompt 构建说明**

按上述 C 段第二个替换，在原文件检查行位置插入 prompt 构建说明 + 新文件检查行。

- [ ] **Step 4：验证原创模式区块**

阅读修改后的原创模式区块，确认：
- 步骤编号：1, 2, 3, 4, 4.5, 5（步骤 5 编号未变）
- 步骤 4.5 中 image-plan.md 格式示例使用 `~~~` 而非 ` ``` `
- 步骤 5 中图片数量引用 image-plan.md，文件检查从 `image_01` 开始
- 步骤 5 中有 `{full_outline}` 仍使用 `content.md` 的说明

- [ ] **Step 5：commit**

```bash
git add agents/rednote.md
git commit -m "feat(rednote): add image content planning step 4.5 for 原创 mode"
```

---

### Task 2：复刻模式 — 更新步骤 3，插入步骤 5.5，更新步骤 6

**Files:**
- Modify: `agents/rednote.md`（复刻模式流程区块，约第 72–103 行）

#### 变更说明

**A. 步骤 3 新增第 6 维度**（在"标签模板"条目之后追加）：

```markdown
   - **视觉结构模板**：图片总张数（含封面）、各内容页主题关键词（按顺序）；若源笔记图片信息无法从 API 返回数据中提取，记录"视觉结构：无法提取"，`tight` 模式图片规划自动降级为 `medium` 行为
```

**B. 插入步骤 5.5**（在步骤 5 末尾与步骤 6 之间）：

~~~markdown
5.5. **图片内容规划**：基于选题方向、最终标题和目标受众，独立规划每张图的具体内容，写入 `$DIR/image-plan.md`。规划规则与原创模式步骤 4.5 相同，但根据改写模式调整参考依据：

   | 模式 | 图片规划依据 |
   |------|------------|
   | `style-only`（默认） | 独立规划，不复用源笔记具体内容（可参考其页数作为参考上限） |
   | `medium` | 参考 `source-analysis.md` 中源笔记信息结构，内容重新设计 |
   | `tight` | 参照 `source-analysis.md` 第6维度（视觉结构模板）的页数和主题结构；若标记"无法提取"则按 `medium` 处理 |
~~~

**C. 步骤 6 末尾追加 prompt 构建说明**（在"无配置参考图"条目之后）：

```markdown

   **内容图 prompt 构建**：将 `image-plan.md` 中所有内容页（image_01 至 image_0{N-2}）和尾部页（image_0{N-1}）合并，构建统一分页描述（每页列出主题、信息点和推荐布局），作为 `--count N-1` 批量生成的 prompt。封面 `{page_content}` 使用 image-plan.md 封面规划：`{钩子} | {辅助信息}`；封面的 `{full_outline}` 仍使用 `content.md` 正文（不替换）。`--count` 使用 image-plan.md 中"计划图片数量"减 1。

   生成后检查每张图片：`$DIR/cover.png`（封面）、`$DIR/image_01.png` ... `$DIR/image_0{N-1}.png`
```

- [ ] **Step 1：步骤 3 新增视觉结构模板条目**

在复刻模式步骤 3 的"标签模板"条目之后追加第 6 维（如上 A 所示）。

- [ ] **Step 2：插入步骤 5.5**

在复刻模式步骤 5 末尾与步骤 6 之间插入新步骤 5.5（如上 B 所示；此处仅是表格，不含嵌套代码块，无需 `~~~` 围栏）。

- [ ] **Step 3：步骤 6 末尾追加 prompt 构建说明**

在复刻模式步骤 6 的最后一行（"无配置参考图"条目）之后追加上述 C 段内容。

- [ ] **Step 4：验证复刻模式区块**

阅读修改后的复刻模式区块，确认：
- 步骤 3 有 6 个维度（含视觉结构模板，含降级说明）
- 步骤 5.5 有 3 行模式映射表格
- 步骤 6 末尾有 prompt 构建说明、`{full_outline}` 仍用 content.md 的说明，以及文件检查从 `image_01` 开始

- [ ] **Step 5：commit**

```bash
git add agents/rednote.md
git commit -m "feat(rednote): add image content planning step 5.5 for 复刻 mode"
```

---

### Task 3：更新工作规范

**Files:**
- Modify: `agents/rednote.md`（工作规范区块，约第 118–132 行）

- [ ] **Step 1：更新文件组织 — 图片命名行**

将：
```
- 图片命名：`$DIR/cover.png`, `$DIR/image_02.png`, `$DIR/image_03.png` 等
```
改为：
```
- 图片命名：`$DIR/cover.png`, `$DIR/image_01.png` ... `$DIR/image_0{N-1}.png`（N 由 image-plan.md 决定）
```

- [ ] **Step 2：文件组织 — 追加 image-plan.md 条目**

在"内容草稿"行之后追加：
```
- 图片规划：`$DIR/image-plan.md`（步骤 4.5/5.5 产物，包含每页具体知识内容）
```

- [ ] **Step 3：更新任务追踪进度示例**

将：
```
- 报告进度：`[3/5] 图片生成完成 → $DIR/ (5张图片)`
```
改为（使用图片生成步骤在流程中的位置，原创模式步骤 5 / 复刻模式步骤 6）：
```
- 报告进度示例：`[5/6] 图片生成完成 → $DIR/ (5张图片)`（原创模式，含步骤4.5共6步）
```

- [ ] **Step 4：commit**

```bash
git add agents/rednote.md
git commit -m "docs(rednote): update file org and task tracking for image plan step"
```

---

### Task 4：整体验证

- [ ] **Step 1：通读全文**

阅读完整的 `agents/rednote.md`，检查以下内容：

| 检查项 | 期望 |
|--------|------|
| 原创模式步骤编号 | 1, 2, 3, 4, 4.5, 5（连续，4.5 不打乱后续编号） |
| 复刻模式步骤编号 | 1, 2, 3, 4, 5, 5.5, 6, 7 |
| `image_02.png` 作为起始的引用 | 全文无遗留（已替换为 `image_01.png`） |
| image-plan.md 格式示例 | 使用 `~~~` 围栏，完整可读 |
| 两处 `{full_outline}` 说明 | 步骤 5（原创）和步骤 6（复刻）均有"仍使用 content.md 正文" |
| 文件组织 | 含 `image-plan.md` 条目，图片命名从 `image_01` 开始 |

- [ ] **Step 2：最终 commit**

```bash
git add agents/rednote.md
git commit -m "feat(rednote): image content planning — complete"
```
