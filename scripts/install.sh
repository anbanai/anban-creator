#!/bin/bash
# Anban 智能创作助手 自动安装脚本
# 适用于：macOS / Linux
# 使用方法：DOWNLOAD_URL/main/scripts/install.sh | bash

set -e

echo "========================================"
echo "   Anban 智能创作助手 安装向导"
echo "========================================"
echo ""

# 检测系统
OS="$(uname -s)"
ARCH="$(uname -m)"

echo "检测到系统: $OS $ARCH"

# 确定下载链接
if [ "$OS" = "Darwin" ]; then
    if [ "$ARCH" = "arm64" ]; then
        BINARY="anban-creator-server-darwin-arm64"
    else
        BINARY="anban-creator-server-darwin-amd64"
    fi
elif [ "$OS" = "Linux" ]; then
    if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        BINARY="anban-creator-server-linux-arm64"
    else
        BINARY="anban-creator-server-linux-amd64"
    fi
else
    echo "❌ 不支持的系统: $OS"
    exit 1
fi

echo "将下载: $BINARY"
echo ""

# 确定安装目录
INSTALL_DIR="$HOME/.local/bin"
mkdir -p "$INSTALL_DIR"

# 下载
echo "正在下载..."
DOWNLOAD_URL="https://github.com/anbanai/anbancreator/releases/latest/download/$BINARY"
echo "下载地址: $DOWNLOAD_URL"

if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/anban-creator-server"
elif command -v wget >/dev/null 2>&1; then
    wget -q "$DOWNLOAD_URL" -O "$INSTALL_DIR/anban-creator-server"
else
    echo "❌ 需要 curl 或 wget 来下载文件"
    exit 1
fi

# 添加执行权限
chmod +x "$INSTALL_DIR/anban-creator-server"

echo ""
echo "✅ 下载完成！"
echo ""

# 检查 PATH
if echo ":$PATH:" | grep -q ":$INSTALL_DIR:"; then
    echo "✅ 安装目录已在 PATH 中"
else
    echo "⚠️  需要将安装目录添加到 PATH"
    echo ""
    echo "请根据你的 shell 执行以下命令："

    # 检测 shell
    if [ -n "$ZSH_VERSION" ]; then
        echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc"
        echo "  source ~/.zshrc"
    elif [ -n "$BASH_VERSION" ]; then
        echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc"
        echo "  source ~/.bashrc"
    else
        echo "  将 $INSTALL_DIR 添加到你的 PATH 环境变量"
    fi
fi

echo ""
echo "========================================"
echo "   安装完成！"
echo "========================================"
echo ""
echo "下一步："
echo "  1. 准备 server/config.yaml"
echo "  2. 运行: anban-creator-server -config server/config.yaml"
echo "  3. 打开 Studio 完成项目和模型配置"
echo ""
echo "查看服务参数: anban-creator-server --help"
echo ""
