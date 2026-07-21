# Color Bible 渐进式构建方法论

## Contents

- [什么是 Color Bible](#什么是-color-bible)
- [渐进式构建流程](#渐进式构建流程)
- [Color Bible 格式](#color-bible-格式)
- [Characters](#characters)
  - [Character: [简短描述名, 如 "Red Hood Girl"]](#character-简短描述名-如-red-hood-girl)
  - [Character: [另一个角色]](#character-另一个角色)
- [Objects](#objects)
  - [Object: [名称, 如 "Picnic Basket"]](#object-名称-如-picnic-basket)
- [Environment](#environment)
  - [Environment: [场景描述]](#environment-场景描述)
- [Color Relationships](#color-relationships)
- [颜色定义最佳实践](#颜色定义最佳实践)

## 什么是 Color Bible

Color Bible 是一份结构化文档，记录所有已识别实体的颜色规格。它是跨图颜色一致性的「绑定契约」——所有上色操作必须遵循 Color Bible 中定义的颜色。

**渐进式**：Color Bible 不是一次性建完的，而是随每张线稿的处理逐步构建和扩展。

## 渐进式构建流程

```
处理线稿 0:
  识别实体 → 全部是新实体 → 定义颜色 → 建立初始 Color Bible

处理线稿 1:
  识别实体 → 匹配已有实体(复用颜色) + 发现新实体(定义新颜色)
  → 更新 Color Bible

处理线稿 N:
  识别实体 → 匹配 + 新增 → 更新 Color Bible
```

## Color Bible 格式

```markdown
# Color Bible

## Characters

### Character: [简短描述名, 如 "Red Hood Girl"]

**Identification**: (用于跨图匹配)
- Build: petite, child-like proportions
- Hair: long, flowing, past shoulders
- Key feature: red hooded cape
- Typical position: center or foreground

**Colors**:
- Hair: deep dark chocolate brown, like 70% cocoa dark chocolate bar — must NOT be light brown, auburn, or reddish
- Skin: warm peachy beige, like light sandstone — not pink, not yellow
- Cape/Hood: bright cherry red, like a fire truck or ripe tomato — must NOT be orange, burgundy, or pink-red
- Dress: deep navy blue, like a midnight business suit — not royal blue, not black
- Shoes: warm chestnut brown, like a leather boot — not black

**Materials**:
- Cape: smooth wool-like fabric, soft matte finish
- Dress: simple cotton-like texture
- Shoes: leather-like

**Palette discipline**（色彩理论纪律，新实体必填）:
- Role in palette: 主角锚色（画面主视觉）
- Harmony: 与 Wolf 的 charcoal gray 构成红-灰互补对比；与场景暖光和谐
- Scene/brand fit: 童话绘本暖色调，饱和度适中、不刺眼

---

### Character: [另一个角色]

**Identification**: ...
**Colors**: ...
**Materials**: ...

## Objects

### Object: [名称, 如 "Picnic Basket"]

**Identification**: (用于跨图匹配)
- Shape: oval woven basket
- Size: medium, held in character's hand
- Key feature: checkered cloth inside

**Colors**:
- Basket body: warm honey gold, like natural wicker — not white, not dark brown
- Cloth: same bright cherry red as Red Hood Girl's cape (COLOR RELATIONSHIP)
- Handle: same warm honey gold as basket body

## Environment

### Environment: [场景描述]

**Colors**:
- Sky: soft warm blue, like a clear spring afternoon
- Ground: fresh grass green, not too dark
- Lighting: warm golden hour, gentle shadows

## Color Relationships

- Red Hood Girl's cape = Picnic Basket's cloth (both bright cherry red)
- Wolf's eyes = sunset amber gold (links to warm lighting)
```

## 颜色定义最佳实践

### 1. 语义色名 + 实物类比

每个颜色必须包含：
- **语义色名**：标准色名 + 强度/深度修饰词
- **实物类比**：一个真实世界的参照物

示例：
```
❌ "red"          — 太模糊，模型可能选任何红色
❌ "#FF0000"      — 模型经常忽略 hex
✅ "bright cherry red, like a fire truck"  — 精确且有视觉锚点
✅ "deep dark chocolate brown, like 70% cocoa bar"  — 无歧义
```

### 2. 反面约束

每个颜色定义附带「不能是什么」：
```
Skin: warm peachy beige, like light sandstone
  — NOT pink, NOT yellow, NOT orange
```

为什么有效：模型有时会把"beige"渲染成黄色或粉色。反面约束帮助模型缩小颜色范围。

### 3. 跨实体颜色关系

如果两个实体共享某种颜色，必须明确绑定：
```
Color Relationships:
- Red Hood Girl's cape = Picnic Basket's cloth (both bright cherry red)
```

这防止模型给两个应该是同色的物品上不同的红色。

### 4. 颜色区分度

不同实体的主色应有足够区分度：
- 如果有两个角色，避免都用蓝色系
- 用互补色或对比色增强视觉区分
- 如果两个角色确实颜色相近（如都穿白衬衫），用配饰/细节色区分

### 5. 色彩理论纪律（和谐 + 场景品牌适配）

新实体定色不只是“选个颜色”，而是让它与整体和谐、与场景/品牌调性适配。每个新实体在 Color Bible 里填 **Palette discipline**：

- **角色在调色板中的位置**：主角锚色（画面主视觉）/ 配角衬托色 / 环境过渡色。
- **和谐关系**：与已有实体色的关系——同类色（同色系深浅）、邻近色（色轮相邻）、互补/对比色（色轮相对）。多角色场景优先用「1 个主色 + 邻近色衬托 + 1 个互补色点睛」的结构，避免颜色打架。
- **场景/品牌适配**：童话绘本偏暖、饱和适中；科技/商务偏冷、低饱和；品牌内容需对齐品牌主色。整体饱和度和明度风格要在 Color Bible 全局统一。

为什么：模型默认倾向高饱和、撞色。明确和谐关系和场景适配，能让多图配色既一致又“像出自同一设计师之手”，而不是各自抢眼。

## 实体匹配方法

处理新线稿时，如何判断图中的角色是否是 Color Bible 中已有的实体：

### 匹配维度

1. **体型比例**：高矮、胖瘦、成人/儿童比例
2. **发型轮廓**：长发/短发/马尾/卷发/特殊发型
3. **服装类型**：外套/裙子/裤子/帽子/围巾
4. **标志性特征**：配饰（眼镜、背包、武器）、伤疤、纹身
5. **故事角色功能**：主角/对手/配角/路人（如果图集有故事线）
6. **空间位置关系**：与其他角色的相对位置

### 匹配判断

- **高置信匹配**（≥ 3 维度一致）→ 确认为同一实体，复用颜色
- **中等置信**（2 维度一致但有差异）→ 需要仔细评估：
  - 差异可能是视角/姿态变化 → 仍为同一实体
  - 差异是根本性特征不同 → 新实体
- **低置信**（< 2 维度一致）→ 视为新实体

### 常见陷阱

- **换装角色**：同一角色换了衣服 → 仍然是同一实体（发型/体型/面部特征匹配）
- **不同姿态**：同一角色从正面变成侧面 → 仍然是同一实体
- **相似角色**：双胞胎/克隆体 → 需要配饰/位置等细节区分
- **年龄变化**：如果图集跨越时间线，同一角色可能外观不同 → 需要故事上下文判断

## 渐进式扩展示例

### 线稿 0 处理后

```markdown
# Color Bible

## Characters

### Character: Little Girl
**Identification**: petite, long straight hair, wearing hooded cape
**Colors**:
- Hair: deep dark chocolate brown — NOT light brown or auburn
- Skin: warm peachy beige — NOT pink or yellow
- Cape: bright cherry red, like a fire truck
- Dress: deep navy blue, like midnight
```

### 线稿 1 处理后（新增 1 个实体）

```markdown
# Color Bible

## Characters

### Character: Little Girl
(matched — same colors as above)
(images: colored_00, colored_01)

### Character: Big Wolf  ← NEW
**Identification**: large, pointed ears, bushy tail, hunched posture
**Colors**:
- Fur: dark charcoal gray, like storm clouds — NOT brown or black
- Eyes: warm amber gold, like honey — NOT red or blue
- Nose: matte black, like polished obsidian
```

### 线稿 3 处理后（发现已有实体，新增 1 个）

```markdown
...

### Object: Basket  ← NEW
**Identification**: small oval woven basket with handle
**Colors**:
- Body: warm honey gold, like natural wicker
- Cloth: SAME bright cherry red as Little Girl's cape

## Color Relationships
- Little Girl's cape = Basket's cloth (bright cherry red)
```

这种渐进扩展确保：
- 已有实体颜色不变（锁定）
- 新实体颜色与已有实体协调（区分度 + 关系）
- 不会遗漏任何实体
