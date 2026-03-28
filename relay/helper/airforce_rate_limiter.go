package helper

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// KeyRateLimitInfo 单个Key的限流状态
type KeyRateLimitInfo struct {
	LastUsedTime    int64  // 最后使用时间
	RateLimited     bool   // 是否被限流
	RateLimitedTime int64  // 被限流的时间
	DisabledReason  string // 禁用原因
}

// AirForceRateLimiter AirForce API 限流管理器
// 上游限流: 1请求/秒/IP, 1请求/分钟/key
type AirForceRateLimiter struct {
	KeyStatusMap     map[int]*KeyRateLimitInfo // keyIndex -> 限流状态
	KeyCooldownSecs  int64                     // Key冷却时间(秒)，默认60秒
	MinIntervalMs    int64                     // 同一Key最小间隔(毫秒)，默认1000ms
	mu               sync.RWMutex
	LastRequestTime  int64 // 全局最后请求时间(用于IP限流)
	MinGlobalSec     int64 // 全局最小间隔(秒)，默认1秒
}

var (
	airForceLimiter     *AirForceRateLimiter
	airForceLimiterOnce sync.Once
)

// GetAirForceRateLimiter 获取全局AirForce限流器实例
func GetAirForceRateLimiter() *AirForceRateLimiter {
	airForceLimiterOnce.Do(func() {
		airForceLimiter = &AirForceRateLimiter{
			KeyStatusMap:    make(map[int]*KeyRateLimitInfo),
			KeyCooldownSecs: 60,    // 1请求/分钟/key，冷却60秒
			MinIntervalMs:   1000,  // 1秒最小间隔
			MinGlobalSec:    2,     // 1请求/秒/IP，请求完成后等待2秒
		}
	})
	return airForceLimiter
}

// IsKeyAvailable 检查指定key是否可用
func (r *AirForceRateLimiter) IsKeyAvailable(keyIndex int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.KeyStatusMap[keyIndex]
	if !exists {
		return true
	}

	// 如果key被限流，检查冷却时间
	if info.RateLimited {
		elapsed := time.Now().Unix() - info.RateLimitedTime
		if elapsed >= r.KeyCooldownSecs {
			return true // 冷却时间已过，可用
		}
		return false // 还在冷却中
	}

	return true
}

// MarkKeyUsed 标记key已使用
func (r *AirForceRateLimiter) MarkKeyUsed(keyIndex int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().Unix()
	info, exists := r.KeyStatusMap[keyIndex]
	if !exists {
		info = &KeyRateLimitInfo{}
		r.KeyStatusMap[keyIndex] = info
	}
	info.LastUsedTime = now
}

// MarkKeyRateLimited 标记key被限流
func (r *AirForceRateLimiter) MarkKeyRateLimited(keyIndex int, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().Unix()
	info, exists := r.KeyStatusMap[keyIndex]
	if !exists {
		info = &KeyRateLimitInfo{}
		r.KeyStatusMap[keyIndex] = info
	}

	info.RateLimited = true
	info.RateLimitedTime = now
	info.DisabledReason = reason

	common.SysLog(fmt.Sprintf("AirForce key [%d] rate limited, reason: %s, will be available after %d seconds",
		keyIndex, reason, r.KeyCooldownSecs))
}

// GetNextAvailableKey 获取下一个可用的key索引
// keys: 所有key列表
// startIndex: 开始搜索的索引
// 返回: 可用key的索引，如果没有可用key返回-1
func (r *AirForceRateLimiter) GetNextAvailableKey(keys []string, startIndex int) int {
	if len(keys) == 0 {
		return -1
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// 检查全局IP限流
	// now := time.Now().Unix()
	// if r.LastRequestTime > 0 && (now - r.LastRequestTime) < r.MinGlobalSec {
	// 	// 等待直到可以发送请求
	// 	time.Sleep(time.Duration(r.MinGlobalSec-(now-r.LastRequestTime)) * time.Second)
	// }

	// 从startIndex开始查找可用key
	for i := 0; i < len(keys); i++ {
		idx := (startIndex + i) % len(keys)
		info, exists := r.KeyStatusMap[idx]
		
		// 如果key没有记录，或者没有被限流，或者冷却时间已过
		if !exists {
			return idx
		}
		
		if !info.RateLimited {
			return idx
		}

		// 检查冷却时间
		elapsed := time.Now().Unix() - info.RateLimitedTime
		if elapsed >= r.KeyCooldownSecs {
			// 冷却时间已过，重置状态
			return idx
		}
	}

	return -1 // 所有key都被限流
}

// ClearKeyRateLimit 清除key的限流状态
func (r *AirForceRateLimiter) ClearKeyRateLimit(keyIndex int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if info, exists := r.KeyStatusMap[keyIndex]; exists {
		info.RateLimited = false
		info.RateLimitedTime = 0
		info.DisabledReason = ""
	}
}

// GetKeyStatus 获取key状态信息
func (r *AirForceRateLimiter) GetKeyStatus(keyIndex int) *KeyRateLimitInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.KeyStatusMap[keyIndex]
	if !exists {
		return nil
	}
	return info
}

