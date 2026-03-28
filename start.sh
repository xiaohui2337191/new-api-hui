#!/bin/bash
# New-API 启动脚本
# 支持同时启动 mihomo 代理和 new-api 服务

set -e

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# 默认配置
CLASH_CONFIG_PATH="${CLASH_CONFIG_PATH:-$SCRIPT_DIR/clash}"
CLASH_PROXY_GROUP="${CLASH_PROXY_GROUP:-🔰国外流量}"
CLASH_SUBSCRIBE_URL="${CLASH_SUBSCRIBE_URL:-}"
HTTP_PROXY="${HTTP_PROXY:-http://127.0.0.1:7890}"
HTTPS_PROXY="${HTTPS_PROXY:-http://127.0.0.1:7890}"
CLASH_API_URL="${CLASH_API_URL:-http://127.0.0.1:9090}"

# Redis 配置
REDIS_CONN_STRING="${REDIS_CONN_STRING:-}"
REDIS_HOST="${REDIS_HOST:-127.0.0.1}"
REDIS_PORT="${REDIS_PORT:-6379}"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 检查命令是否存在
check_cmd() {
    command -v "$1" >/dev/null 2>&1
}

# 启动 Redis (如果可用)
start_redis() {
    if [ -n "$REDIS_CONN_STRING" ]; then
        log_info "Redis 连接字符串已设置: $REDIS_CONN_STRING"
        return 0
    fi
    
    # 检查 redis-server 是否存在
    if ! type redis-server >/dev/null 2>&1; then
        log_warn "未找到 redis-server，继续不使用 Redis"
        return 1
    fi
    
    # 检查 Redis 是否已运行
    if redis-cli ping 2>/dev/null | grep -q PONG; then
        log_info "Redis 已在运行"
        export REDIS_CONN_STRING="redis://127.0.0.1:6379"
        return 0
    fi
    
    # 启动 Redis
    log_info "启动 Redis..."
    redis-server --daemonize yes 2>/dev/null
    sleep 1
    
    if redis-cli ping 2>/dev/null | grep -q PONG; then
        log_info "Redis 已启动"
        export REDIS_CONN_STRING="redis://127.0.0.1:6379"
        return 0
    else
        log_warn "Redis 启动失败，继续不使用 Redis"
        return 1
    fi
}

# 启动 mihomo 代理
start_mihomo() {
    local mihomo_path="$SCRIPT_DIR/clash/mihomo"
    
    if [ ! -f "$mihomo_path" ]; then
        log_warn "未找到 mihomo: $mihomo_path"
        return 1
    fi
    
    # 创建配置目录
    mkdir -p "$CLASH_CONFIG_PATH"
    
    # 下载订阅配置
    if [ -n "$CLASH_SUBSCRIBE_URL" ]; then
        log_info "下载订阅配置..."
        if curl -L -o "$CLASH_CONFIG_PATH/config.yaml" -H "User-Agent: clash-meta" "$CLASH_SUBSCRIBE_URL" 2>/dev/null; then
            log_info "订阅配置下载成功"
        else
            log_warn "订阅配置下载失败"
        fi
    fi
    
    # 检查配置文件
    if [ ! -f "$CLASH_CONFIG_PATH/config.yaml" ]; then
        log_warn "未找到 clash 配置文件: $CLASH_CONFIG_PATH/config.yaml"
        return 1
    fi
    
    # 复制 geoip 数据库
    if [ -f "$SCRIPT_DIR/clash/geoip.metadb" ] && [ ! -f "$CLASH_CONFIG_PATH/geoip.metadb" ]; then
        cp "$SCRIPT_DIR/clash/geoip.metadb" "$CLASH_CONFIG_PATH/"
    fi
    
    # 启动 mihomo
    log_info "启动 mihomo 代理..."
    cd "$CLASH_CONFIG_PATH"
    "$mihomo_path" -d . >/dev/null 2>&1 &
    MIHOMO_PID=$!
    cd "$SCRIPT_DIR"
    
    sleep 2
    
    if kill -0 $MIHOMO_PID 2>/dev/null; then
        log_info "mihomo 已启动 (PID: $MIHOMO_PID)"
        log_info "代理地址: $HTTP_PROXY"
        log_info "API 地址: $CLASH_API_URL"
        
        # 设置环境变量
        export HTTP_PROXY HTTPS_PROXY CLASH_API_URL CLASH_CONFIG_PATH CLASH_PROXY_GROUP
        return 0
    else
        log_error "mihomo 启动失败"
        return 1
    fi
}

# 启动 new-api
start_newapi() {
    # 检测架构
    local arch=$(uname -m)
    case $arch in
        x86_64|amd64)
            NEWAPI_BIN="new-api-linux-amd64"
            ;;
        aarch64|arm64)
            NEWAPI_BIN="new-api-linux-arm64"
            ;;
        *)
            NEWAPI_BIN="new-api"
            ;;
    esac
    
    local newapi_path="$SCRIPT_DIR/$NEWAPI_BIN"
    
    # 如果架构特定的二进制不存在，尝试默认的 new-api
    if [ ! -f "$newapi_path" ]; then
        newapi_path="$SCRIPT_DIR/new-api"
    fi
    
    if [ ! -f "$newapi_path" ]; then
        log_error "未找到 new-api 二进制文件"
        exit 1
    fi
    
    log_info "启动 new-api ($newapi_path)..."
    
    # 导出所有环境变量
    export HTTP_PROXY HTTPS_PROXY CLASH_API_URL CLASH_CONFIG_PATH CLASH_PROXY_GROUP
    [ -n "$REDIS_CONN_STRING" ] && export REDIS_CONN_STRING
    
    # 启动 new-api
    exec "$newapi_path"
}

# 停止所有服务
stop_all() {
    log_info "停止服务..."
    pkill -f "mihomo" 2>/dev/null || true
    pkill -f "new-api" 2>/dev/null || true
    log_info "服务已停止"
}

# 主逻辑
main() {
    case "${1:-start}" in
        start)
            log_info "======================================"
            log_info "New-API 启动器"
            log_info "======================================"
            
            # 启动 Redis
            
            # 启动 mihomo
            start_mihomo || log_warn "mihomo 未启动，将直连访问"
            
            # 启动 new-api
            start_newapi
            ;;
        stop)
            stop_all
            ;;
        restart)
            stop_all
            sleep 1
            $0 start
            ;;
        *)
            echo "用法: $0 {start|stop|restart}"
            echo ""
            echo "环境变量:"
            echo "  CLASH_SUBSCRIBE_URL - 订阅地址"
            echo "  CLASH_PROXY_GROUP   - 代理组名称 (默认: 🔰国外流量)"
            echo "  CLASH_CONFIG_PATH   - 配置目录 (默认: ./clash)"
            echo "  HTTP_PROXY          - HTTP 代理地址"
            echo "  HTTPS_PROXY         - HTTPS 代理地址"
            echo "  REDIS_CONN_STRING   - Redis 连接字符串"
            echo "  REDIS_HOST          - Redis 主机 (默认: 127.0.0.1)"
            echo "  REDIS_PORT          - Redis 端口 (默认: 6379)"
            exit 1
            ;;
    esac
}

main "$@"
