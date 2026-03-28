package service

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

// IsRateLimitError 检查是否为限流错误(429)
// 限流错误不应该禁用channel，应该触发重试切换到下一个key
func IsRateLimitError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.StatusCode == http.StatusTooManyRequests {
		return true
	}
	// 检查错误消息中是否包含限流相关关键词
	lowerMessage := strings.ToLower(err.Error())
	rateLimitKeywords := []string{"rate limit", "too many requests", "limit exceeded", "quota exceeded", "请稍后再试"}
	for _, keyword := range rateLimitKeywords {
		if strings.Contains(lowerMessage, keyword) {
			return true
		}
	}
	return false
}

// HandleRateLimitError 处理限流错误：标记key为临时限流状态
func HandleRateLimitError(channelId int, apiKey string) {
	// 使用AirForce限流处理器记录限流
	helper.MarkKeyRateLimited(channelId, apiKey)
	
	// 获取完整的channel对象（包括缓存中的）
	channel, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("Key被限流，获取channel失败: channel_id=%d, err=%v", channelId, err))
		return
	}
	if channel == nil {
		common.SysLog(fmt.Sprintf("Key被限流，channel为nil: channel_id=%d", channelId))
		return
	}
	
	common.SysLog(fmt.Sprintf("Key被限流，检查channel: channel_id=%d, is_multi_key=%v, key_count=%d", 
		channelId, channel.ChannelInfo.IsMultiKey, len(strings.Split(channel.Key, "\n"))))
	
	if channel.ChannelInfo.IsMultiKey {
		keys := strings.Split(channel.Key, "\n")
		found := false
		for i, key := range keys {
			if strings.TrimSpace(key) == strings.TrimSpace(apiKey) {
				// 标记该key为禁用状态
				// 注意：channel是指向缓存的指针，修改会直接更新缓存
				if channel.ChannelInfo.MultiKeyStatusList == nil {
					channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
				}
				channel.ChannelInfo.MultiKeyStatusList[i] = common.ChannelStatusAutoDisabled
				if channel.ChannelInfo.MultiKeyDisabledReason == nil {
					channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
				}
				channel.ChannelInfo.MultiKeyDisabledReason[i] = "rate limited"
				if channel.ChannelInfo.MultiKeyDisabledTime == nil {
					channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
				}
				channel.ChannelInfo.MultiKeyDisabledTime[i] = time.Now().Unix()
				
				// 保存到数据库（缓存已通过指针修改更新）
				saveErr := channel.SaveChannelInfo()
				if saveErr != nil {
					common.SysLog(fmt.Sprintf("Key被限流，保存失败: channel_id=%d, err=%v", channelId, saveErr))
				}
				common.SysLog(fmt.Sprintf("Key被限流，已标记为临时禁用: channel_id=%d, key_index=%d", channelId, i))
				found = true
				break
			}
		}
		if !found {
			common.SysLog(fmt.Sprintf("Key被限流，未找到匹配的key: channel_id=%d, api_key_len=%d", channelId, len(apiKey)))
		}
	} else {
		common.SysLog(fmt.Sprintf("Key被限流，非多key模式: channel_id=%d", channelId))
	}
}

func ShouldDisableChannel(channelType int, err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	
	// 限流错误(429)不应该禁用channel，而是触发key切换
	if IsRateLimitError(err) {
		return false
	}
	
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}
	//if err.StatusCode == http.StatusUnauthorized {
	//	return true
	//}
	if err.StatusCode == http.StatusForbidden {
		switch channelType {
		case constant.ChannelTypeGemini:
			return true
		}
	}
	oaiErr := err.ToOpenAIError()
	switch oaiErr.Code {
	case "invalid_api_key":
		return true
	case "account_deactivated":
		return true
	case "billing_not_active":
		return true
	case "pre_consume_token_quota_failed":
		return true
	case "Arrearage":
		return true
	}
	switch oaiErr.Type {
	case "insufficient_quota":
		return true
	case "insufficient_user_quota":
		return true
	// https://docs.anthropic.com/claude/reference/errors
	case "authentication_error":
		return true
	case "permission_error":
		return true
	case "forbidden":
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

func ShouldEnableChannel(newAPIError *types.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}