// GetAvailableKeyCount 获取可用key数量
func (r *AirForceRateLimiter) GetAvailableKeyCount(totalKeys int) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	now := time.Now().Unix()
	
	for i := 0; i < totalKeys; i++ {
		info, exists := r.KeyStatusMap[i]
		if !exists {
			count++
			continue
		}
		
		if !info.RateLimited {
			count++
			continue
		}
		
		// 检查冷却时间
		if now-info.RateLimitedTime >= r.KeyCooldownSecs {
			count++
		}
	}
	
	return count
}

// WaitForGlobalRateLimit 等待全局限流
// 返回一个函数在请求完成后调用（更新时间戳）
func (r *AirForceRateLimiter) WaitForGlobalRateLimit() func() {
	r.mu.Lock()
	now := time.Now().Unix()
	
	// 检查是否需要等待（基于上一个请求的完成时间）
	if r.LastRequestTime > 0 && (now - r.LastRequestTime) < r.MinGlobalSec {
		waitTime := r.MinGlobalSec - (now - r.LastRequestTime)
		common.SysLog(fmt.Sprintf("AirForce global rate limit, waiting %d seconds (last=%d, now=%d, min=%d)", waitTime, r.LastRequestTime, now, r.MinGlobalSec))
		r.mu.Unlock()
		
		time.Sleep(time.Duration(waitTime) * time.Second)
	} else {
		r.mu.Unlock()
	}
	
	// 返回函数，在请求完成后更新时间戳
	return func() {
		r.mu.Lock()
		r.LastRequestTime = time.Now().Unix()
		common.SysLog(fmt.Sprintf("AirForce request completed, updating timestamp to %d", r.LastRequestTime))
		r.mu.Unlock()
	}
}

// IsAirForceChannel 检查是否是AirForce渠道
func IsAirForceChannel(baseUrl string) bool {
	return baseUrl != "" && 
		(containsAirForce(baseUrl, "api.airforce") || 
		 containsAirForce(baseUrl, "airforce"))
}

// ShouldUseFingerprintClient 检查是否应该使用指纹随机化客户端
// 包括 AirForce、DeepInfra 等需要绕过检测的渠道
func ShouldUseFingerprintClient(baseUrl string) bool {
	if baseUrl == "" {
		return false
	}
	// AirForce
	if containsAirForce(baseUrl, "airforce") {
		return true
	}
	// DeepInfra
	if containsAirForce(baseUrl, "deepinfra") {
		return true
	}
	return false
}

