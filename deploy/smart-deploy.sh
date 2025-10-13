#!/bin/bash

# SSH Collector Pro 智能端口部署脚本
# 自动检测可用端口并部署容器

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
CONTAINER_NAME="ssh-collector-pro"
IMAGE_NAME="ssh-collector-pro"
INTERNAL_PORT=18000
DEFAULT_EXTERNAL_PORT=18000
MAX_PORT_SCAN=18100

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查端口是否被占用
check_port() {
    local port=$1
    if netstat -tuln | grep -q ":${port} "; then
        return 1  # 端口被占用
    else
        return 0  # 端口可用
    fi
}

# 查找可用端口
find_available_port() {
    local start_port=$1
    local max_port=$2
    
    log_info "从端口 $start_port 开始扫描可用端口..." >&2
    
    for ((port=start_port; port<=max_port; port++)); do
        if check_port $port; then
            echo $port
            return 0
        fi
    done
    
    log_error "在范围 $start_port-$max_port 内未找到可用端口" >&2
    return 1
}

# 停止并删除现有容器
cleanup_container() {
    log_info "清理现有容器..."
    
    if docker ps -a --format "table {{.Names}}" | grep -q "^${CONTAINER_NAME}$"; then
        log_warning "发现现有容器 ${CONTAINER_NAME}，正在停止并删除..."
        docker stop ${CONTAINER_NAME} >/dev/null 2>&1 || true
        docker rm ${CONTAINER_NAME} >/dev/null 2>&1 || true
        log_success "容器清理完成"
    else
        log_info "未发现现有容器"
    fi
}

# 检查Docker镜像
check_image() {
    if ! docker images --format "table {{.Repository}}" | grep -q "^${IMAGE_NAME}$"; then
        log_error "Docker镜像 ${IMAGE_NAME} 不存在"
        log_info "请先构建镜像: docker build -f deploy/Dockerfile -t ${IMAGE_NAME} ."
        exit 1
    fi
    log_success "Docker镜像 ${IMAGE_NAME} 已存在"
}

# 启动容器
start_container() {
    local external_port=$1
    
    log_info "启动容器，端口映射: ${external_port}:${INTERNAL_PORT}"
    
    # 确保必要的目录存在
    mkdir -p data logs configs
    
    # 设置目录权限
    sudo chown -R 1001:1001 data logs 2>/dev/null || {
        log_warning "无法设置目录权限，可能需要手动处理"
    }
    
    # 启动容器
    docker run -d \
        --name ${CONTAINER_NAME} \
        -p ${external_port}:${INTERNAL_PORT} \
        -v $(pwd)/data:/app/data \
        -v $(pwd)/logs:/app/logs \
        -v $(pwd)/configs:/app/configs \
        --restart unless-stopped \
        ${IMAGE_NAME}
    
    if [ $? -eq 0 ]; then
        log_success "容器启动成功"
        return 0
    else
        log_error "容器启动失败"
        return 1
    fi
}

# 等待服务就绪
wait_for_service() {
    local port=$1
    local max_attempts=30
    local attempt=1
    
    log_info "等待服务就绪 (端口: $port)..."
    
    while [ $attempt -le $max_attempts ]; do
        if curl -f -s "http://localhost:$port/api/v1/health" >/dev/null 2>&1; then
            log_success "服务已就绪"
            return 0
        fi
        
        echo -n "."
        sleep 2
        attempt=$((attempt + 1))
    done
    
    echo ""
    log_error "服务启动超时"
    return 1
}

# 显示服务信息
show_service_info() {
    local port=$1
    local server_ip=$(hostname -I | awk '{print $1}')
    
    echo ""
    echo "=========================================="
    log_success "SSH Collector Pro 部署完成!"
    echo "=========================================="
    echo -e "${BLUE}容器名称:${NC} ${CONTAINER_NAME}"
    echo -e "${BLUE}外部端口:${NC} ${port}"
    echo -e "${BLUE}内部端口:${NC} ${INTERNAL_PORT}"
    echo -e "${BLUE}本地访问:${NC} http://localhost:${port}"
    echo -e "${BLUE}外网访问:${NC} http://${server_ip}:${port}"
    echo ""
    echo -e "${BLUE}健康检查:${NC} curl http://localhost:${port}/api/v1/health"
    echo -e "${BLUE}查看日志:${NC} docker logs ${CONTAINER_NAME}"
    echo -e "${BLUE}停止服务:${NC} docker stop ${CONTAINER_NAME}"
    echo "=========================================="
}

# 主函数
main() {
    log_info "开始 SSH Collector Pro 智能部署..."
    
    # 检查Docker是否运行
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker 未运行或无权限访问"
        exit 1
    fi
    
    # 检查镜像
    check_image
    
    # 清理现有容器
    cleanup_container
    
    # 查找可用端口
    available_port=$(find_available_port $DEFAULT_EXTERNAL_PORT $MAX_PORT_SCAN)
    if [ $? -ne 0 ]; then
        exit 1
    fi
    
    if [ "$available_port" = "$DEFAULT_EXTERNAL_PORT" ]; then
        log_success "默认端口 $DEFAULT_EXTERNAL_PORT 可用"
    else
        log_warning "默认端口 $DEFAULT_EXTERNAL_PORT 被占用，使用端口 $available_port"
    fi
    
    # 启动容器
    if start_container $available_port; then
        # 等待服务就绪
        if wait_for_service $available_port; then
            show_service_info $available_port
        else
            log_error "服务启动失败，查看容器日志: docker logs ${CONTAINER_NAME}"
            exit 1
        fi
    else
        exit 1
    fi
}

# 脚本入口
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi