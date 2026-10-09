# 真实对话解析与 IP 画像映射（本机预览）

2026-10-09，分支 `feature/project-creator-onboarding`。

## 已实现

本机预览的默认入口改为 `LivePortraitOnboarding`，不再要求使用预设花店话术。键盘输入、离线语音转写结果在用户点击发送后，进入同一个真实 DeepSeek 请求。模型自然回应、按需要追问，并返回完整画像候选。

支持身份、读者、风格、平台、偏好与边界、独有经历的逐步补充。未知项保持空白；本次具体作品意向单独保存，不当作长期画像记忆。名字必须在用户原话中出现；非空维度必须带用户消息 id 和逐字引用，服务端校验后才更新 UI。区分用户表述与 AI 推断，用户可以展开“依据”检查原文。引用存在不代表语义已经被证明，AI 归纳仍需用户核对。

每轮返回完整候选，支持替换纠正及撤回字段；有新输入时撤销原确认。只有身份、读者和平台已有信息才允许确认，不要求所有维度填满。确认后的创作简报由真实画像和本次意向组成，不再使用花店内容。

错误、超时、余额/密钥异常、无效 JSON、截断输出及伪造依据均不覆盖原画像，也不清空输入。重启当前对话会取消请求，迟到结果不能污染新对话。未接通时明确提示，不回退成伪造 AI 回复。

## API 和密钥

用户选择 DeepSeek 官方 API。配置存放于仓库外的 `D:\Anban\.secrets\deepseek-onboarding.json`，也可用 `ANBAN_PREVIEW_PROVIDER_FILE` 指定服务器端配置路径。程序读取该文件但从不返回其内容；接口状态只包含是否配置及模型名。

配置形状（不包含实际密钥）：

```json
{
  "apiKey": "",
  "baseUrl": "https://api.deepseek.com",
  "model": "deepseek-flash"
}
```

文件按请求重新读取，保存后无需重启；页面可点击“检查 API 配置”。默认使用官方文档列出的 `deepseek-flash`，也支持 `deepseek-v4-pro`。当前仅允许官方 HTTPS 主机和根路径或 `/v1`，不自动把密钥发送给兼容中转站，不跟随上游重定向。

调用 `/chat/completions`，使用非思考模式、JSON Output、最多 5000 输出 token。一次请求只做画像解析，不提供工具调用、不执行网页指令、不生成或发布作品。没有自动重试，避免重复调用与额外消耗。

启动沿用 `preview.voice.config.ts`：

```powershell
$env:ANBAN_PREVIEW_WHISPER_ROOT = Join-Path $env:LOCALAPPDATA 'WhisperLocal'
bun run dev -- --config preview.voice.config.ts
```

本机 HTML 入口 `studio/tmp/onboarding-preview.html` 加载：

```tsx
import { createRoot } from 'react-dom/client'
import LivePortraitOnboarding from '@/dev/LivePortraitOnboarding'
import PortraitOnboardingPreview from '@/dev/PortraitOnboardingPreview'
import '@/index.css'
const demo = new URLSearchParams(window.location.search).get('demo') === '1'
createRoot(document.getElementById('root')!).render(demo ? <PortraitOnboardingPreview /> : <LivePortraitOnboarding />)
```

只有显式加 `?demo=1` 才进入旧的预设交互样例，二者不共用对话或画像。入口 HTML/TSX 位于忽略目录；业务组件及服务适配代码已提交。

## 数据与运行边界

- 音频仍在本机 Whisper 处理；发送后的文字及本次对话会交给 DeepSeek，页面已明确提示。
- 本机对话 API 只允许回环地址、精确 Host、同源 Origin 和专用请求头。接口限制单次文字/消息数/响应大小，并只允许一个并发解析。
- `.secrets` 目录位于仓库和 Studio 根目录之外，开发服务器额外禁止访问；实测 `/@fs/` 请求返回 403。密钥不进入浏览器包、请求体、日志或 Git。
- 此次是本机预览对真实模型的解析适配，不是新的生产 Agent 框架。没有改动正式 Go Server、数据库、画像六维协议、harness、权限、计费或发布链路。
- 候选、对话和确认只在当前页面内存中存在，刷新会丢失；确认不等于已写入正式项目。正式接入仍应沿用服务端画像版本、草稿持久化、Agent 执行和审计约束，不能直接把预览状态当成生产事实。
- 下一个“创作”页面目前交付真实创作简报，没有调用作品生成或正式发布。合并与上线仍由用户和技术合伙人决定。

## 验证

1. DeepSeek 官方实测：虚构的整理收纳业务介绍成功映射六个维度，并返回有效逐字引用。
2. 后续纠正测试：读者由双职工家庭替换为独居老人，平台改为公众号，旧选题撤回为 null。
3. 浏览器实测：将用户原预览中的语音转写原文保留并移入真实入口，已提取“面向中小电商提供 AI 产品”和“中小电商”，并追问具体解决的问题。没有替用户确认画像。
4. 自动化覆盖：输入边界、伪造/助手引用、姓名依据、严格结构、密钥目的地址约束、上游错误与截断、自由输入、纠正/撤回、失败保留、旧请求隔离、浏览器和离线语音回归。

接口依据：[官方 Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)、[JSON Output](https://api-docs.deepseek.com/guides/json_mode/)、[Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode/)。
