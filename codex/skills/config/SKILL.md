---
name: config
description: Configures anbanwriter channel settings including writer style (写作风格), theme (排版主题), image provider (图片生成服务), and API keys. Use when user wants to initialize, view, or modify anbanwriter configuration. Also use when user mentions '配置频道', '设置风格', '修改配置', 'config', '频道设置', '查看配置', '写作风格', '排版主题', or when wanting to check or change channel-level settings.
---

# anbanwriter 配置管理

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `get_channel_profile` (channel_id, scope?) | 查看频道配置信息（定位、风格、主题、图片服务等） |
| `list_channels` () | 列出所有频道 |

---

## 查看当前配置

调用 `get_channel_profile(channel_id="$CHANNEL_ID")` 获取完整的频道配置。

返回结果包含以下字段（与下方配置项对应）：

| 字段 | 说明 | 示例 |
|------|------|------|
| `positioning` | 账号定位描述 | "专注中医养生领域..." |
| `keywords` | 关键词列表 | ["中医", "养生", "健康"] |
| `target_audience` | 目标受众 | "30-50岁关注健康的职场人士" |
| `writer` | 写作风格 | "dan-koe" |
| `theme` | 排版主题 | "default" |
| `image_provider` | 图片生成服务 | "openai" |

如果返回结果中某个字段为空或缺失，说明该配置项尚未设置。

---

## 配置项参考

### 写作风格（writer）

控制文章的语气、结构和修辞风格。配置后 `write_article` MCP 工具自动使用该风格。

| 风格名 | 英文名 | 特点 | 适合账号 |
|--------|--------|------|----------|
| 简洁犀利 | `dan-koe` | 深刻但不晦涩，犀利但不刻薄，有哲学深度 | 个人成长、观点评论、人生感悟 |
| 文化底蕴 | `cultural-depth` | 文化底蕴深厚，文学修辞丰富，深度思考 | 文化历史、艺术鉴赏、人文关怀 |
| 轻松科普 | `casual-science` | 通俗易懂，生动有趣，科学严谨 | 科普教育、生活常识、知识分享 |

风格定义文件位于 `writers/` 目录，支持自定义。

### 排版主题（theme）

控制文章在微信公众号的 HTML 排版样式。配置后 `convert_markdown` MCP 工具自动使用该主题。

| 主题名 | 说明 |
|--------|------|
| `default` | 经典商务排版（黑白灰），通用 |

主题由服务器管理，通过 `convert_markdown` MCP 工具自动应用频道配置的主题。

### 图片生成服务（image_provider）

控制封面图和配图的生成 API。

| 服务名 | 说明 | 适合场景 |
|--------|------|----------|
| `openai` | OpenAI DALL-E（默认） | 通用场景，稳定性好 |
| `gemini` | Google Gemini | 图片质量高，内联返回 |
| `volcengine` | 火山引擎 Seedream | 中文语境理解好，异步生成 |

---

## 分步配置流程

### 第一步：查看当前状态

调用 `get_channel_profile` 了解当前配置。对比上方参考表，确定需要修改的项。

### 第二步：按需修改

配置支持增量更新——修改一个字段不会影响其他已设置的字段。无需一次性配置所有项。

优先级建议：
1. **写作风格**（影响最大）→ 选择与账号定位匹配的风格
2. **排版主题**（视觉效果）→ 选择与账号调性匹配的主题
3. **图片生成服务**（可选）→ 默认 openai，除非有特殊需求

### 第三步：验证配置

修改后再次调用 `get_channel_profile` 确认配置已生效。

---

## 常见问题

**Q: 修改风格后已有的草稿会受影响吗？**
A: 不会。风格配置只影响后续新生成的内容，已发布的草稿不受影响。

**Q: 可以自定义写作风格吗？**
A: 可以。在 `writers/` 目录下创建 YAML 文件，格式参考 `writers/dan-koe.yaml`。

**Q: 主题可以自定义吗？**
A: 主题由服务器统一管理。如需自定义主题，请联系管理员在服务端添加。
