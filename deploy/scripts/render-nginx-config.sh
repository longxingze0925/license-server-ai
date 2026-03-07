#!/bin/sh
set -eu

TEMPLATE_FILE="${NGINX_CONFIG_TEMPLATE:-/opt/nginx-template/default.conf.template}"
OUTPUT_FILE="${NGINX_CONFIG_OUTPUT:-/etc/nginx/conf.d/default.conf}"
MAX_RELEASE_UPLOAD_MB="${MAX_RELEASE_UPLOAD_MB:-500}"

case "$MAX_RELEASE_UPLOAD_MB" in
    ''|*[!0-9]*)
        echo "[WARN] invalid MAX_RELEASE_UPLOAD_MB='$MAX_RELEASE_UPLOAD_MB', fallback to 500"
        MAX_RELEASE_UPLOAD_MB="500"
        ;;
esac

if [ ! -f "$TEMPLATE_FILE" ]; then
    echo "[WARN] nginx template not found: $TEMPLATE_FILE"
    exit 0
fi

export MAX_RELEASE_UPLOAD_MB
sed "s/\${MAX_RELEASE_UPLOAD_MB}/${MAX_RELEASE_UPLOAD_MB}/g" "$TEMPLATE_FILE" > "$OUTPUT_FILE"
echo "[INFO] rendered nginx config: client_max_body_size=${MAX_RELEASE_UPLOAD_MB}M"
