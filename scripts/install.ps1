# Writer for WeChat Windows 自动安装脚本
# 使用方法：在 PowerShell 中运行
# Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/royalrick/wechatwriter/main/scripts/install.ps1'))

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   Writer for WeChat 安装向导" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# 检测是否是管理员
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

# 确定安装目录
if ($isAdmin) {
    $installDir = "C:\Program Files\writer"
} else {
    $installDir = "$env:USERPROFILE\AppData\Local\writer"
}

Write-Host "安装目录: $installDir" -ForegroundColor Yellow
Write-Host ""

# 创建目录
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

# 下载
Write-Host "正在下载..." -ForegroundColor Green
$downloadUrl = "https://github.com/royalrick/wechatwriter/app/releases/latest/download/writer-windows-amd64.exe"
$outputFile = "$installDir\writer.exe"

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -Uri $downloadUrl -OutFile $outputFile -UseBasicParsing
    Write-Host "✅ 下载完成！" -ForegroundColor Green
} catch {
    Write-Host "❌ 下载失败: $_" -ForegroundColor Red
    Read-Host "按回车键退出"
    exit 1
}

Write-Host ""

# 添加到 PATH
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($currentPath -notlike "*$installDir*") {
    Write-Host "添加到系统 PATH..." -ForegroundColor Yellow
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$installDir", "User")
    Write-Host "✅ 已添加到 PATH" -ForegroundColor Green
    Write-Host ""
    Write-Host "⚠️  需要重启终端或命令提示符才能生效" -ForegroundColor Yellow
} else {
    Write-Host "✅ 已在 PATH 中" -ForegroundColor Green
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   安装完成！" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "下一步：" -ForegroundColor Yellow
Write-Host "  1. 重启此终端或命令提示符" -ForegroundColor White
Write-Host "  2. 运行: writer config init" -ForegroundColor White
Write-Host "  3. 编辑生成的配置文件" -ForegroundColor White
Write-Host "  4. 运行: writer convert 文章.md --preview" -ForegroundColor White
Write-Host ""
Write-Host "查看帮助: writer --help" -ForegroundColor White
Write-Host ""

Read-Host "按回车键退出"
