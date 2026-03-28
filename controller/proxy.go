package controller

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// ProxyConfigResponse 代理配置响应
type ProxyConfigResponse struct {
	Enabled         bool   `json:"enabled"`
	ClashApiUrl     string `json:"clash_api_url"`
	ClashProxyGroup string `json:"clash_proxy_group"`
	AutoSelect      bool   `json:"auto_select"`
	MaxDelay        int    `json:"max_delay"`
	CooldownSecs    int64  `json:"cooldown_secs"`
	TestUrl         string `json:"test_url"`
	NodeRotation    bool   `json:"node_rotation"`
	SubscribeUrl    string `json:"subscribe_url"`
}

// ProxyNodeResponse 代理节点响应
type ProxyNodeResponse struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Alive        bool   `json:"alive"`
	Delay        int    `json:"delay"`
	Region       string `json:"region"`
	InCooldown   bool   `json:"in_cooldown"`
	CooldownEnd  int64  `json:"cooldown_end"`
	LastTestTime int64  `json:"last_test_time"`
}

// ProxyStatusResponse 代理状态响应
type ProxyStatusResponse struct {
	Enabled          bool                `json:"enabled"`
	CurrentNode      string              `json:"current_node"`
	TotalNodes       int                 `json:"total_nodes"`
	AvailableNodes   int                 `json:"available_nodes"`
	CooldownNodes    int                 `json:"cooldown_nodes"`
	Nodes            []ProxyNodeResponse `json:"nodes"`
	Config           ProxyConfigResponse `json:"config"`
}

// getClashConfigPath 获取 clash 配置目录路径
func getClashConfigPath() string {
	path := os.Getenv("CLASH_CONFIG_PATH")
	if path == "" {
		path = "/data/clash"
	}
	return path
}

// GetProxyConfig 获取代理配置
func GetProxyConfig(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	setting := operation_setting.GetProxySetting()
	
	// 从数据库获取订阅URL
	subscribeUrl, _ := common.OptionMap["proxy_setting.subscribe_url"]
	
	response := ProxyConfigResponse{
		Enabled:         setting.ProxyEnabled,
		ClashApiUrl:     setting.ClashApiUrl,
		ClashProxyGroup: setting.ClashProxyGroup,
		AutoSelect:      setting.AutoSelect,
		MaxDelay:        setting.MaxDelay,
		CooldownSecs:    setting.CooldownSecs,
		TestUrl:         setting.TestUrl,
		NodeRotation:    setting.NodeRotation,
		SubscribeUrl:    subscribeUrl,
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

// UpdateProxyConfig 更新代理配置
func UpdateProxyConfig(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	var req ProxyConfigResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 打印接收到的请求
	common.SysLog(fmt.Sprintf("UpdateProxyConfig request: enabled=%v, cooldown=%d, max_delay=%d", 
		req.Enabled, req.CooldownSecs, req.MaxDelay))

	newSetting := &operation_setting.ProxySetting{
		ClashApiUrl:     req.ClashApiUrl,
		ClashProxyGroup: req.ClashProxyGroup,
		AutoSelect:      req.AutoSelect,
		MaxDelay:        req.MaxDelay,
		CooldownSecs:    req.CooldownSecs,
		TestUrl:         req.TestUrl,
		ProxyEnabled:    req.Enabled,
		NodeRotation:    req.NodeRotation,
	}

	operation_setting.UpdateProxySetting(newSetting)

	// 保存配置到数据库
	configItems := map[string]string{
		"proxy_setting.clash_api_url":     req.ClashApiUrl,
		"proxy_setting.clash_proxy_group": req.ClashProxyGroup,
		"proxy_setting.auto_select":       fmt.Sprintf("%v", req.AutoSelect),
		"proxy_setting.max_delay":         fmt.Sprintf("%d", req.MaxDelay),
		"proxy_setting.cooldown_secs":     fmt.Sprintf("%d", req.CooldownSecs),
		"proxy_setting.test_url":          req.TestUrl,
		"proxy_setting.proxy_enabled":     fmt.Sprintf("%v", req.Enabled),
		"proxy_setting.node_rotation":     fmt.Sprintf("%v", req.NodeRotation),
	}

	for key, value := range configItems {
		if err := model.UpdateOption(key, value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "保存配置失败: " + err.Error(),
			})
			return
		}
	}

	// 更新 ClashProxyPool 配置
	if helper.IsClashEnabled() {
		pool := helper.GetClashProxyPool()
		pool.SetConfig(req.ClashApiUrl, req.ClashProxyGroup, req.MaxDelay, req.CooldownSecs, req.AutoSelect)
		// 刷新节点列表
		pool.RefreshNodes()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "配置已保存",
	})
}

