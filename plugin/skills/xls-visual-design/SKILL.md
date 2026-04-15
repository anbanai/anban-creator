---
name: xls-visual-design
description: Generates cover and content images for WeChat Xiaolvshu (小绿书/图片帖) image posts with 3:4 ratio. Use when creating visual content for WeChat newspic format.
---

# 小绿书图片生成

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `generate_image` (channel_id, prompt, image_type="cover", output_path) | 生成封面（单张） |
| `generate_batch_images` (channel_id, prompt, count, output_dir) | 批量生成内容图 |
| `upload_image` (channel_id, file_path) | 上传图片到微信素材库 |

---

## 平台 Gotcha

小绿书（微信图片帖）最多 20 张图片，发布后图片顺序固定。封面是用户看到的第一张，内容图靠滑动浏览。与小红书相比：微信用户相对成熟，设计感可以更强，但仍需保持视觉一致性。

---

## 使用方式

通过 MCP 工具调用：

1. **生成封面（单张）**：调用 `generate_image`，image_type 设为 `"cover"`，prompt 中描述封面内容和风格
2. **批量生成内容图**：调用 `generate_batch_images`，指定 count 和 prompt
3. **带参考图保持一致**：提供参考图路径，保持视觉风格统一
4. **带风格描述**：在 prompt 中加入风格描述（如"简约质感，米白色调"）

**关键规则**：内容图使用 `generate_batch_images` 批量生成，不逐张调用。

---

## 视觉风格设计原则

根据内容类型和账号调性动态设计，不使用固定预设：

**常见小绿书内容风格参考**：
- 旅行图集：真实感照片风，自然色温，minimal 文字叠层
- 好物测评：产品感，纯净背景，数据对比清晰
- 知识干货：结构感强，信息图式，配色克制
- 日常打卡：生活感，暖色调，随拍质感

**封面与内容图一致性**：
- 无参考图：先生成封面 → 以封面作为参考图批量生成内容图
- 有配置参考图：统一使用配置参考图

---

## 设计规范

见 [references/design-norms.md](references/design-norms.md)
