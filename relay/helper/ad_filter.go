package helper

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// AdFilterConfig 广告过滤配置
type AdFilterConfig struct {
	Enabled       bool
	AdPatterns    []string
	compiledRegex []*regexp.Regexp
	mu            sync.RWMutex
}

// 全局广告过滤器实例
var globalAdFilter *AdFilterConfig
var adFilterOnce sync.Once

// GetAdFilter 获取全局广告过滤器实例
func GetAdFilter() *AdFilterConfig {
	adFilterOnce.Do(func() {
		// 从环境变量读取配置
		enabled := common.GetEnvOrDefaultBool("AD_FILTER_ENABLED", true)
		
		globalAdFilter = &AdFilterConfig{
			Enabled: enabled,
			AdPatterns: []string{
				// AirForce 特定广告模式 - 更精确的匹配
				`Need proxies cheaper than the market\?`,
				`https?://op\.wtf`,
				`Upgrade your plan to remove this message`,
				`https?://api\.airforce`,
				`discord\.gg/airforce`,  // 只匹配 airforce 的 discord
				// 限流提示
				`Ratelimit Exceeded!`,
				`Try again in \d+\.?\d* seconds?\. Or upgrade at`,
				// 超短模式用于流式检测 - 只在换行后匹配
				`\n\nNeed proxies cheaper`,
				`\n\nNeed pro`,
			},
		}
		globalAdFilter.compilePatterns()
		
		if enabled {
			common.SysLog("ad filter enabled with " + strconv.Itoa(len(globalAdFilter.AdPatterns)) + " patterns")
		} else {
			common.SysLog("ad filter disabled")
		}
	})
	return globalAdFilter
}

// compilePatterns 编译正则表达式
func (f *AdFilterConfig) compilePatterns() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.compiledRegex = make([]*regexp.Regexp, 0, len(f.AdPatterns))
	for _, pattern := range f.AdPatterns {
		re, err := regexp.Compile(pattern)
		if err == nil {
			f.compiledRegex = append(f.compiledRegex, re)
		}
	}
}

// IsEnabled 检查过滤器是否启用
func (f *AdFilterConfig) IsEnabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.Enabled
}

// SetEnabled 设置过滤器启用状态
func (f *AdFilterConfig) SetEnabled(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Enabled = enabled
}