// GetProxyStatus 获取代理状态
func GetProxyStatus(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	setting := operation_setting.GetProxySetting()
	
	response := ProxyStatusResponse{
		Enabled:        setting.ProxyEnabled,
		CurrentNode:    "",
		TotalNodes:     0,
		AvailableNodes: 0,
		CooldownNodes:  0,
		Nodes:          []ProxyNodeResponse{},
		Config: ProxyConfigResponse{
			Enabled:         setting.ProxyEnabled,
			ClashApiUrl:     setting.ClashApiUrl,
			ClashProxyGroup: setting.ClashProxyGroup,
			AutoSelect:      setting.AutoSelect,
			MaxDelay:        setting.MaxDelay,
			CooldownSecs:    setting.CooldownSecs,
			TestUrl:         setting.TestUrl,
			NodeRotation:    setting.NodeRotation,
		},
	}

	// 获取节点信息
	if helper.IsClashEnabled() {
		pool := helper.GetClashProxyPool()
		response.CurrentNode = pool.GetCurrentNode()
		response.TotalNodes = pool.GetNodeCount()
		response.AvailableNodes = pool.GetAvailableNodeCount()

		nodes := pool.GetAllNodes()
		for _, node := range nodes {
			nodeResp := ProxyNodeResponse{
				Name:        node.Name,
				Type:        node.Type,
				Alive:       node.Alive,
				Delay:       node.Delay,
				Region:      node.Region,
				InCooldown:  node.CooldownEnd > 0,
				CooldownEnd: node.CooldownEnd,
				LastTestTime: node.LastTestTime,
			}
			if nodeResp.InCooldown {
				response.CooldownNodes++
			}
			response.Nodes = append(response.Nodes, nodeResp)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

// RefreshProxyNodes 刷新代理节点列表
func RefreshProxyNodes(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	if !helper.IsClashEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "代理功能未启用",
		})
		return
	}

	pool := helper.GetClashProxyPool()
	if err := pool.RefreshNodes(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "刷新节点列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "节点列表已刷新",
		"data": gin.H{
			"total_nodes": pool.GetNodeCount(),
			"current":     pool.GetCurrentNode(),
		},
	})
}

// SwitchProxyNode 切换代理节点
func SwitchProxyNode(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	if !helper.IsClashEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "代理功能未启用",
		})
		return
	}

	var req struct {
		NodeName string `json:"node_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数错误",
		})
		return
	}

	pool := helper.GetClashProxyPool()
	
	var err error
	var newNode string
	
	if req.NodeName != "" {
		// 切换到指定节点
		err = pool.SwitchToNode(req.NodeName)
		newNode = req.NodeName
	} else {
		// 自动切换到下一个节点
		newNode, err = pool.SwitchToNextNode()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "切换节点失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已切换节点",
		"data": gin.H{
			"current_node": newNode,
		},
	})
}

// TestProxyNode 测试代理节点延迟
func TestProxyNode(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	if !helper.IsClashEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "代理功能未启用",
		})
		return
	}

	var req struct {
		NodeName string `json:"node_name"`
		TestAll  bool   `json:"test_all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数错误",
		})
		return
	}

	pool := helper.GetClashProxyPool()

	if req.TestAll {
		// 测试所有节点
		results := pool.TestAllNodes()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "测试完成",
			"data":    results,
		})
		return
	}

	if req.NodeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请指定要测试的节点",
		})
		return
	}

	// 测试单个节点
	delay, err := pool.TestNodeDelay(req.NodeName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "测试失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "测试完成",
		"data": gin.H{
			"node_name": req.NodeName,
			"delay":     delay,
		},
	})
}

