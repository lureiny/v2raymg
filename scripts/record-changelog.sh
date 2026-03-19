#!/bin/bash
# record-changelog.sh - 自动记录 CHANGELOG 条目
# 用法: ./scripts/record-changelog.sh "任务描述" "详细说明"
# 示例: ./scripts/record-changelog.sh "T-999 新增功能" "- 新增 xxx 功能\n- 修复 yyy 问题"

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
CHANGELOG="$PROJECT_ROOT/CHANGELOG.md"

if [ ! -f "$CHANGELOG" ]; then
    echo "Error: CHANGELOG.md not found at $CHANGELOG"
    exit 1
fi

if [ -z "$1" ]; then
    echo "用法: $0 \"任务描述\" \"详细说明\""
    echo "示例: $0 \"T-999 新增功能\" \"- 新增 xxx 功能\n- 修复 yyy 问题\""
    exit 1
fi

TASK_TITLE="$1"
DETAILS="$2"
DATE=$(date +%Y-%m-%d)

# 构建新条目
NEW_ENTRY="## $DATE — $TASK_TITLE"

if [ -n "$DETAILS" ]; then
    # 将多行详细说明格式化
    DETAILS_FORMATTED=$(echo "$DETAILS" | sed 's/^/- /')
    NEW_ENTRY="$NEW_ENTRY
$DETAILS_FORMATTED"
fi

# 在第二个 ## 之后插入新条目（第一个是标题）
# 找到第一个真正条目（不是 # CHANGELOG 标题）的位置
FIRST_ENTRY_LINE=$(grep -n "^## " "$CHANGELOG" | head -2 | tail -1 | cut -d: -f1)

if [ -z "$FIRST_ENTRY_LINE" ]; then
    # 如果没有现有条目，插入到标题之后
    NEW_CHANGELOG=$(cat <<EOF
# CHANGELOG.md

$NEW_ENTRY

EOF
)
    echo "$NEW_CHANGELOG" > "$CHANGELOG"
else
    # 在第一个条目之前插入
    HEAD=$(head -n $((FIRST_ENTRY_LINE - 1)) "$CHANGELOG")
    TAIL=$(tail -n +$((FIRST_ENTRY_LINE)) "$CHANGELOG")

    cat > "$CHANGELOG" <<EOF
$HEAD

$NEW_ENTRY
$TAIL
EOF
fi

echo "已记录: $TASK_TITLE ($DATE)"
