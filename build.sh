#!/bin/bash

# 全局变量（默认值）
builduser="aspnmy" # 镜像仓库用户名
buildname="ollama-scanner" # 镜像名称
buildver="v2.2" # 镜像版本
buildurl="docker.io" # 镜像推送的 URL 位置

TELEGRAM_BOT_TOKEN="your_telegram_bot_token" # Telegram Bot Token
TELEGRAM_CHAT_ID="your_telegram_chat_id" # Telegram 群组 Chat ID

release_dir="Releases/$buildver" # 发布目录


builddir_core="dockerfile-ollama-scaner" 
builddir_mongodb="dockerfile-ollama-scaner-mongoDB" 
builddir_core_arm64="dockerfile-ollama-scaner-arm64" 
builddir_mongodb_arm64="dockerfile-ollama-scaner-arm64-mongoDB" 


# 初始化全局变量
init() {
    local env_file="./env.json"

    # 检查 env.json 文件是否存在
    if [ ! -f "$env_file" ]; then
        echo "警告: env.json 文件不存在,使用默认值."
        return
    fi

    # 使用 jq 读取 env.json 文件并更新全局变量
    if command -v jq &> /dev/null; then
        builduser=$(jq -r '.builduser // empty' "$env_file")
        if [ -n "$builduser" ]; then
            echo "从 env.json 读取 builduser: $builduser"
        else
            builduser="aspnmy"
        fi

        buildname=$(jq -r '.buildname // empty' "$env_file")
        if [ -n "$buildname" ]; then
            echo "从 env.json 读取 buildname: $buildname"

        else
            buildname="ollama-scanner"
        fi

        buildver=$(jq -r '.buildver // empty' "$env_file")
        if [ -n "$buildver" ]; then
            echo "从 env.json 读取 buildver: $buildver"
        else
            buildver="v2.2.0"
        fi

        buildurl=$(jq -r '.buildurl // empty' "$env_file")
        if [ -n "$buildurl" ]; then
            echo "从 env.json 读取 buildurl: $buildurl"
        else
            buildurl="docker.io"
        fi
        
        TELEGRAM_URI=$(jq -r '.TELEGRAM_URI // empty' "$env_file")
        if [ -n "$TELEGRAM_URI" ]; then
            echo "从 env.json 读取 TELEGRAM_URI: $TELEGRAM_URI"
        else
            TELEGRAM_URI="ELEGRAM_URI"
        fi

        TELEGRAM_BOT_TOKEN=$(jq -r '.TELEGRAM_BOT_TOKEN // empty' "$env_file")
        if [ -n "$TELEGRAM_BOT_TOKEN" ]; then
            echo "从 env.json 读取 TELEGRAM_BOT_TOKEN: $TELEGRAM_BOT_TOKEN"
        else
            TELEGRAM_BOT_TOKEN="your_telegram_bot_token"
        fi

        TELEGRAM_CHAT_ID=$(jq -r '.TELEGRAM_CHAT_ID // empty' "$env_file")
        if [ -n "$TELEGRAM_CHAT_ID" ]; then
            echo "从 env.json 读取 TELEGRAM_CHAT_ID: $TELEGRAM_CHAT_ID"
        else
            TELEGRAM_CHAT_ID="your_telegram_chat_id"
        fi

        release_dir=$(jq -r '.release_dir // empty' "$env_file")
        if [ -n "$release_dir" ]; then
            echo "从 env.json 读取 release_dir: $release_dir"
        else
            release_dir="Releases/$buildver"
        fi
    else
        echo "错误: jq 未安装,无法解析 env.json 文件."
        exit 1
    fi

    # 打印初始化后的全局变量
    # echo "初始化全局变量:"
    # echo "builduser=$builduser"
    # echo "buildname=$buildname"
    # echo "buildver=$buildver"
    # echo "buildurl=$buildurl"
    # echo "TELEGRAM_BOT_TOKEN=$TELEGRAM_BOT_TOKEN"
    # echo "TELEGRAM_CHAT_ID=$TELEGRAM_CHAT_ID"
    # echo "release_dir=$release_dir"
}

# 检测并安装 buildah
check_and_install_buildah() {
    if ! command -v buildah &> /dev/null; then
        echo "buildah 未安装,正在安装 buildah..."
        if command -v apt-get &> /dev/null; then
            sudo apt-get update && sudo apt-get install -y buildah
        elif command -v yum &> /dev/null; then
            sudo yum install -y buildah
        elif command -v dnf &> /dev/null; then
            sudo dnf install -y buildah
        else
            echo "无法自动安装 buildah,请手动安装后重试."
            exit 1
        fi
        echo "buildah 安装完成."
    else
        echo "buildah 已安装."
    fi
}

