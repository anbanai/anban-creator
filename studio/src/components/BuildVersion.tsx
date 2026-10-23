export function BuildVersion() {
  const commit = typeof __APP_COMMIT_SHA__ === 'string' ? __APP_COMMIT_SHA__ : 'unknown'
  const valid = /^[a-f0-9]{40}$/i.test(commit)
  return <span className="select-text text-xs" title={valid ? `前端完整版本：${commit}` : '当前构建未提供提交信息'}>前端版本 <span className="font-mono">{valid ? commit.slice(0, 7) : '开发版'}</span></span>
}
