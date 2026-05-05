#!/bin/sh
# 自签名证书生成脚本（Docker 容器启动时执行）
# 如果证书不存在，自动生成自签名证书

SSL_DIR="/etc/nginx/ssl"
CERT_FILE="$SSL_DIR/server.crt"
KEY_FILE="$SSL_DIR/server.key"
NGINX_MODE="${NGINX_MODE:-auto}"

if [ "$NGINX_MODE" = "http" ]; then
    echo "[INFO] HTTP mode, skip SSL certificate generation"
    exit 0
fi

if [ "$NGINX_MODE" = "auto" ] && [ ! -f "$CERT_FILE" ] && [ ! -f "$KEY_FILE" ]; then
    echo "[INFO] no mounted SSL certificate, auto mode uses HTTP"
    exit 0
fi

# 检查证书是否存在
if [ ! -f "$CERT_FILE" ] || [ ! -f "$KEY_FILE" ]; then
    echo "[INFO] SSL 证书不存在，正在生成自签名证书..."

    # 获取服务器 IP（从环境变量或使用默认值）
    SERVER_IP="${SERVER_IP:-localhost}"

    # 生成自签名证书
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$KEY_FILE" \
        -out "$CERT_FILE" \
        -subj "/CN=${SERVER_IP}" \
        -addext "subjectAltName=DNS:${SERVER_IP},DNS:localhost,IP:${SERVER_IP},IP:127.0.0.1" \
        2>/dev/null

    if [ $? -eq 0 ]; then
        echo "[SUCCESS] 自签名证书生成成功"
        echo "[INFO] 证书位置: $CERT_FILE"
        echo "[INFO] 私钥位置: $KEY_FILE"
        echo "[WARNING] 这是自签名证书，浏览器会显示安全警告"
    else
        echo "[ERROR] 证书生成失败"
        exit 1
    fi
else
    echo "[INFO] SSL 证书已存在，跳过生成"
fi