# 检测并安装 make
check_and_install_make() {
    if ! command -v make &> /dev/null; then
        echo "make 未安装,正在安装 make..."
        if command -v apt-get &> /dev/null; then
            sudo apt-get update && sudo apt-get install -y make
        elif command -v yum &> /dev/null; then
            sudo yum install -y make
        elif command -v dnf &> /dev/null; then
            sudo dnf install -y make
        else
            echo "无法自动安装 make,请手动安装后重试."
            exit 1
        fi
        echo "make 安装完成."
    else
        echo "make 已安装."
    fi
}

# 检测并安装 GitHub CLI (gh)
check_and_install_gh() {
    if ! command -v gh &> /dev/null; then
        echo "GitHub CLI (gh) 未安装,正在安装 gh..."
        if command -v apt-get &> /dev/null; then
            sudo apt-get update && sudo apt-get install -y gh
        elif command -v yum &> /dev/null; then
            sudo yum install -y gh
        elif command -v dnf &> /dev/null; then
            sudo dnf install -y gh
        else
            echo "无法自动安装 GitHub CLI (gh),请手动安装后重试."
            exit 1
        fi
        echo "GitHub CLI (gh) 安装完成."
    else
        echo "GitHub CLI (gh) 已安装."
    fi
}

# 使用 make 构建本体
build_makefile() {
     
    local ver=$1
    local make_res=$2
    # 检查 Makefile 是否存在
    local timeNowLocal=$(TZ='Asia/Shanghai' date "+%Y-%m-%d %H:%M:%S")
    #echo "${timeNowLocal}"
    

    echo "正在使用 makefile 构建程序本体..."

    # 在指定路径下执行 make 命令
    make  BIN_VER="$ver"
    if [ $? -eq 0 ]; then
       echo "成功构建程序本体,标签为 $ver"

       send_telegram_message "✅ $timeNowLocal 构建新版本成功:$ver,新增功能:$make_res"
    else
        echo "构建程序本体失败,标签为 $ver"
        exit 1
    fi
}

# 使用 buildah 构建 Docker 镜像
build_buildah_image() {
    local dockerfile=$1
    local tag=$2
    local ver=$3
    echo "正在使用 $dockerfile 构建镜像,标签为 $tag..."
    # 无缓存构建方式 保证镜像的一致性
    buildah bud --no-cache --build-arg version=$ver -f $dockerfile -t $tag

    if [ $? -eq 0 ]; then
        echo "成功构建镜像,标签为 $tag"
    else
        echo "构建镜像失败,标签为 $tag"
        exit 1
    fi
}

# 使用 buildx 跨架构构建 Docker 镜像
buildx_buildah_image() {
    local dockerfile=$1
    local tag=$2
    local ver=$3
    local platform=$4

    # 检查 QEMU 是否已安装,如果未安装则自动安装
    if ! command -v qemu-aarch64-static &> /dev/null; then
        echo "QEMU 未安装,正在自动安装 qemu-user-static..."
        if command -v apt-get &> /dev/null; then
            sudo apt-get update
            sudo apt-get install -y qemu-user-static
        elif command -v yum &> /dev/null; then
            sudo yum install -y qemu-user-static
        elif command -v dnf &> /dev/null; then
            sudo dnf install -y qemu-user-static
        elif command -v zypper &> /dev/null; then
            sudo zypper install -y qemu-user-static
        else
            echo "无法自动安装 QEMU,请手动安装 qemu-user-static."
            exit 1
        fi
    fi

    # 使用 buildah 构建跨架构镜像
    # 使用 --no-cache 参数禁用缓存,保证镜像的一致性
    buildah build \
        --platform $platform \
        --build-arg version=$ver \
        --file $dockerfile \
        --tag $tag \
        --no-cache


    # 检查构建是否成功
    if [ $? -eq 0 ]; then
        echo "成功构建镜像,标签为 $tag"
    else
        echo "构建镜像失败,标签为 $tag"
        exit 1
    fi
}




# 使用 buildah 推送镜像
push_buildah_image() {
    local tag=$1
    echo "正在推送镜像,标签为 $tag..."
    buildah push $tag
    if [ $? -eq 0 ]; then
        echo "成功推送镜像,标签为 $tag"
        send_telegram_message "✅ 镜像推送成功:$tag"
    else
        echo "推送镜像失败,标签为 $tag"
        send_telegram_message "❌ 镜像推送失败:$tag"
        exit 1
    fi
}