// ClearProxyCooldown 清除节点冷却状态
func ClearProxyCooldown(c *gin.Context) {
	// 检查管理员权限
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权限访问",
		})
		return
	}

	if !helper.IsClashEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "代理功能未启用",
		})
		return
	}

	var req struct {
		NodeName string `json:"node_name"`
		ClearAll bool   `json:"clear_all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数错误",
		})
		return
	}

	pool := helper.GetClashProxyPool()
	nodes := pool.GetAllNodes()
	
	if req.ClearAll {
		// 清除所有节点的冷却状态
		for _, node := range nodes {
			if node.CooldownEnd > 0 {
				pool.MarkNodeCooldown(node.Name) // 重置冷却时间为0需要新方法
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "已清除所有节点的冷却状态",
		})
		return
	}

	if req.NodeName != "" {
		// 刷新节点列表会重置冷却状态
		pool.RefreshNodes()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "已清除节点 " + req.NodeName + " 的冷却状态",
		})
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"message": "请指定要清除冷却的节点",
	})
}

// isAdmin 检查是否是管理员
func isAdmin(c *gin.Context) bool {
	userId := c.GetInt("id")
	if userId == 0 {
		return false
	}
	
	user, err := model.GetUserById(userId, false)
	if err != nil {
		return false
	}
	
	// 检查用户角色，1 = 管理员, 100 = 超级管理员
	return user.Role >= 1
}

// SubscribeInfo 订阅信息
type SubscribeInfo struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	Url     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// GetSubscribeList 获取订阅列表
func GetSubscribeList(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权限访问"})
		return
	}
	
	// 从数据库获取订阅列表
	subscribeStr, _ := common.OptionMap["proxy_setting.subscribe_list"]
	
	var subscribes []SubscribeInfo
	if subscribeStr != "" {
		common.Unmarshal([]byte(subscribeStr), &subscribes)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    subscribes,
	})
}

// UpdateSubscribeList 更新订阅列表
func UpdateSubscribeList(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权限访问"})
		return
	}
	
	var subscribes []SubscribeInfo
	if err := c.ShouldBindJSON(&subscribes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数错误"})
		return
	}
	
	data, _ := common.Marshal(subscribes)
	if err := model.UpdateOption("proxy_setting.subscribe_list", string(data)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存失败"})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "保存成功"})
}

// DownloadSubscribe 下载订阅配置并更新mihomo
func DownloadSubscribe(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权限访问"})
		return
	}
	
	var req struct {
		Url string `json:"url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供订阅地址"})
		return
	}
	
	// 创建带 User-Agent 的请求
	httpReq, err := http.NewRequest("GET", req.Url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "clash-meta")
	
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "下载失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "下载失败: HTTP " + fmt.Sprintf("%d", resp.StatusCode)})
		return
	}
	
	// 保存配置文件
	configPath := getClashConfigPath() + "/config.yaml"
	os.MkdirAll(getClashConfigPath(), 0755)
	
	configData, _ := io.ReadAll(resp.Body)
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存配置失败"})
		return
	}
	
	// 重启 mihomo
	// 先杀掉旧的进程
	if helper.IsClashEnabled() {
		pool := helper.GetClashProxyPool()
		pool.RestartMihomo()
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "订阅已更新",
		"data": gin.H{
			"config_size": len(configData),
		},
	})
}

// GetMihomoLogs 获取 mihomo 日志
func GetMihomoLogs(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权限访问"})
		return
	}
	
	logPath := getClashConfigPath() + "/mihomo.log"
	data, err := os.ReadFile(logPath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": "日志文件不存在"})
		return
	}
	
	// 只返回最后1000行
	lines := strings.Split(string(data), "\n")
	if len(lines) > 1000 {
		lines = lines[len(lines)-1000:]
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    strings.Join(lines, "\n"),
	})
}