func containsAirForce(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// 全局限流状态缓存 (channelId_apiKey -> 限流时间)
var globalRateLimitCache = make(map[string]int64)
var globalRateLimitMutex sync.RWMutex

// MarkKeyRateLimited 全局函数：标记key被限流
// channelId: 渠道ID
// apiKey: API密钥
func MarkKeyRateLimited(channelId int, apiKey string) {
	globalRateLimitMutex.Lock()
	defer globalRateLimitMutex.Unlock()
	
	key := fmt.Sprintf("%d_%s", channelId, apiKey)
	globalRateLimitCache[key] = time.Now().Unix()
	
	keyPreview := apiKey
	if len(apiKey) > 10 {
		keyPreview = apiKey[:10]
	}
	common.SysLog(fmt.Sprintf("Key rate limited: channel_id=%d, key=%s***", channelId, keyPreview))
}

// IsKeyRateLimited 检查key是否被限流
func IsKeyRateLimited(channelId int, apiKey string, cooldownSecs int64) bool {
	globalRateLimitMutex.RLock()
	defer globalRateLimitMutex.RUnlock()
	
	key := fmt.Sprintf("%d_%s", channelId, apiKey)
	if limitTime, exists := globalRateLimitCache[key]; exists {
		return time.Now().Unix()-limitTime < cooldownSecs
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RandomUserAgent 随机User-Agent列表
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.43 Mobile Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 Edg/119.0.0.0",
}

// acceptLanguages 随机Accept-Language列表
var acceptLanguages = []string{
	"en-US,en;q=0.9",
	"zh-CN,zh;q=0.9",
	"ja-JP,ja;q=0.9",
	"ko-KR,ko;q=0.9",
	"de-DE,de;q=0.8,en;q=0.6",
	"fr-FR,fr;q=0.8,en;q=0.6",
	"es-ES,es;q=0.8,en;q=0.6",
	"pt-BR,pt;q=0.8,en;q=0.6",
}

// GetRandomUserAgent 获取随机User-Agent
func GetRandomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// GetRandomAcceptLanguage 获取随机Accept-Language
func GetRandomAcceptLanguage() string {
	return acceptLanguages[rand.Intn(len(acceptLanguages))]
}

// ========== Clash代理池管理 ==========

// ClashProxy Clash代理节点
type ClashProxy struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Alive bool `json:"alive"`
}

// ClashProxiesResponse Clash API响应
type ClashProxiesResponse struct {
	Proxies map[string]ClashProxy `json:"proxies"`
}

// ProxyNodeInfo 代理节点详细信息
type ProxyNodeInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Alive        bool   `json:"alive"`
	Delay        int    `json:"delay"`         // 延迟(ms)，-1表示未测试
	LastTestTime int64  `json:"last_test_time"` // 上次测试时间
	CooldownEnd  int64  `json:"cooldown_end"`   // 冷却结束时间（被限流时设置）
	Region       string `json:"region"`         // 地区标识
}

// ClashProxyPool Clash代理池
type ClashProxyPool struct {
	Nodes         []ProxyNodeInfo // 所有节点信息
	NodeMap       map[string]int  // 节点名称到索引的映射
	ApiUrl        string          // Clash API 地址
	ProxyGroup    string          // 代理组名称
	CurrentIndex  int             // 当前节点索引
	mu            sync.RWMutex
	
	// 智能选择配置
	MaxDelay      int   `json:"max_delay"`       // 最大可接受延迟(ms)，默认1000
	CooldownSecs  int64 `json:"cooldown_secs"`   // 冷却时间(秒)，默认60
	TestUrl       string `json:"test_url"`       // 测试URL
	AutoSelect    bool  `json:"auto_select"`     // 是否自动选择最优节点
	
	// 活跃请求计数（用于暂停自动测速）
	activeRequests int32           // 当前活跃请求数
}

var (
	clashPool     *ClashProxyPool
	clashPoolOnce sync.Once
)

// GetClashProxyPool 获取Clash代理池实例
func GetClashProxyPool() *ClashProxyPool {
	clashPoolOnce.Do(func() {
		apiUrl := os.Getenv("CLASH_API_URL")
		if apiUrl == "" {
			apiUrl = "http://localhost:9090"
		}
		proxyGroup := os.Getenv("CLASH_PROXY_GROUP")
		if proxyGroup == "" {
			proxyGroup = "🔰国外流量"
		}
		
		clashPool = &ClashProxyPool{
			Nodes:        []ProxyNodeInfo{},
			NodeMap:      make(map[string]int),
			ApiUrl:       apiUrl,
			ProxyGroup:   proxyGroup,
			CurrentIndex: 0,
			MaxDelay:     1000,   // 最大延迟1秒
			CooldownSecs: 60,     // 冷却60秒
			TestUrl:      "https://api.airforce/v1/models",
			AutoSelect:   true,
		}
		
		// 初始化节点列表
		clashPool.RefreshNodes()
		
		// 启动自动测速（5分钟间隔）
		go func() {
			time.Sleep(5 * time.Second)
			clashPool.StartAutoSpeedTest(300)
		}()
	})
	return clashPool
}

