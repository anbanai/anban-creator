# line-art-coloring Examples

### Case 1: 三角色儿童插画

- Input: 一组线稿包含女孩、猫、风筝。
- Recommended path: 先建 Color Bible，再用原线稿单源 ref 生成候选并做颜色/线稿双轨审计。
- Artifacts: color-bible.md、colored_01.png、consistency-report.md。
- Quality gate: 颜色一致优先，不能承诺像素级保线。

### Case 2: 角色新增道具

- Input: 第 4 张线稿新增雨伞。
- Recommended path: 沿用已有角色颜色，为雨伞新增语义色并记录反面约束。
- Artifacts: color-bible.md、best-refs.md。
- Quality gate: 新增颜色与角色主色区分，避免跨图混淆。

### Case 3: 线稿退化回归

- Input: 修正颜色后线条比前一版变形。
- Recommended path: 拒收修正版，回退颜色次优但线稿更稳的版本，标 needs_img2img。
- Artifacts: verification.md、consistency-report.md。
- Quality gate: 回归守卫优先于继续重绘。