// ContainsAd 检查文本是否包含广告
func (f *AdFilterConfig) ContainsAd(text string) bool {
	if !f.IsEnabled() || text == "" {
		return false
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, re := range f.compiledRegex {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// FindAdPosition 查找广告开始的位置，返回-1表示没有广告
func (f *AdFilterConfig) FindAdPosition(text string) int {
	if !f.IsEnabled() || text == "" {
		return -1
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	minPos := -1
	for _, re := range f.compiledRegex {
		loc := re.FindStringIndex(text)
		if loc != nil {
			if minPos < 0 || loc[0] < minPos {
				minPos = loc[0]
			}
		}
	}
	return minPos
}

// FilterText 过滤文本中的广告
func (f *AdFilterConfig) FilterText(text string) string {
	if !f.IsEnabled() || text == "" {
		return text
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	result := text
	for _, re := range f.compiledRegex {
		result = re.ReplaceAllString(result, "")
	}

	// 清理多余的空白
	result = strings.TrimSpace(result)
	return result
}

// FilterStreamData 过滤流式数据中的广告
// 返回过滤后的数据、是否检测到广告、是否检测到key限流
func (f *AdFilterConfig) FilterStreamData(data string) (string, bool, bool) {
	if !f.IsEnabled() || data == "" {
		return data, false, false
	}

	// 尝试解析为流式响应
	var streamResp dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
		// 解析失败，直接检查原始文本
		if f.ContainsAd(data) {
			// 过滤广告但保留其他内容
			filtered := f.FilterText(data)
			return filtered, true, false
		}
		// 检测key限流
		if IsKeyRateLimitMessage(data) {
			return data, false, true
		}
		return data, false, false
	}

	// 检查是否包含tool_calls，如果有则不过滤（避免误杀）
	for i := range streamResp.Choices {
		if len(streamResp.Choices[i].Delta.ToolCalls) > 0 {
			// 包含tool_calls的响应不过滤
			return data, false, false
		}
	}

	// 首先检查是否有key限流（如果有，不过滤广告，保留原样）
	keyRateLimited := false
	for i := range streamResp.Choices {
		content := streamResp.Choices[i].Delta.GetContentString()
		reasoningContent := streamResp.Choices[i].Delta.GetReasoningContent()
		
		if content != "" && IsKeyRateLimitMessage(content) {
			keyRateLimited = true
		}
		if reasoningContent != "" && IsKeyRateLimitMessage(reasoningContent) {
			keyRateLimited = true
		}
	}
	
	// 如果检测到key限流，直接返回原数据（不过滤广告）
	if keyRateLimited {
		return data, false, true
	}
	
	// 检查并过滤每个 choice 的内容
	hasAd := false
	for i := range streamResp.Choices {
		content := streamResp.Choices[i].Delta.GetContentString()
		reasoningContent := streamResp.Choices[i].Delta.GetReasoningContent()
		
		if content != "" {
			if f.ContainsAd(content) {
				hasAd = true
				filteredContent := f.FilterText(content)
				streamResp.Choices[i].Delta.SetContentString(filteredContent)
			}
		}

		// 检查 reasoning content
		if reasoningContent != "" {
			if f.ContainsAd(reasoningContent) {
				hasAd = true
				filteredReasoning := f.FilterText(reasoningContent)
				streamResp.Choices[i].Delta.SetReasoningContent(filteredReasoning)
			}
		}
	}

	if hasAd || keyRateLimited {
		// 重新序列化
		filteredData, err := common.Marshal(streamResp)
		if err != nil {
			return data, false, keyRateLimited
		}
		return string(filteredData), hasAd, keyRateLimited
	}

	return data, false, false
}

// ShouldFilterAdContent 检查是否应该过滤广告内容
// 可以通过环境变量控制
func ShouldFilterAdContent() bool {
	return GetAdFilter().IsEnabled()
}

// FilterCompleteResponse 过滤完整响应中的广告
// 返回: 是否有广告, 是否检测到key限流
func FilterCompleteResponse(response *dto.OpenAITextResponse) (bool, bool) {
	if !GetAdFilter().IsEnabled() || response == nil {
		return false, false
	}

	hasAd := false
	keyRateLimited := false
	for i := range response.Choices {
		content := response.Choices[i].Message.StringContent()
		if content != "" {
			// 检测key限流提示
			if strings.Contains(content, "Ratelimit Exceeded!") {
				keyRateLimited = true
			}
			if GetAdFilter().ContainsAd(content) {
				hasAd = true
				filteredContent := GetAdFilter().FilterText(content)
				response.Choices[i].Message.SetStringContent(filteredContent)
			}
		}

		// 检查 reasoning content
		reasoningContent := response.Choices[i].Message.ReasoningContent
		if reasoningContent != "" {
			if strings.Contains(reasoningContent, "Ratelimit Exceeded!") {
				keyRateLimited = true
			}
			if GetAdFilter().ContainsAd(reasoningContent) {
				hasAd = true
				filteredReasoning := GetAdFilter().FilterText(reasoningContent)
				response.Choices[i].Message.ReasoningContent = filteredReasoning
			}
		}
	}

	return hasAd, keyRateLimited
}

// IsKeyRateLimitMessage 检查内容是否为key限流消息
// 使用 JSON 解析只检查 content 和 reasoning_content 字段，避免误判
func IsKeyRateLimitMessage(data string) bool {
	// 快速预检查：如果整个字符串都不包含关键词，直接返回 false
	if !strings.Contains(data, "Ratelimit Exceeded!") {
		return false
	}

	// 解析 JSON，只检查 content 和 reasoning_content 字段
	var streamResp struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResp); err != nil {
		// 解析失败时，回退到字符串检查
		return strings.Contains(data, "Ratelimit Exceeded!")
	}

	// 检查每个 choice 的 content 和 reasoning_content
	for _, choice := range streamResp.Choices {
		// 检查 delta (流式响应)
		if strings.Contains(choice.Delta.Content, "Ratelimit Exceeded!") ||
			strings.Contains(choice.Delta.ReasoningContent, "Ratelimit Exceeded!") {
			return true
		}
		// 检查 message (非流式响应)
		if strings.Contains(choice.Message.Content, "Ratelimit Exceeded!") ||
			strings.Contains(choice.Message.ReasoningContent, "Ratelimit Exceeded!") {
			return true
		}
	}

	return false
}
// IsEmptyStreamResponse 检查流式响应是否为空（没有实际 content 内容）
func IsEmptyStreamResponse(data string) bool {
	var streamResp struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}

	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResp); err != nil {
		return false
	}

	for _, choice := range streamResp.Choices {
		if choice.Delta.Content != "" || choice.Delta.ReasoningContent != "" {
			return false
		}
	}

	return true
}
