# 优化版 Dockerfile - 利用缓存加速构建
# 如果依赖没变，会使用缓存，大幅加速

# 第一阶段：编译前端
FROM node:20-slim AS frontend-builder

WORKDIR /build
ENV NODE_OPTIONS="--max-old-space-size=4096"

# 先复制依赖文件，利用缓存
COPY web/package.json web/package-lock.json ./web/
WORKDIR /build/web
RUN npm install --legacy-peer-deps

# 再复制源码
COPY web/ ./
COPY VERSION ../
RUN DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../VERSION) npm run build

# 第二阶段：编译后端
FROM golang:1.25 AS backend-builder

ENV GO111MODULE=on CGO_ENABLED=0
ENV GOOS=linux GOARCH=amd64

WORKDIR /build

# 先复制依赖，利用缓存
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码
COPY . .
COPY --from=frontend-builder /build/web/dist ./web/dist
RUN go build -ldflags "-s -w" -o new-api

# 第三阶段：最终镜像
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata curl gzip && \
    rm -rf /var/lib/apt/lists/*

# 下载 mihomo
RUN curl -L -o /tmp/mihomo.gz "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.20/mihomo-linux-amd64-v1.19.20.gz" && \
    gunzip /tmp/mihomo.gz && mv /tmp/mihomo /usr/local/bin/mihomo && chmod +x /usr/local/bin/mihomo && /usr/local/bin/mihomo -v

RUN mkdir -p /data/clash && \
    curl -L -o /data/clash/geoip.metadb "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.metadb" || true

# 启动脚本
RUN printf '#!/bin/bash\nset -e\nCLASH_CONFIG_PATH=${CLASH_CONFIG_PATH:-/data/clash}\nCLASH_PROXY_GROUP=${CLASH_PROXY_GROUP:-🔰国外流量}\nmkdir -p "$CLASH_CONFIG_PATH"\nif [ -n "$CLASH_SUBSCRIBE_URL" ]; then\n    curl -L -o "$CLASH_CONFIG_PATH/config.yaml" -H "User-Agent: clash-meta" "$CLASH_SUBSCRIBE_URL" || true\nfi\nif [ -f "$CLASH_CONFIG_PATH/config.yaml" ]; then\n    cd "$CLASH_CONFIG_PATH" && /usr/local/bin/mihomo -d . &\n    sleep 3\nfi\n# 设置代理环境变量（无论是否有配置文件都设置，因为mihomo可能已启动）\nexport HTTP_PROXY=http://127.0.0.1:7890\nexport HTTPS_PROXY=http://127.0.0.1:7890\nexport CLASH_API_URL=http://127.0.0.1:9090\nexec /new-api\n' > /entrypoint.sh && chmod +x /entrypoint.sh

COPY --from=backend-builder /build/new-api /

EXPOSE 3000 7890 9090
WORKDIR /data
ENTRYPOINT ["/entrypoint.sh"]