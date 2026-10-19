# 画像库与创作身份

## 产品关系

- 一个现有 Project 承载一个创作身份，如钢厂老板、接班人、师傅、销售或品牌。运营人员的多个 Project 在画像库集中展示。
- 身份画像仍以 Project.Profile 为唯一事实来源；不复制数据库、不改已有 project_id、不迁移历史作品。
- 平台账号资料保存于现有 profile_setup.platform_accounts。同一身份的多平台创作引用同一 project_id / profile，新任务沿用现有服务端画像快照机制。
- 账号资料登记不代表平台授权，也不承诺支持同平台多个账号的自动分发。发布连接继续使用原有平台设置。
- 本次不建立跨 Project 的动态画像引用关系；同一身份跨平台复用应在同一 Project 下配置，避免两份画像相互覆盖。

## 用户入口

- 主导航“画像库”：`/portraits`。默认含归档项目，支持名称、定位、账号名搜索。
- “查看与修改”：`/portraits/:projectId`，展示六维画像、版本冲突提示、平台账号和创作方式。
- “继续访谈”：原有 project-creator / update-project-profile，携带原 project_id；由画像库进入的访谈保存后回到画像详情。
- 新手引导增加“查看已有画像”；项目卡片增加“查看与修改画像”。
- 旧版已保存画像不再仅根据分析任务 initialization_status 判断为“待配置”。完善度只反映六项是否填写，不代表内容质量评估。

## 发布与回退

- 仅 Studio 前端变更；不更新 Server、runtime-dsh、Agent、Harness 或数据库。
- 流水线：`creator.anbanai.com-prod`。从合并后的 main 构建，构建文件 `deploy/docker/Dockerfile.studio`。
- 保留流水线现有后端地址、TLS、密钥、人工卡点；本功能无新增环境变量或 API Key。
- 按现有 Dockerfile 要求，将 `STUDIO_PREVIOUS_IMAGE` 保持为当前生产前端的不可变镜像引用，避免已打开页面的旧静态资源失效。
- 回退到发布前的 Studio 镜像即可；新页面写入的数据使用原有接口与字段，回退不会删除画像。

## 验收

- 登录后画像库可看到历史和已归档身份，查看原画像，编辑后刷新仍存在。
- 两个身份相互独立，继续访谈携带正确的 project_id。
- 画像修改通过 expected_revision 校验；发生冲突保留本地编辑，要求载入最新版本。
- 添加公众号、小红书等账号资料保留原始回答与素材，不重置画像，不启动分析或生成任务。
- 浏览器本地验收使用虚构钢厂角色；示例和截图放在忽略的 tmp 目录，不进入生产包。
