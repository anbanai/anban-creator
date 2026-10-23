# Studio 人设创作入口与版本展示（2026-10-23）

## 用户变化
- 登录页底部、用户菜单「关于案板」显示前端提交号前 7 位；悬停可查看完整 SHA。
- 人设归档操作、提示和通知统一使用「取消归档」。
- 人设详情「用此画像创作」进入独立创建页并选中当前人设；作品列表保持 project_id 筛选。
- 缺少创作类型时直接打开配置弹窗，只保存 agent_ids；保留已填写内容，不自动发起生成。

## 发布范围
只更新 creator.anbanai.com-prod（流水线 4839221 / creator-studio-web）。无数据库迁移、后端、Harness、Agent 镜像或密钥调整。

版本 SHA 优先取 STUDIO_COMMIT_SHA，其次当前 Git HEAD。云效现有宿主构建会生成 studio/.build-commit，Docker 构建复用它。直接构建镜像可传 --build-arg STUDIO_COMMIT_SHA=<40位SHA>。生产镜像缺少有效版本信息会构建失败。公开 version.json 仅包含完整和短 SHA，无任何凭证。

本次发布前将 STUDIO_PREVIOUS_IMAGE 指向第 284 次运行的镜像，以保留已打开页面所需的旧静态资源：
crpi-671lgv9lrdyll4t9.cn-chengdu.personal.cr.aliyuncs.com/bx_anbanai/creator-studio-web@sha256:ad995b7fd4bac3d3224144074962867b7d7cdf99c1ea9eab38bf2a561b7236ef

回退：使用云效第 284 次成功部署（2026-10-23 13:17，代码 acb01470）。本改动不变更服务端数据结构。

## 验证
- 全量 Studio 测试：159 个文件、1358 项通过。
- Studio 类型检查及生产构建通过。
- 回归覆盖：人设筛选保留、直接创建页面、配置保存继续创作、配置失败保留输入、7 位/完整 SHA 与缺少构建标记。
- 生产发布后仍需核对 version.json 与合并提交一致，以及菜单、详情、创建、设置入口。验证不自动创建收费作品或修改真实人设。
