package operation_setting

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

// ProxySetting 代理设置
type ProxySetting struct {
	// Clash 配置
	ClashApiUrl     string `json:"clash_api_url"`      // Clash API 地址
	ClashProxyGroup string `json:"clash_proxy_group"`  // 代理组名称
	
	// 智能选择配置
	AutoSelect      bool  `json:"auto_select"`       // 是否自动选择最优节点
	MaxDelay        int   `json:"max_delay"`         // 最大可接受延迟(ms)
	CooldownSecs    int64 `json:"cooldown_secs"`     // 节点冷却时间(秒)
	
	// 测试配置
	TestUrl         string `json:"test_url"`          // 测试延迟的URL
	
	// 功能开关
	ProxyEnabled    bool  `json:"proxy_enabled"`      // 是否启用代理功能
	NodeRotation    bool  `json:"node_rotation"`      // 是否启用节点轮换
}

// 默认配置
var proxySetting = ProxySetting{
	ClashApiUrl:     getEnvOrDefault("CLASH_API_URL", "http://localhost:9090"),
	ClashProxyGroup: getEnvOrDefault("CLASH_PROXY_GROUP", "🔰国外流量"),
	AutoSelect:      true,
	MaxDelay:        1000,
	CooldownSecs:    60,
	TestUrl:         "https://api.airforce/v1/models",
	ProxyEnabled:    os.Getenv("CLASH_API_URL") != "",
	NodeRotation:    true,
}

var proxySettingMutex sync.RWMutex

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("proxy_setting", &proxySetting)
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// GetProxySetting 获取代理设置
func GetProxySetting() *ProxySetting {
	proxySettingMutex.RLock()
	defer proxySettingMutex.RUnlock()
	return &proxySetting
}

// UpdateProxySetting 更新代理设置
func UpdateProxySetting(newSetting *ProxySetting) {
	proxySettingMutex.Lock()
	defer proxySettingMutex.Unlock()
	
	if newSetting.ClashApiUrl != "" {
		proxySetting.ClashApiUrl = newSetting.ClashApiUrl
	}
	if newSetting.ClashProxyGroup != "" {
		proxySetting.ClashProxyGroup = newSetting.ClashProxyGroup
	}
	proxySetting.AutoSelect = newSetting.AutoSelect
	if newSetting.MaxDelay > 0 {
		proxySetting.MaxDelay = newSetting.MaxDelay
	}
	if newSetting.CooldownSecs > 0 {
		proxySetting.CooldownSecs = newSetting.CooldownSecs
	}
	if newSetting.TestUrl != "" {
		proxySetting.TestUrl = newSetting.TestUrl
	}
	proxySetting.ProxyEnabled = newSetting.ProxyEnabled
	proxySetting.NodeRotation = newSetting.NodeRotation
}

// ProxySetting2JsonString 将代理设置转换为JSON字符串
func ProxySetting2JsonString() string {
	proxySettingMutex.RLock()
	defer proxySettingMutex.RUnlock()
	bytes, err := json.Marshal(&proxySetting)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

// IsProxyEnabled 检查是否启用代理
func IsProxyEnabled() bool {
	proxySettingMutex.RLock()
	defer proxySettingMutex.RUnlock()
	return proxySetting.ProxyEnabled && proxySetting.ClashApiUrl != ""
}

// IsNodeRotationEnabled 检查是否启用节点轮换
func IsNodeRotationEnabled() bool {
	proxySettingMutex.RLock()
	defer proxySettingMutex.RUnlock()
	return proxySetting.NodeRotation && proxySetting.ProxyEnabled
}

// ParseProxySettingFromEnv 从环境变量解析代理设置
func ParseProxySettingFromEnv() {
	proxySettingMutex.Lock()
	defer proxySettingMutex.Unlock()
	
	if val := os.Getenv("CLASH_API_URL"); val != "" {
		proxySetting.ClashApiUrl = val
		proxySetting.ProxyEnabled = true
	}
	if val := os.Getenv("CLASH_PROXY_GROUP"); val != "" {
		proxySetting.ClashProxyGroup = val
	}
	if val := os.Getenv("CLASH_AUTO_SELECT"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			proxySetting.AutoSelect = b
		}
	}
	if val := os.Getenv("CLASH_MAX_DELAY"); val != "" {
		if i, err := strconv.Atoi(val); err == nil && i > 0 {
			proxySetting.MaxDelay = i
		}
	}
	if val := os.Getenv("CLASH_COOLDOWN_SECS"); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil && i > 0 {
			proxySetting.CooldownSecs = i
		}
	}
}
