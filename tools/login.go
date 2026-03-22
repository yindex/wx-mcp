package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yindex/wx-mcp/internal/mcp"
	"github.com/yindex/wx-mcp/internal/state"
	"github.com/yindex/wx-mcp/internal/wx"
)

// ─── Active Login Sessions ────────────────────────────────────────────────────

type loginSession struct {
	SessionKey       string
	QRCode           string // raw token for status polling
	QRCodeScanURL    string // WeChat-scannable URL (encode as QR image)
	BaseURL          string
	StartedAt        time.Time
	RefreshCount     int
}

var (
	loginMu       sync.Mutex
	loginSessions = map[string]*loginSession{}
)

func storeSession(sess *loginSession) {
	loginMu.Lock()
	loginSessions[sess.SessionKey] = sess
	loginMu.Unlock()
}

func getSession(key string) (*loginSession, bool) {
	loginMu.Lock()
	defer loginMu.Unlock()
	s, ok := loginSessions[key]
	return s, ok
}

func deleteSession(key string) {
	loginMu.Lock()
	delete(loginSessions, key)
	loginMu.Unlock()
}

// ─── Tool Registration ────────────────────────────────────────────────────────

func registerLoginTools(srv *mcp.Server, mgr *state.Manager) {
	// wx_login_start — fetch QR code
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_login_start",
		Description: "开始微信 Bot 扫码登录流程。返回二维码图片 URL，用微信扫描后调用 wx_login_poll 完成登录。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"base_url": {
					Type:        "string",
					Description: "WeChat iLink API 地址，默认 https://ilinkai.weixin.qq.com",
				},
				"bot_type": {
					Type:        "string",
					Description: "Bot 类型，默认 3",
					Default:     "3",
				},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		return handleLoginStart(args)
	})

	// wx_login_poll — poll until confirmed / expired
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_login_poll",
		Description: "轮询微信扫码登录状态。返回 status: wait|scaned|confirmed|expired。confirmed 时登录成功并返回 account_id。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"session_key": {
					Type:        "string",
					Description: "wx_login_start 返回的 session_key",
				},
			},
			Required: []string{"session_key"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		return handleLoginPoll(args, mgr)
	})
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

type loginStartArgs struct {
	BaseURL string `json:"base_url"`
	BotType string `json:"bot_type"`
}

func handleLoginStart(raw json.RawMessage) (*mcp.CallToolResult, error) {
	var args loginStartArgs
	_ = json.Unmarshal(raw, &args)
	if args.BaseURL == "" {
		args.BaseURL = wx.DefaultBaseURL
	}
	if args.BotType == "" {
		args.BotType = wx.DefaultBotType
	}

	qr, err := wx.GetQRCode(args.BaseURL, args.BotType)
	if err != nil {
		return mcp.ToolErr("获取二维码失败: " + err.Error()), nil
	}

	sess := &loginSession{
		SessionKey:    qr.QRCode, // use raw token as session key (unique)
		QRCode:        qr.QRCode,
		QRCodeScanURL: qr.QRCodeImgContent,
		BaseURL:       args.BaseURL,
		StartedAt:     time.Now(),
	}
	storeSession(sess)

	result := map[string]any{
		"session_key":      sess.SessionKey,
		"qrcode_scan_url":  qr.QRCodeImgContent,
		"hint":             "请用微信扫描 qrcode_scan_url 对应的二维码，然后调用 wx_login_poll 轮询结果",
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return mcp.ToolOK(string(data)), nil
}

type loginPollArgs struct {
	SessionKey string `json:"session_key"`
}

const maxQRRefresh = 3

func handleLoginPoll(raw json.RawMessage, mgr *state.Manager) (*mcp.CallToolResult, error) {
	var args loginPollArgs
	if err := json.Unmarshal(raw, &args); err != nil || args.SessionKey == "" {
		return mcp.ToolErr("缺少 session_key"), nil
	}

	sess, ok := getSession(args.SessionKey)
	if !ok {
		return mcp.ToolErr("session_key 无效或已过期，请重新调用 wx_login_start"), nil
	}

	// Session TTL: 5 minutes.
	if time.Since(sess.StartedAt) > 5*time.Minute {
		deleteSession(args.SessionKey)
		return mcp.ToolErr("登录会话已超时，请重新调用 wx_login_start"), nil
	}

	status, err := wx.PollQRStatus(sess.BaseURL, sess.QRCode)
	if err != nil {
		return mcp.ToolErr("轮询状态失败: " + err.Error()), nil
	}

	switch status.Status {
	case "wait":
		result := map[string]any{"status": "wait", "message": "等待用户扫码…"}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil

	case "scaned":
		result := map[string]any{"status": "scaned", "message": "已扫码，等待用户在微信中确认…"}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil

	case "expired":
		sess.RefreshCount++
		if sess.RefreshCount > maxQRRefresh {
			deleteSession(args.SessionKey)
			return mcp.ToolErr("二维码多次过期，请重新调用 wx_login_start"), nil
		}
		// Refresh QR code.
		newQR, err := wx.GetQRCode(sess.BaseURL, wx.DefaultBotType)
		if err != nil {
			deleteSession(args.SessionKey)
			return mcp.ToolErr("刷新二维码失败: " + err.Error()), nil
		}
		// Update session with new QR.
		deleteSession(args.SessionKey)
		sess.SessionKey = newQR.QRCode
		sess.QRCode = newQR.QRCode
		sess.QRCodeScanURL = newQR.QRCodeImgContent
		sess.StartedAt = time.Now()
		storeSession(sess)

		result := map[string]any{
			"status":          "expired",
			"message":         fmt.Sprintf("二维码已过期，已刷新 (%d/%d)，请重新扫码", sess.RefreshCount, maxQRRefresh),
			"new_session_key": sess.SessionKey,
			"qrcode_scan_url": sess.QRCodeScanURL,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil

	case "confirmed":
		if status.ILinkBotID == "" {
			deleteSession(args.SessionKey)
			return mcp.ToolErr("登录确认但服务器未返回 ilink_bot_id"), nil
		}
		deleteSession(args.SessionKey)

		acc := &state.Account{
			ID:         status.ILinkBotID,
			Token:      status.BotToken,
			BaseURL:    status.BaseURL,
			CDNBaseURL: wx.DefaultCDNBaseURL,
			UserID:     status.ILinkUserID,
			LoggedAt:   time.Now(),
			Status:     "active",
		}
		if acc.BaseURL == "" {
			acc.BaseURL = sess.BaseURL
		}
		mgr.AddAccount(acc)

		result := map[string]any{
			"status":     "confirmed",
			"account_id": acc.ID,
			"user_id":    acc.UserID,
			"base_url":   acc.BaseURL,
			"message":    "✅ 微信登录成功！现在可以使用 wx_send_text 等工具发送消息。",
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil

	default:
		result := map[string]any{"status": status.Status}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil
	}
}

