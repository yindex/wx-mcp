package tools

import (
	"context"
	"encoding/json"

	"github.com/yindex/wx-mcp/internal/mcp"
	"github.com/yindex/wx-mcp/internal/state"
)

func registerAccountTools(srv *mcp.Server, mgr *state.Manager) {
	// wx_list_accounts
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_list_accounts",
		Description: "列出所有已登录的微信 Bot 账号及其状态。",
		InputSchema: mcp.JSONSchema{Type: "object"},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		accounts := mgr.ListAccounts()
		if len(accounts) == 0 {
			return mcp.ToolOK("暂无已登录账号。请先调用 wx_login_start 扫码登录。"), nil
		}
		type accountView struct {
			ID       string `json:"id"`
			UserID   string `json:"user_id"`
			Status   string `json:"status"`
			BaseURL  string `json:"base_url"`
			LoggedAt string `json:"logged_at"`
		}
		views := make([]accountView, len(accounts))
		for i, a := range accounts {
			views[i] = accountView{
				ID:       a.ID,
				UserID:   a.UserID,
				Status:   a.Status,
				BaseURL:  a.BaseURL,
				LoggedAt: a.LoggedAt.Format("2006-01-02 15:04:05"),
			}
		}
		data, _ := json.MarshalIndent(views, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// wx_account_status
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_account_status",
		Description: "查询指定账号的详细状态，包括是否暂停（Session 过期冷却中）。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id": {Type: "string", Description: "账号 ID（来自 wx_list_accounts 或 wx_login_poll）"},
			},
			Required: []string{"account_id"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		var a struct {
			AccountID string `json:"account_id"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.AccountID == "" {
			return mcp.ToolErr("缺少 account_id"), nil
		}
		acc, ok := mgr.GetAccount(a.AccountID)
		if !ok {
			return mcp.ToolErr("账号不存在: " + a.AccountID), nil
		}
		result := map[string]any{
			"id":           acc.ID,
			"user_id":      acc.UserID,
			"status":       acc.Status,
			"base_url":     acc.BaseURL,
			"cdn_base_url": acc.CDNBaseURL,
			"logged_at":    acc.LoggedAt.Format("2006-01-02 15:04:05"),
			"is_paused":    acc.IsPaused(),
		}
		if acc.IsPaused() {
			result["paused_until"] = acc.PausedUntil.Format("2006-01-02 15:04:05")
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// wx_remove_account
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_remove_account",
		Description: "移除一个已登录账号并停止其消息轮询。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id": {Type: "string", Description: "要移除的账号 ID"},
			},
			Required: []string{"account_id"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		var a struct {
			AccountID string `json:"account_id"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.AccountID == "" {
			return mcp.ToolErr("缺少 account_id"), nil
		}
		_, ok := mgr.GetAccount(a.AccountID)
		if !ok {
			return mcp.ToolErr("账号不存在: " + a.AccountID), nil
		}
		mgr.RemoveAccount(a.AccountID)
		return mcp.ToolOK("账号 " + a.AccountID + " 已移除"), nil
	})
}