// RefreshNodes 从Clash获取可用节点列表
func (p *ClashProxyPool) RefreshNodes() error {
	client := &http.Client{Timeout: 5 * time.Second}
	
	// 从代理组获取节点列表
	resp, err := client.Get(p.ApiUrl + "/proxies/" + url.PathEscape(p.ProxyGroup))
	if err != nil {
		common.SysLog(fmt.Sprintf("Clash API获取节点失败: %v", err))
		return err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	
	// 代理组响应结构
	var groupData struct {
		All  []string `json:"all"`
		Now  string   `json:"now"`
		Type string   `json:"type"`
	}
	if err := json.Unmarshal(body, &groupData); err != nil {
		return err
	}
	
	// 获取每个节点的详细信息
	nodesResp, err := client.Get(p.ApiUrl + "/proxies")
	if err != nil {
		common.SysLog(fmt.Sprintf("Clash API获取节点详情失败: %v", err))
		return err
	}
	defer nodesResp.Body.Close()
	
	nodesBody, err := io.ReadAll(nodesResp.Body)
	if err != nil {
		return err
	}
	
	var proxiesResp ClashProxiesResponse
	if err := json.Unmarshal(nodesBody, &proxiesResp); err != nil {
		return err
	}
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// 保留旧的节点状态
	oldNodes := make(map[string]ProxyNodeInfo)
	for _, node := range p.Nodes {
		oldNodes[node.Name] = node
	}
	
	// 构建新节点列表
	var nodes []ProxyNodeInfo
	nodeMap := make(map[string]int)
	currentIdx := 0
	
	for _, name := range groupData.All {
		// 过滤掉非真实节点
		if name == "DIRECT" || name == "REJECT" || 
		   strings.HasPrefix(name, "🚀") || strings.HasPrefix(name, "🔰") ||
		   strings.HasPrefix(name, "🐟") || strings.HasPrefix(name, "Global") {
			continue
		}
		
		nodeInfo := ProxyNodeInfo{
			Name:   name,
			Delay:  -1, // 未测试
			Region: extractRegion(name),
		}
		
		// 从 Clash 获取节点状态
		if proxy, ok := proxiesResp.Proxies[name]; ok {
			nodeInfo.Type = proxy.Type
			nodeInfo.Alive = proxy.Alive
		}
		
		// 保留旧状态（延迟、冷却等）
		if old, ok := oldNodes[name]; ok {
			nodeInfo.Delay = old.Delay
			nodeInfo.LastTestTime = old.LastTestTime
			nodeInfo.CooldownEnd = old.CooldownEnd
		}
		
		// 记录当前节点索引
		if name == groupData.Now {
			currentIdx = len(nodes)
		}
		
		nodeMap[name] = len(nodes)
		nodes = append(nodes, nodeInfo)
	}
	
	p.Nodes = nodes
	p.NodeMap = nodeMap
	p.CurrentIndex = currentIdx
	
	common.SysLog(fmt.Sprintf("Clash代理池已更新，共%d个节点，当前: %s", len(nodes), groupData.Now))
	return nil
}

// extractRegion 从节点名称提取地区
func extractRegion(name string) string {
	// 检测常见地区标识
	regionPatterns := []struct {
		pattern string
		region  string
	}{
		{"🇭🇰", "HK"}, {"香港", "HK"}, {"HK", "HK"},
		{"🇹🇼", "TW"}, {"台湾", "TW"}, {"TW", "TW"},
		{"🇯🇵", "JP"}, {"日本", "JP"}, {"JP", "JP"},
		{"🇰🇷", "KR"}, {"韩国", "KR"}, {"KR", "KR"},
		{"🇸🇬", "SG"}, {"新加坡", "SG"}, {"SG", "SG"},
		{"🇺🇸", "US"}, {"美国", "US"}, {"US", "US"},
		{"🇬🇧", "UK"}, {"英国", "UK"}, {"UK", "UK"},
		{"🇩🇪", "DE"}, {"德国", "DE"}, {"DE", "DE"},
	}
	
	for _, rp := range regionPatterns {
		if strings.Contains(name, rp.pattern) {
			return rp.region
		}
	}
	return "Other"
}

// TestNodeDelay 测试单个节点延迟
func (p *ClashProxyPool) TestNodeDelay(nodeName string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	
	// 先切换到该节点
	p.mu.Lock()
	if idx, ok := p.NodeMap[nodeName]; ok {
		p.CurrentIndex = idx
	}
	p.mu.Unlock()
	
	if err := p.switchToNode(nodeName); err != nil {
		return -1, err
	}
	
	// 测试延迟
	start := time.Now()
	resp, err := client.Get(p.TestUrl)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	
	delay := int(time.Since(start).Milliseconds())
	
	// 更新节点延迟和存活状态
	p.mu.Lock()
	if idx, ok := p.NodeMap[nodeName]; ok {
		p.Nodes[idx].Delay = delay
		p.Nodes[idx].LastTestTime = time.Now().Unix()
		p.Nodes[idx].Alive = true // 测速成功，标记为存活
	}
	p.mu.Unlock()
	
	return delay, nil
}

// TestAllNodes 测试所有节点延迟
func (p *ClashProxyPool) TestAllNodes() map[string]int {
	results := make(map[string]int)
	var resultsMu sync.Mutex
	
	p.mu.RLock()
	nodes := make([]string, len(p.Nodes))
	for i, node := range p.Nodes {
		nodes[i] = node.Name
	}
	p.mu.RUnlock()
	
	if len(nodes) == 0 {
		return results
	}
	
	// 使用 worker pool 并行测速，并发数5
	concurrency := 5
	if len(nodes) < concurrency {
		concurrency = len(nodes)
	}
	
	nodeCh := make(chan string, len(nodes))
	for _, name := range nodes {
		nodeCh <- name
	}
	close(nodeCh)
	
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)
	
	for name := range nodeCh {
		wg.Add(1)
		semaphore <- struct{}{}
		
		go func(nodeName string) {
			defer wg.Done()
			defer func() { <-semaphore }()
			
			delay, err := p.TestNodeDelay(nodeName)
			resultsMu.Lock()
			if err != nil {
				results[nodeName] = -1
			} else {
				results[nodeName] = delay
			}
			resultsMu.Unlock()
		}(name)
	}
	
	wg.Wait()
	return results
}

