# 技能概览

## 保留的技能

| 技能 | 类型 | 说明 |
|------|------|------|
| config | 用户命令 | 配置管理和账号信息 |
| content-writing | 知识库 | 写作风格、AI去痕、HTML规范 |
| visual-design | 知识库 | 图片生成、主题管理 |
| topic-research | 知识库 | 选题评分、大纲生成 |
| seo-optimization | 知识库 | 标题/关键词/摘要优化 |
| article-publishing | 知识库 | 图文文章草稿发布 |
| xls-publishing | 知识库 | 小绿书图片帖发布 |
| rednote-research | 知识库 | 小红书选题研究、MCP 数据采集、互动率评分模型 |
| rednote-writing | 知识库 | 小红书文案公式、标题心理学、行业模板 |

## 自动化流水线

### 图文文章 (wechatarticle agent)

```
选题研究 → 文章撰写 → AI去痕 → SEO优化 → 封面配图 → HTML转换 → 草稿发布
```

对应技能链：

```
topic-research → content-writing → content-writing(humanize)
→ seo-optimization → visual-design → content-writing(convert)
→ article-publishing
```

### 小绿书 (wechatxls agent)

```
选题研究 → 图片设计 → 草稿发布
```

对应技能链：

```
topic-research → visual-design → xls-publishing
```

### 小红书 (rednote agent)

```
选题研究 → 内容创作 → 图片设计
```

对应技能链：

```
rednote-research → rednote-writing → visual-design
```
