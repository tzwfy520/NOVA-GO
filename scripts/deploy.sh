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
COMPOSE_FILE="docker-compose.yml"
PROJECT_NAME="sshcollector"

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
    
    mkdir -p data logs nginx/conf.d nginx/ssl monitoring/grafana/dashboards scripts
    
    # 设置权限
    chmod 755 data logs
    
    echo -e "${GREEN}目录创建完成${NC}"
}

# 下载XXL-Job SQL初始化脚本
download_xxljob_sql() {
    echo -e "${YELLOW}下载XXL-Job SQL初始化脚本...${NC}"
    
    if [ ! -f "scripts/xxl-job-mysql.sql" ]; then
        curl -fsSL https://raw.githubusercontent.com/xuxueli/xxl-job/2.4.0/doc/db/tables_xxl_job.sql \
            -o scripts/xxl-job-mysql.sql
        echo -e "${GREEN}XXL-Job SQL脚本下载完成${NC}"
    else
        echo -e "${GREEN}XXL-Job SQL脚本已存在${NC}"
    fi
}

# 创建Nginx配置
create_nginx_config() {
    echo -e "${YELLOW}创建Nginx配置...${NC}"
    
    cat > nginx/nginx.conf << 'EOF'
user nginx;
worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
    use epoll;
    multi_accept on;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;
    
    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';
    
    access_log /var/log/nginx/access.log main;
    
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;
    
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_types
        text/plain
        text/css
        text/xml
        text/javascript
        application/json
        application/javascript
        application/xml+rss
        application/atom+xml
        image/svg+xml;
    
    include /etc/nginx/conf.d/*.conf;
}
EOF

    cat > nginx/conf.d/default.conf << 'EOF'
# SSH采集器API
upstream sshcollector_api {
    server sshcollector:8080;
}

# XXL-Job管理后台
upstream xxljob_admin {
    server xxl-job-admin:8080;
}

server {
    listen 80;
    server_name localhost;
    
    # 健康检查
    location /health {
        proxy_pass http://sshcollector_api/health;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    
    # SSH采集器API
    location /api/ {
        proxy_pass http://sshcollector_api/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # 超时设置
        proxy_connect_timeout 30s;
        proxy_send_timeout 30s;
        proxy_read_timeout 30s;
    }
    
    # XXL-Job管理后台
    location /xxl-job-admin/ {
        proxy_pass http://xxljob_admin/xxl-job-admin/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    
    # 默认页面
    location / {
        return 200 'SSH Collector Pro is running!';
        add_header Content-Type text/plain;
    }
}
EOF

    echo -e "${GREEN}Nginx配置创建完成${NC}"
}

# 创建监控配置
create_monitoring_config() {
    echo -e "${YELLOW}创建监控配置...${NC}"
    
    mkdir -p monitoring/grafana/provisioning/datasources monitoring/grafana/provisioning/dashboards
    
    # Prometheus配置
    cat > monitoring/prometheus.yml << 'EOF'
global:
  scrape_interval: 15s
  evaluation_interval: 15s

rule_files:
  # - "first_rules.yml"
  # - "second_rules.yml"

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  - job_name: 'sshcollector'
    static_configs:
      - targets: ['sshcollector:8080']
    metrics_path: '/metrics'
    scrape_interval: 30s

  - job_name: 'redis'
    static_configs:
      - targets: ['redis:6379']

  - job_name: 'mysql'
    static_configs:
      - targets: ['mysql:3306']
EOF

    # Grafana数据源配置
    cat > monitoring/grafana/provisioning/datasources/prometheus.yml << 'EOF'
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: true
EOF

    # Grafana仪表板配置
    cat > monitoring/grafana/provisioning/dashboards/dashboard.yml << 'EOF'
apiVersion: 1

providers:
  - name: 'default'
    orgId: 1
    folder: ''
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
EOF

    echo -e "${GREEN}监控配置创建完成${NC}"
}

# 启动服务
start_services() {
    echo -e "${YELLOW}启动服务...${NC}"
    
    # 停止现有服务
    docker-compose -p ${PROJECT_NAME} down --remove-orphans
    
    # 构建并启动服务
    docker-compose -p ${PROJECT_NAME} up -d --build
    
    echo -e "${GREEN}服务启动完成${NC}"
}

# 等待服务就绪
wait_for_services() {
    echo -e "${YELLOW}等待服务就绪...${NC}"
    
    # 等待MySQL就绪
    echo -e "${YELLOW}等待MySQL就绪...${NC}"
    timeout=60
    while [ $timeout -gt 0 ]; do
        if docker-compose -p ${PROJECT_NAME} exec -T mysql mysqladmin ping -h localhost -u root -p123456 --silent; then
            echo -e "${GREEN}MySQL已就绪${NC}"
            break
        fi
        sleep 2
        timeout=$((timeout-2))
    done
    
    # 等待Redis就绪
    echo -e "${YELLOW}等待Redis就绪...${NC}"
    timeout=30
    while [ $timeout -gt 0 ]; do
        if docker-compose -p ${PROJECT_NAME} exec -T redis redis-cli ping; then
            echo -e "${GREEN}Redis已就绪${NC}"
            break
        fi
        sleep 2
        timeout=$((timeout-2))
    done
    
    # 等待SSH采集器就绪
    echo -e "${YELLOW}等待SSH采集器就绪...${NC}"
    timeout=60
    while [ $timeout -gt 0 ]; do
        if curl -f http://localhost:8080/health &> /dev/null; then
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
    echo -e "  SSH采集器API: ${BLUE}http://localhost:8080${NC}"
    echo -e "  XXL-Job管理后台: ${BLUE}http://localhost:8081/xxl-job-admin${NC}"
    echo -e "  Prometheus监控: ${BLUE}http://localhost:9090${NC}"
    echo -e "  Grafana可视化: ${BLUE}http://localhost:3000${NC}"
    echo -e "  Nginx代理: ${BLUE}http://localhost${NC}"
    echo ""
    echo -e "${GREEN}默认账号密码:${NC}"
    echo -e "  XXL-Job: ${YELLOW}admin/123456${NC}"
    echo -e "  Grafana: ${YELLOW}admin/admin123${NC}"
    echo ""
    echo -e "${GREEN}服务状态:${NC}"
    docker-compose -p ${PROJECT_NAME} ps
    echo ""
    echo -e "${GREEN}部署成功! 🎉${NC}"
}

# 主函数
main() {
    case "${1:-deploy}" in
        "deploy")
            check_docker
            create_directories
            download_xxljob_sql
            create_nginx_config
            create_monitoring_config
            start_services
            wait_for_services
            show_status
            ;;
        "start")
            docker-compose -p ${PROJECT_NAME} start
            echo -e "${GREEN}服务已启动${NC}"
            ;;
        "stop")
            docker-compose -p ${PROJECT_NAME} stop
            echo -e "${GREEN}服务已停止${NC}"
            ;;
        "restart")
            docker-compose -p ${PROJECT_NAME} restart
            echo -e "${GREEN}服务已重启${NC}"
            ;;
        "status")
            docker-compose -p ${PROJECT_NAME} ps
            ;;
        "logs")
            docker-compose -p ${PROJECT_NAME} logs -f ${2:-}
            ;;
        "clean")
            docker-compose -p ${PROJECT_NAME} down --volumes --remove-orphans
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