// MarkNodeCooldown 标记节点进入冷却（被限流时调用）
func (p *ClashProxyPool) MarkNodeCooldown(nodeName string) {
	// 统一调用MarkNodeShortCooldown，使用相同的冷却逻辑
	p.MarkNodeShortCooldown(nodeName)
}

// MarkCurrentNodeCooldown 标记当前节点进入冷却
func (p *ClashProxyPool) MarkCurrentNodeCooldown() {
	p.mu.RLock()
	if p.CurrentIndex < len(p.Nodes) {
		name := p.Nodes[p.CurrentIndex].Name
		p.mu.RUnlock()
		p.MarkNodeCooldown(name)
	} else {
		p.mu.RUnlock()
	}
}

// GetSmartNode 智能选择最优节点
// 优先选择：未冷却 + 延迟最低 + 存活
func (p *ClashProxyPool) GetSmartNode() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.Nodes) == 0 {
		return "", fmt.Errorf("没有可用节点")
	}
	
	now := time.Now().Unix()
	var bestNode *ProxyNodeInfo
	bestIdx := -1
	
	// 遍历所有节点，找最优（严格跳过冷却节点）
	for i := range p.Nodes {
		node := &p.Nodes[i]
		
		// 严格跳过冷却中的节点
		if node.CooldownEnd > now {
			continue
		}
		
		// 跳过不存活的节点（但有有效延迟的节点认为是存活的）
		if !node.Alive && node.Delay <= 0 {
			continue
		}
		
		// 优先选择延迟低的，未测试节点（Delay < 0）优先级低于已测试的
		if node.Delay < 0 {
			if bestNode != nil && bestNode.Delay >= 0 {
				// 已测试的更好
				continue
			}
			// 都未测试，选第一个
		} else {
			// 当前节点已测试
			if bestNode == nil || bestNode.Delay < 0 || node.Delay < bestNode.Delay {
				bestNode = node
				bestIdx = i
			}
		}
		
		// 如果bestNode还是nil，说明前面都是未测试的，先选第一个
		if bestNode == nil {
			bestNode = node
			bestIdx = i
		}
	}
	
	// 如果没找到最优节点（都在冷却中），报错
	if bestIdx < 0 {
		cooldownCount := 0
		for _, node := range p.Nodes {
			if node.CooldownEnd > now {
				cooldownCount++
			}
		}
		return "", fmt.Errorf("所有节点都在冷却中 (%d/%d)", cooldownCount, len(p.Nodes))
	}
	
	p.CurrentIndex = bestIdx
	common.SysLog(fmt.Sprintf("智能选择选中节点 %s (延迟: %dms)", bestNode.Name, bestNode.Delay))
	return bestNode.Name, nil
}

