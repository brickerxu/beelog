#!/bin/bash

set -e

APP_NAME="beelog"
VERSION=$(grep '^version' Cargo.toml | head -n1 | sed -E 's/version\s*=\s*"(.*)"/\1/')
OUT_DIR="dist"
BUILD_MATRIX_FILE="build-matrix.json"

# 检查 jq 是否存在
if ! command -v jq &>/dev/null; then
    echo "❌ jq 未安装，请先安装 jq"
    exit 1
fi

# 检查配置文件
if [[ ! -f "$BUILD_MATRIX_FILE" ]]; then
    echo "❌ 未找到配置文件 $BUILD_MATRIX_FILE"
    exit 1
fi

# 安装 cross，如果没装的话
if ! command -v cross &> /dev/null; then
    echo "安装 cross..."
    cargo install cross
fi

mkdir -p "$OUT_DIR"

echo "🚀 构建 $APP_NAME version $VERSION"

# 解析 JSON 并遍历 targets
COUNT=$(jq '.targets | length' "$BUILD_MATRIX_FILE")
for ((i = 0; i < COUNT; i++)); do
    TARGET=$(jq -r ".targets[$i].os" "$BUILD_MATRIX_FILE")
    ALIAS=$(jq -r ".targets[$i].os_name" "$BUILD_MATRIX_FILE")

    echo "🔧 构建 $TARGET ..."

    if [[ "$TARGET" == "aarch64-apple-darwin" && "$OSTYPE" != "darwin"* ]]; then
            echo "⚠️  macOS 的构建必须在macOs的机器上. 跳过."
            continue
        fi

        cross build --release --target "$TARGET"

        BIN_NAME="$APP_NAME"
        [[ "$TARGET" == *"windows"* ]] && BIN_NAME="$APP_NAME.exe"

        BUILD_PATH="target/$TARGET/release/$BIN_NAME"
        PACKAGE_NAME="${APP_NAME}-${ALIAS}"
        BIN_PATH="${PACKAGE_NAME}/bin"

        mkdir -p "$OUT_DIR/$BIN_PATH"
        cp "$BUILD_PATH" "$OUT_DIR/$BIN_PATH/"

        echo "📦 打包 $PACKAGE_NAME..."
        cd "$OUT_DIR"
        zip -r "${PACKAGE_NAME}.zip" "$PACKAGE_NAME"
        rm -rf "$PACKAGE_NAME"
        cd ..
    echo $TARGET
done

echo "✅ 全部构建完成. 输出 ===> ./$OUT_DIR"