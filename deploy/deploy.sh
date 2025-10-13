#!/bin/bash

# SSH采集器部署脚本
# 用于一键部署SSH采集器及其依赖服务

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
PROJECT_NAME="sshcollector"

# 路径设置（脚本所在目录与仓库根目录）
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  SSH采集器部署脚本${NC}"
echo -e "${BLUE}========================================${NC}"

# 检查Docker环境
check_docker() {
    echo -e "${YELLOW}检查Docker环境...${NC}"
    
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}错误: 未找到Docker${NC}"
        echo "请先安装Docker: https://docs.docker.com/get-docker/"
        exit 1
    fi
    
    if ! command -v docker-compose &> /dev/null; then
        echo -e "${RED}错误: 未找到Docker Compose${NC}"
        echo "请先安装Docker Compose: https://docs.docker.com/compose/install/"
        exit 1
    fi
    
    # 检查Docker是否运行
    if ! docker info &> /dev/null; then
        echo -e "${RED}错误: Docker服务未运行${NC}"
        echo "请启动Docker服务"
        exit 1
    fi
    
    echo -e "${GREEN}Docker环境检查通过${NC}"
}

# 创建必要的目录
create_directories() {
    echo -e "${YELLOW}创建必要的目录...${NC}"
    
    # 根目录数据与日志目录
    mkdir -p "$ROOT_DIR/data" "$ROOT_DIR/logs"
    
    # 部署相关目录（与docker-compose.yml同目录）
    mkdir -p "$SCRIPT_DIR/scripts"
    
    # 设置权限
    chmod 755 "$ROOT_DIR/data" "$ROOT_DIR/logs"
    
    echo -e "${GREEN}目录创建完成${NC}"
}


# 创建Nginx配置
# 已移除 Nginx 配置生成

# 已移除监控配置生成

# 启动服务
start_services() {
    echo -e "${YELLOW}启动服务...${NC}"
    
    # 停止现有服务
    docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} down --remove-orphans
    
    # 构建并启动服务
    docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} up -d --build
    
    echo -e "${GREEN}服务启动完成${NC}"
}

# 等待服务就绪
wait_for_services() {
    echo -e "${YELLOW}等待服务就绪...${NC}"
    
    
    # 无需等待 Redis
    
    # 等待SSH采集器就绪
    echo -e "${YELLOW}等待SSH采集器就绪...${NC}"
    timeout=60
    while [ $timeout -gt 0 ]; do
        if curl -f http://localhost:18000/health &> /dev/null; then
            echo -e "${GREEN}SSH采集器已就绪${NC}"
            break
        fi
        sleep 2
        timeout=$((timeout-2))
    done
}

# 显示服务状态
show_status() {
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}  部署完成${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "${GREEN}服务访问地址:${NC}"
    echo -e "  SSH采集器API: ${BLUE}http://localhost:18000${NC}"
    # 已移除 Prometheus 与 Grafana
    # 已移除 Nginx 代理
    echo ""
    echo -e "${GREEN}默认账号密码:${NC}"
    # 已移除 Grafana 相关信息
    echo ""
    echo -e "${GREEN}服务状态:${NC}"
    docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} ps
    echo ""
    echo -e "${GREEN}部署成功! 🎉${NC}"
}

# 主函数
main() {
    case "${1:-deploy}" in
        "deploy")
            check_docker
            create_directories
            # 已移除 Nginx 与监控配置生成
            start_services
            wait_for_services
            show_status
            ;;
        "start")
            docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} start
            echo -e "${GREEN}服务已启动${NC}"
            ;;
        "stop")
            docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} stop
            echo -e "${GREEN}服务已停止${NC}"
            ;;
        "restart")
            docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} restart
            echo -e "${GREEN}服务已重启${NC}"
            ;;
        "status")
            docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} ps
            ;;
        "logs")
            docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} logs -f ${2:-}
            ;;
        "clean")
            docker-compose -f "$COMPOSE_FILE" -p ${PROJECT_NAME} down --volumes --remove-orphans
            docker system prune -f
            echo -e "${GREEN}清理完成${NC}"
            ;;
        *)
            echo "用法: $0 {deploy|start|stop|restart|status|logs|clean}"
            echo ""
            echo "命令说明:"
            echo "  deploy  - 部署所有服务（默认）"
            echo "  start   - 启动服务"
            echo "  stop    - 停止服务"
            echo "  restart - 重启服务"
            echo "  status  - 查看服务状态"
            echo "  logs    - 查看服务日志"
            echo "  clean   - 清理所有容器和数据"
            exit 1
            ;;
    esac
}

# 执行主函数
main "$@"