// MarkNodeShortCooldown 标记节点进入短暂冷却（智能选择轮换用）
// 使用10秒短暂冷却，快速轮换节点
func (p *ClashProxyPool) MarkNodeShortCooldown(nodeName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if idx, ok := p.NodeMap[nodeName]; ok {
		// 短暂冷却用配置的CooldownSecs（60秒，配合RPM限制）
		p.Nodes[idx].CooldownEnd = time.Now().Unix() + p.CooldownSecs
		common.SysLog(fmt.Sprintf("节点 %s 进入短暂冷却（智能选择轮换），%d秒后可用", nodeName, p.CooldownSecs))
	}
}

// MarkCurrentNodeShortCooldown 标记当前节点进入短暂冷却（全局函数）
func MarkCurrentNodeShortCooldown() {
	pool := GetClashProxyPool()
	pool.mu.RLock()
	if pool.CurrentIndex < len(pool.Nodes) {
		name := pool.Nodes[pool.CurrentIndex].Name
		pool.mu.RUnlock()
		pool.MarkNodeShortCooldown(name)
	} else {
		pool.mu.RUnlock()
	}
}

// GetCurrentClashNode 获取当前Clash节点名称（全局函数）
func GetCurrentClashNode() string {
	pool := GetClashProxyPool()
	return pool.GetCurrentNode()
}

// EnsureAvailableNode 确保当前节点可用，如果冷却中则切换（全局函数）
func EnsureAvailableNode() (string, error) {
	pool := GetClashProxyPool()
	
	pool.mu.RLock()
	if pool.CurrentIndex >= len(pool.Nodes) {
		pool.mu.RUnlock()
		return pool.SwitchToNextNode()
	}
	
	currentNode := &pool.Nodes[pool.CurrentIndex]
	now := time.Now().Unix()
	if currentNode.CooldownEnd > now {
		// 当前节点在冷却中，需要切换
		pool.mu.RUnlock()
		common.SysLog(fmt.Sprintf("当前节点 %s 在冷却中，切换到其他节点", currentNode.Name))
		return pool.SwitchToNextNode()
	}
	pool.mu.RUnlock()
	return currentNode.Name, nil
}

// RotateNode 切换到下一个可用节点（跳过冷却中的）
func (p *ClashProxyPool) RotateNode() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.Nodes) == 0 {
		return fmt.Errorf("没有可用节点")
	}
	
	now := time.Now().Unix()
	
	// 找下一个不在冷却的节点
	for i := 0; i < len(p.Nodes); i++ {
		idx := (p.CurrentIndex + 1 + i) % len(p.Nodes)
		if p.Nodes[idx].CooldownEnd <= now {
			p.CurrentIndex = idx
			return p.switchToNode(p.Nodes[idx].Name)
		}
	}
	
	return fmt.Errorf("所有节点都在冷却中")
}

// SwitchToNextNode 切换到下一个节点（公开方法）
func (p *ClashProxyPool) SwitchToNextNode() (string, error) {
	if p.AutoSelect {
		name, err := p.GetSmartNode()
		if err != nil {
			return "", err
		}
		if err := p.switchToNode(name); err != nil {
			return "", err
		}
		// 智能选择后标记节点冷却，下次选其他节点（绕开RPM）
		p.MarkNodeShortCooldown(name)
		return name, nil
	}
	
	// 非智能选择，简单轮询
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.Nodes) == 0 {
		return "", fmt.Errorf("没有可用节点")
	}
	
	now := time.Now().Unix()
	for i := 0; i < len(p.Nodes); i++ {
		idx := (p.CurrentIndex + 1 + i) % len(p.Nodes)
		if p.Nodes[idx].CooldownEnd <= now {
			p.CurrentIndex = idx
			node := p.Nodes[idx].Name
			if err := p.switchToNode(node); err != nil {
				return "", err
			}
			return node, nil
		}
	}
	
	return "", fmt.Errorf("所有节点都在冷却中")
}