send_telegram_message() {
    local message=$1
    # 转义特殊字符
    message=$(echo "$message" | sed 's/"/\\"/g')
    # echo "正在发送 Telegram 消息: $message"

    # 发送请求并捕获响应
    res=$(curl -s -X POST "$TELEGRAM_URI/bot$TELEGRAM_BOT_TOKEN/sendMessage" \
        -H "Content-Type: application/json" \
        --data-raw "{\"chat_id\":\"$TELEGRAM_CHAT_ID\",\"message_thread_id\":\"$MESSAGE_THREAD_ID\",\"text\":\"$message\"}")

    # # 打印响应（调试用）
    # echo "Telegram API 响应: $res"

    # 检查 curl 是否成功
    if [ $? -ne 0 ]; then
        echo "Telegram 消息发送失败: curl 请求出错."
        return 1
    fi

    # 使用 jq 解析响应并检查 ok 字段
    if echo "$res" | jq -e '.ok == true' > /dev/null; then
        echo "Telegram 消息发送成功."
        return 0
    else
        echo "Telegram 消息发送失败: API 返回错误."
        echo "错误信息: $(echo "$res" | jq -r '.description')"
        return 1
    fi
}

# 动态生成 Release Notes
generate_release_notes() {
    local version=$1
    local notes=""

    # 添加标题
    notes+="# Release $version\n\n"

    # 添加构建信息
    notes+="## 构建信息\n"
    notes+="- 构建用户: $builduser\n"
    notes+="- 镜像名称: $buildname\n"
    notes+="- 镜像版本: $buildver\n"
    notes+="- 镜像仓库: $buildurl\n\n"

    # 添加 Git 提交历史
    if command -v git &> /dev/null; then
        notes+="## 提交历史\n"
        notes+="\`\`\`\n"
        notes+="$(git log --oneline -n 5)\n"
        notes+="\`\`\`\n\n"
    else
        notes+="## 提交历史\n"
        notes+="Git 未安装,无法获取提交历史.\n\n"
    fi

    # 添加构建日志
    notes+="## 构建日志\n"
    notes+="构建成功完成,所有镜像已推送至仓库.\n"

    echo -e "$notes"
}

# 发布到 GitHub Releases
publish_to_github_releases() {
    local version=$1
    local release_dir=$2



    echo "正在发布到 GitHub Releases,版本为 $version..."

    # 生成 Release Notes
    local release_notes
    release_notes=$(generate_release_notes "$version")

    # 创建 GitHub Release
    gh release create "$version" "$release_dir"/* --title "Release $version" --notes "$release_notes"
    if [ $? -eq 0 ]; then
        echo "成功发布到 GitHub Releases,版本为 $version"
        send_telegram_message "✅ GitHub Releases 发布成功:版本 $version"
    else
        echo "发布到 GitHub Releases 失败,版本为 $version"
        send_telegram_message "❌ GitHub Releases 发布失败:版本 $version"
        exit 1
    fi
}

# 主函数
main() {
    # 初始化全局变量
    init

    # 检测并安装 make
    check_and_install_make
    # 检测并安装 buildah
    check_and_install_buildah
    # 检测并安装 GitHub CLI (gh)
    check_and_install_gh

    # 使用 make 构建本体
    make_res="$buildver v2.2.1 //增加断点续扫功能 支持进度条显示
// 自动获取 eth0 网卡的 MAC 地址
// 在以下情况下尝试自动获取 MAC 地址
// 配置文件不存在时
// 配置文件中的 MAC 地址为空时
// 命令行参数未指定 MAC 地址时
// 获取失败时给出相应的错误提示
// 合并组件zmap和masscan,根据操作系统自动选择扫描器"
    build_makefile  "$buildver" "$make_res"

    # 发布到 GitHub Releases
    # publish_to_github_releases "$buildver" "$release_dir/$buildver"


    # 构建 core 镜像
    core_tag="$buildurl/$builduser/$buildname:$buildver"
    build_buildah_image $builddir_core $core_tag $buildver

    # 构建 mongodb 镜像
    mongodb_tag="$buildurl/$builduser/$buildname:$buildver-mongodb"
    build_buildah_image $builddir_mongodb $mongodb_tag $buildver

    # 构建 buildx 跨架构构建core_arm64 镜像
    build_platform="linux/arm64"
    arm64_tag="$buildurl/$builduser/$buildname:$buildver-arm64"
    buildx_buildah_image $builddir_core_arm64 $arm64_tag $buildver $build_platform

    # 构建 buildx 跨架构构建mongodb_arm64 镜像
    build_platform="linux/arm64"
    arm64_tag_mongodb="$buildurl/$builduser/$buildname:$buildver-arm64-mongodb"
    buildx_buildah_image $builddir_mongodb_arm64 $arm64_tag_mongodb $buildver $build_platform

    # 推送 core 镜像
    push_buildah_image $core_tag

    # 推送 mongodb 镜像
    push_buildah_image $mongodb_tag

    # 推送 arm64_tag 镜像
    push_buildah_image $arm64_tag

    # 推送 arm64_tag_mongodb 镜像
    push_buildah_image $arm64_tag_mongodb
}

# 执行主函数
main
