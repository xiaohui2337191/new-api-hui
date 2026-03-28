# New API Hui

基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 的个人定制版本，专门针对 AirForce 渠道优化。

## 新增功能

### TLS 指纹随机化（绕过 AirForce 限流）
绕过 AirForce 的 TLS 指纹检测，每次请求随机选择浏览器指纹（Chrome、Firefox、Safari、Edge、iOS），支持 HTTP/SOCKS5 代理。

### Clash 代理池管理
- 智能节点选择 - 优先选择延迟低且不在冷却的节点
- 短暂冷却机制 - 请求成功后标记节点冷却，避免触发 AirForce 限流
- 并行测速 - 使用 worker pool 并行测试节点延迟
- 自动测速 - 服务启动时自动开启定时测速

### 令牌桶限流器
针对 AirForce 的平滑限流，避免突发请求触发全局限流，支持全局 IP 限流和 Key 级别限流。

### AirForce 批量注册脚本
提供 `airforce_register.py` 脚本，使用 DrissionPage 自动化注册 AirForce 账号并获取 API Key。

```bash
# 安装依赖
pip install DrissionPage

# 运行
python airforce_register.py
```

## 环境变量配置

```bash
# Clash 代理配置
CLASH_API_URL=http://127.0.0.1:9090
CLASH_PROXY_GROUP=🔰国外流量

# HTTP 代理（用于 TLS 指纹通过代理）
HTTP_PROXY=http://127.0.0.1:7890
HTTPS_PROXY=http://127.0.0.1:7890

# Redis 缓存
REDIS_CONN_STRING=redis://localhost:6379
MEMORY_CACHE_ENABLED=true

# 调试模式
DEBUG=false
```

## 快速开始

```bash
# 构建
go build -ldflags "-s -w" -o new-api

# 运行
./new-api --port 3000
```

## 联系方式

QQ: 1424823965

## 致谢

- [QuantumNous/new-api](https://github.com/QuantumNous/new-api)
- 原项目 [Calcium-Ion/new-api](https://github.com/Calcium-Ion/new-api)