// SwitchToNode 切换到指定节点
func (p *ClashProxyPool) SwitchToNode(nodeName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if idx, ok := p.NodeMap[nodeName]; ok {
		p.CurrentIndex = idx
		return p.switchToNode(nodeName)
	}
	return fmt.Errorf("节点不存在: %s", nodeName)
}

// switchToNode 切换代理组到指定节点（内部方法，需持有锁）
func (p *ClashProxyPool) switchToNode(node string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	
	reqBody := fmt.Sprintf(`{"name":"%s"}`, node)
	req, err := http.NewRequest("PUT", 
		fmt.Sprintf("%s/proxies/%s", p.ApiUrl, url.PathEscape(p.ProxyGroup)),
		strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := client.Do(req)
	if err != nil {
		common.SysLog(fmt.Sprintf("Clash切换节点失败: %v", err))
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("切换节点失败: %s", string(body))
	}
	
	common.SysLog(fmt.Sprintf("已切换Clash节点: %s", node))
	return nil
}

// GetCurrentNode 获取当前节点
func (p *ClashProxyPool) GetCurrentNode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if len(p.Nodes) == 0 || p.CurrentIndex >= len(p.Nodes) {
		return ""
	}
	return p.Nodes[p.CurrentIndex].Name
}

// GetNodeCount 获取节点数量
func (p *ClashProxyPool) GetNodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.Nodes)
}

// GetAllNodes 获取所有节点信息
func (p *ClashProxyPool) GetAllNodes() []ProxyNodeInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	result := make([]ProxyNodeInfo, len(p.Nodes))
	copy(result, p.Nodes)
	return result
}

// GetAvailableNodeCount 获取可用节点数量（不在冷却中）
func (p *ClashProxyPool) GetAvailableNodeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	now := time.Now().Unix()
	count := 0
	for _, node := range p.Nodes {
		// 不在冷却中的节点都算可用（包括未测试的）
		if node.CooldownEnd <= now {
			count++
		}
	}
	return count
}

// SetConfig 设置代理池配置
func (p *ClashProxyPool) SetConfig(apiUrl, proxyGroup string, maxDelay int, cooldownSecs int64, autoSelect bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if apiUrl != "" {
		p.ApiUrl = apiUrl
	}
	if proxyGroup != "" {
		p.ProxyGroup = proxyGroup
	}
	if maxDelay > 0 {
		p.MaxDelay = maxDelay
	}
	if cooldownSecs > 0 {
		p.CooldownSecs = cooldownSecs
	}
	p.AutoSelect = autoSelect
}

// GetConfig 获取当前配置
func (p *ClashProxyPool) GetConfig() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return map[string]interface{}{
		"api_url":      p.ApiUrl,
		"proxy_group":  p.ProxyGroup,
		"max_delay":    p.MaxDelay,
		"cooldown_secs": p.CooldownSecs,
		"auto_select":  p.AutoSelect,
		"node_count":   len(p.Nodes),
		"available":    p.GetAvailableNodeCount(),
		"current":      p.GetCurrentNode(),
	}
}

// RotateClashNode 切换Clash节点（全局便捷函数）
func RotateClashNode() (string, error) {
	pool := GetClashProxyPool()
	return pool.SwitchToNextNode()
}

// IncrementActiveRequests 增加活跃请求计数（开始处理请求时调用）
func IncrementActiveRequests() {
	pool := GetClashProxyPool()
	count := atomic.AddInt32(&pool.activeRequests, 1)
	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("活跃请求+1，当前: %d", count))
	}
}

// DecrementActiveRequests 减少活跃请求计数（请求完成时调用）
func DecrementActiveRequests() {
	pool := GetClashProxyPool()
	count := atomic.AddInt32(&pool.activeRequests, -1)
	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("活跃请求-1，当前: %d", count))
	}
}

// GetActiveRequests 获取当前活跃请求数
func GetActiveRequests() int32 {
	pool := GetClashProxyPool()
	return atomic.LoadInt32(&pool.activeRequests)
}

// MarkCurrentNodeCooldown 全局函数：标记当前节点进入冷却
func MarkCurrentNodeCooldown() {
	pool := GetClashProxyPool()
	pool.MarkCurrentNodeCooldown()
}

// IsClashEnabled 检查是否启用了Clash
func IsClashEnabled() bool {
	return os.Getenv("CLASH_API_URL") != ""
}

