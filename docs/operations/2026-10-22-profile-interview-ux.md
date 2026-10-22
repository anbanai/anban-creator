# 2026-10-22 人设交互与访谈修复

## 变更
- 人设卡片增加独立、可键盘访问的铺满卡片详情链接；按钮和其他链接位于上层，保持原操作。
- 归档项目的恢复操作统一为“取消归档”；历史版本恢复等其他含义不改。
- 修复 ChatPage 的发送锁竞态：SSE user.message 提前清掉 pending submission 后，HTTP finally 仍须释放发送锁，否则 turn.end 后输入框持续禁用。保留并发提交和过期会话保护。
- 人设页 AI 创建及 /projects/new/interview 使用现有轻量访谈页。该页展示六维画像、完善度、语音输入和开始创作，调用既有 /onboarding/* 接口，不启动 DSH Agent 工作容器。
- AI 助手新画像模式增加快速访谈入口，未发送文字带入输入框、由用户确认后发送。已有 Agent 对话不迁移、不丢弃。
- 通用 Agent 处理记录默认折叠，原消息和草稿确认流程保留。

## 发布
仅更新 creator.anbanai.com-prod / Studio 镜像。没有 Server、Runtime、Harness、数据库或密钥变更。轻量访谈依赖生产已存在的 /onboarding/capabilities、/onboarding/chat、/onboarding/transcribe；若 capabilities.configured=false，应检查原服务端 onboarding 配置，不能在前端填 API Key。语音是否可用按 speech_available 显示。

使用发布前的当前 Studio 镜像 digest 设置 STUDIO_PREVIOUS_IMAGE，保留旧资源。遵循现有发布审批，不覆盖后端配置。回滚上一版前端镜像即可。

## 验收与限制
- 验证卡片空白点击到详情，归档/编辑/作品按钮仍执行各自功能。
- 连续两轮对话，覆盖服务端消息回执早于发送请求返回的顺序。
- 访谈展示画像维度与填写进度；有任何输入可选择开始创作，未达标时先确认。
- 当前生产中已经展开的 Agent 会话继续使用原流程；本次没有将历史对话自动转写到轻量访谈，也未更改 Agent 推理速度。
- 五分钟建立初步画像是产品目标，需使用真实线上模型实测，不能把前端测试当成延迟指标。
- 工作区原有 harness 差异未提交。拉取时历史不可访问子模块提交曾报错，当前 main 的 harness 指针为 29de42d9894fe6aa922ac20f991c3136d4fdb00d，本次保持该指针。

验证：Studio 全量测试 157 个文件、1424 项通过；最终变更后定向复测 116 项通过；TypeScript 与生产构建通过。构建仅有既有的大包体积提示。未在生产进行写入或性能测试。
