#!/bin/bash
# ============================================================
# 天琴/Jenkins 流水线构建入口
# 参考 krow-console 项目的 build.sh，适配 keling-agent-platform
# ============================================================
set -e

echo "=========================================="
echo "keling-agent-platform 构建脚本"
echo "=========================================="

# ---- 1. 环境检测（3 层优先级） ----
# 优先级：BUILD_ENV 环境变量 > VITE_CDN_URL 关键字匹配 > Git 分支名推断
detect_env() {
  # 1) 天琴流水线直接注入 BUILD_ENV
  if [ -n "$BUILD_ENV" ]; then
    echo "$BUILD_ENV"
    return
  fi

  # 2) 根据 VITE_CDN_URL 关键字推断
  if [ -n "$VITE_CDN_URL" ]; then
    if echo "$VITE_CDN_URL" | grep -qE "prt"; then
      echo "prt"
      return
    fi
    if echo "$VITE_CDN_URL" | grep -qE "staging|test|dev"; then
      echo "staging"
      return
    fi
    echo "production"
    return
  fi

  # 3) 根据 Git 分支名推断
  local branch
  branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
  case "$branch" in
    main|master)
      echo "production"
      ;;
    prt)
      echo "prt"
      ;;
    develop|staging|test)
      echo "staging"
      ;;
    *)
      echo "staging"  # 默认走 staging，安全第一
      ;;
  esac
}

BUILD_TARGET=$(detect_env)
echo "📦 检测到构建环境: $BUILD_TARGET"

# ---- 2. Node.js 版本检测 ----
echo ""
echo "🔧 检测 Node.js 版本..."

NODE_VERSION=$(node -v 2>/dev/null | sed 's/v//' | cut -d. -f1)
echo "  当前 Node.js 版本: $(node -v 2>/dev/null || echo '未安装')"

if [ -n "$NODE_VERSION" ] && [ "$NODE_VERSION" -lt 18 ]; then
  echo "  ⚠️ Node.js 版本过低 (需要 >= 18)，尝试切换..."
  # 尝试通过 nvm 切换
  if [ -s "$NVM_DIR/nvm.sh" ]; then
    . "$NVM_DIR/nvm.sh"
    nvm use 18 2>/dev/null || nvm install 18
  # 尝试通过 n 切换
  elif command -v n &>/dev/null; then
    n 18
  fi
  echo "  切换后 Node.js 版本: $(node -v)"
fi

# ---- 3. 包管理器初始化 (pnpm via corepack) ----
echo ""
echo "🔧 初始化包管理器..."

# CI 环境下禁用 husky
export CI=true

# 尝试使用 corepack 激活 pnpm（与 package.json 中 packageManager 字段一致）
if command -v corepack &>/dev/null; then
  corepack enable
  corepack prepare pnpm@10.29.1 --activate 2>/dev/null || true
  echo "  corepack 已激活 pnpm"
else
  # corepack 不可用时，尝试全局安装指定版本 pnpm
  echo "  corepack 不可用，尝试安装 pnpm@10.29.1..."
  npm install -g pnpm@10.29.1 2>/dev/null || true
fi

# 验证 pnpm 可用
if ! command -v pnpm &>/dev/null; then
  echo "❌ 错误: pnpm 未找到，请安装 pnpm 或启用 corepack"
  exit 1
fi

echo "  pnpm 版本: $(pnpm --version)"

# ---- 4. 安装依赖 ----
echo ""
echo "📥 安装依赖..."

# --ignore-scripts 避免 prepare 脚本执行 husky（CI 不需要 git hooks）
if pnpm install --frozen-lockfile --ignore-scripts --config.confirmModulesPurge=false 2>/dev/null; then
  echo "  依赖安装完成 (frozen-lockfile)"
else
  echo "  ⚠️ frozen-lockfile 失败，降级为 no-frozen-lockfile"
  pnpm install --no-frozen-lockfile --ignore-scripts --config.confirmModulesPurge=false
fi

# ---- 5. 执行构建 ----
echo ""
echo "🔨 开始构建 (环境: $BUILD_TARGET)..."

case "$BUILD_TARGET" in
  prt)
    pnpm build:prt
    ;;
  staging)
    pnpm build:staging
    ;;
  production)
    pnpm build
    ;;
  *)
    echo "❌ 未知的构建环境: $BUILD_TARGET"
    exit 1
    ;;
esac

# ---- 6. 构建产物校验 ----
echo ""
if [ -f "dist/index.html" ]; then
  echo "✅ 构建成功！dist/index.html 已生成"
  echo "  产物大小: $(du -sh dist | cut -f1)"
  echo "  文件数量: $(find dist -type f | wc -l | tr -d ' ')"
else
  echo "❌ 构建失败！dist/index.html 不存在"
  exit 1
fi

echo ""
echo "=========================================="
echo "构建完成 🎉"
echo "=========================================="