// ClearNodeCooldown 清除单个节点的冷却状态
func (p *ClashProxyPool) ClearNodeCooldown(nodeName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if idx, ok := p.NodeMap[nodeName]; ok {
		p.Nodes[idx].CooldownEnd = 0
	}
}

// ClearAllCooldown 清除所有节点的冷却状态
func (p *ClashProxyPool) ClearAllCooldown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	for i := range p.Nodes {
		p.Nodes[i].CooldownEnd = 0
	}
	common.SysLog(fmt.Sprintf("已清除所有节点的冷却状态，共%d个节点", len(p.Nodes)))
}

// StartAutoSpeedTest 启动自动测速
// intervalSeconds: 测速间隔（秒）
func (p *ClashProxyPool) StartAutoSpeedTest(intervalSeconds int) {
	if intervalSeconds <= 0 {
		intervalSeconds = 300 // 默认5分钟
	}
	
	go func() {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()
		
		// 启动时先测一次
		p.runSpeedTest()
		
		for range ticker.C {
			p.runSpeedTest()
		}
	}()
	
	common.SysLog(fmt.Sprintf("自动测速已启动，间隔 %d 秒", intervalSeconds))
}

// runSpeedTest 执行测速（并行版本）
func (p *ClashProxyPool) runSpeedTest() {
	// 检查是否有活跃请求，有则跳过测速
	if atomic.LoadInt32(&p.activeRequests) > 0 {
		common.SysLog("有活跃请求，跳过本次自动测速")
		return
	}
	
	common.SysLog("开始自动测速...")

	p.mu.RLock()
	nodes := make([]string, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		// 未测试的节点也参与测速（Cooldown中的节点除外）
		if node.CooldownEnd <= time.Now().Unix() {
			nodes = append(nodes, node.Name)
		}
	}
	originalNode := ""
	if p.CurrentIndex < len(p.Nodes) {
		originalNode = p.Nodes[p.CurrentIndex].Name
	}
	p.mu.RUnlock()

	if len(nodes) == 0 {
		common.SysLog("没有需要测速的节点")
		return
	}

	// 使用worker pool并行测速，并发数5
	concurrency := 5
	if len(nodes) < concurrency {
		concurrency = len(nodes)
	}

	testCount := int32(0)
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)

	for _, name := range nodes {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(nodeName string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// 使用TestNodeDelay测试单个节点
			delay, err := p.TestNodeDelay(nodeName)
			if err == nil && delay > 0 {
				atomic.AddInt32(&testCount, 1)
			}
		}(name)
	}

	wg.Wait()

	// 恢复原始节点
	if originalNode != "" {
		p.switchToNode(originalNode)
	}

	// 测速完成后，如果启用了智能选择，自动切换到最优节点
	if p.AutoSelect && testCount > 0 {
		if name, err := p.GetSmartNode(); err == nil {
			p.switchToNode(name)
			common.SysLog(fmt.Sprintf("自动测速完成，已切换到最优节点: %s", name))
		}
	} else {
		common.SysLog(fmt.Sprintf("自动测速完成，共测试 %d 个节点", testCount))
	}
}

// autoSpeedTestInterval 自动测速间隔
var autoSpeedTestInterval int = 3600 // 默认1小时

// SetAutoSpeedTestInterval 设置自动测速间隔
func SetAutoSpeedTestInterval(seconds int) {
	autoSpeedTestInterval = seconds
}

// GetAutoSpeedTestInterval 获取自动测速间隔
func GetAutoSpeedTestInterval() int {
	return autoSpeedTestInterval
}


// RestartMihomo 通过 API 重新加载 mihomo 配置
func (p *ClashProxyPool) RestartMihomo() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	client := &http.Client{Timeout: 5 * time.Second}
	
	// 使用 mihomo 的 reload API
	reloadUrl := p.ApiUrl + "/configs?force=true"
	req, _ := http.NewRequest("PUT", reloadUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		common.SysLog(fmt.Sprintf("Reload mihomo config failed: %v", err))
		return err
	}
	defer resp.Body.Close()
	
	common.SysLog("Mihomo config reloaded")
	go p.RefreshNodes()
	return nil
}

func (p *ClashProxyPool) ClearAllNodeCooldowns() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	for i := range p.Nodes {
		p.Nodes[i].CooldownEnd = 0
	}
}
