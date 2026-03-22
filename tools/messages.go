package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yindex/wx-mcp/internal/mcp"
	"github.com/yindex/wx-mcp/internal/state"
	"github.com/yindex/wx-mcp/internal/wx"
)

func registerMessageTools(srv *mcp.Server, mgr *state.Manager) {

	// ── wx_list_conversations ─────────────────────────────────────────────────
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_list_conversations",
		Description: "列出指定账号的所有会话（按最近消息时间排序），包括未读数和最后一条消息。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id": {Type: "string", Description: "账号 ID"},
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
		if _, ok := mgr.GetAccount(a.AccountID); !ok {
			return mcp.ToolErr("账号不存在: " + a.AccountID), nil
		}
		convs := mgr.ListConversations(a.AccountID)
		if len(convs) == 0 {
			return mcp.ToolOK("暂无会话。等待用户发送消息后自动出现。"), nil
		}
		type convView struct {
			UserID    string `json:"user_id"`
			LastText  string `json:"last_text"`
			Unread    int    `json:"unread"`
			UpdatedAt string `json:"updated_at"`
		}
		views := make([]convView, len(convs))
		for i, c := range convs {
			views[i] = convView{
				UserID:    c.UserID,
				LastText:  c.LastText,
				Unread:    c.Unread,
				UpdatedAt: c.UpdatedAt.Format("2006-01-02 15:04:05"),
			}
		}
		data, _ := json.MarshalIndent(views, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// ── wx_get_messages ───────────────────────────────────────────────────────
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_get_messages",
		Description: "获取与指定用户的聊天记录（最多 100 条，默认 20 条）。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id": {Type: "string", Description: "账号 ID"},
				"user_id":    {Type: "string", Description: "用户 ID（如 xxx@im.wechat）"},
				"limit":      {Type: "number", Description: "返回条数，默认 20，最多 100"},
			},
			Required: []string{"account_id", "user_id"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		var a struct {
			AccountID string `json:"account_id"`
			UserID    string `json:"user_id"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.AccountID == "" || a.UserID == "" {
			return mcp.ToolErr("缺少 account_id 或 user_id"), nil
		}
		if a.Limit <= 0 {
			a.Limit = 20
		}
		if a.Limit > 100 {
			a.Limit = 100
		}
		msgs := mgr.GetMessages(a.AccountID, a.UserID, a.Limit)
		if len(msgs) == 0 {
			return mcp.ToolOK("暂无消息记录"), nil
		}
		type msgView struct {
			ID        string `json:"id"`
			Direction string `json:"direction"`
			Type      string `json:"type"`
			Text      string `json:"text,omitempty"`
			MediaInfo string `json:"media_info,omitempty"`
			Timestamp string `json:"timestamp"`
		}
		views := make([]msgView, len(msgs))
		for i, m := range msgs {
			views[i] = msgView{
				ID:        m.ID,
				Direction: m.Direction,
				Type:      m.Type,
				Text:      m.Text,
				MediaInfo: m.MediaInfo,
				Timestamp: m.Timestamp.Format("2006-01-02 15:04:05"),
			}
		}
		data, _ := json.MarshalIndent(views, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// ── wx_send_text ──────────────────────────────────────────────────────────
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_send_text",
		Description: "向微信用户发送文字消息。需要先有该用户发来过消息（用于获取 context_token）。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id":    {Type: "string", Description: "账号 ID"},
				"to_user_id":    {Type: "string", Description: "目标用户 ID"},
				"text":          {Type: "string", Description: "消息文字内容"},
				"context_token": {Type: "string", Description: "会话关联 Token（可选，自动从历史消息获取）"},
			},
			Required: []string{"account_id", "to_user_id", "text"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		var a struct {
			AccountID    string `json:"account_id"`
			ToUserID     string `json:"to_user_id"`
			Text         string `json:"text"`
			ContextToken string `json:"context_token"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return mcp.ToolErr("参数解析失败: " + err.Error()), nil
		}
		if a.AccountID == "" || a.ToUserID == "" || a.Text == "" {
			return mcp.ToolErr("缺少必填参数 account_id / to_user_id / text"), nil
		}

		acc, ok := mgr.GetAccount(a.AccountID)
		if !ok {
			return mcp.ToolErr("账号不存在: " + a.AccountID), nil
		}
		if acc.IsPaused() {
			return mcp.ToolErr(fmt.Sprintf("账号 Session 冷却中，%.0f 分钟后恢复", time.Until(acc.PausedUntil).Minutes())), nil
		}

		// Auto-resolve context token from conversation history.
		ctxToken := a.ContextToken
		if ctxToken == "" {
			ctxToken = mgr.GetContextToken(a.AccountID, a.ToUserID)
		}
		if ctxToken == "" {
			return mcp.ToolErr("无 context_token：用户尚未发送过消息，无法主动发起对话"), nil
		}

		clientID := genClientID()
		client := wx.NewClient(acc.BaseURL, acc.CDNBaseURL, acc.Token)
		if err := client.SendText(a.ToUserID, a.Text, ctxToken, clientID); err != nil {
			return mcp.ToolErr("发送失败: " + err.Error()), nil
		}

		mgr.AddOutboundMessage(a.AccountID, a.ToUserID, a.Text, ctxToken)
		result := map[string]any{
			"success":   true,
			"client_id": clientID,
			"to":        a.ToUserID,
			"text":      a.Text,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// ── wx_send_image ─────────────────────────────────────────────────────────
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_send_image",
		Description: "向微信用户发送图片。支持本地文件路径或 base64 编码内容。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id":    {Type: "string", Description: "账号 ID"},
				"to_user_id":    {Type: "string", Description: "目标用户 ID"},
				"file_path":     {Type: "string", Description: "图片本地文件路径（与 base64_data 二选一）"},
				"base64_data":   {Type: "string", Description: "图片 base64 内容（与 file_path 二选一）"},
				"caption":       {Type: "string", Description: "图片说明文字（可选）"},
				"context_token": {Type: "string", Description: "会话 Token（可选，自动获取）"},
			},
			Required: []string{"account_id", "to_user_id"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		var a struct {
			AccountID    string `json:"account_id"`
			ToUserID     string `json:"to_user_id"`
			FilePath     string `json:"file_path"`
			Base64Data   string `json:"base64_data"`
			Caption      string `json:"caption"`
			ContextToken string `json:"context_token"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return mcp.ToolErr("参数解析失败: " + err.Error()), nil
		}
		if a.AccountID == "" || a.ToUserID == "" {
			return mcp.ToolErr("缺少 account_id 或 to_user_id"), nil
		}
		if a.FilePath == "" && a.Base64Data == "" {
			return mcp.ToolErr("缺少 file_path 或 base64_data"), nil
		}

		acc, ok := mgr.GetAccount(a.AccountID)
		if !ok {
			return mcp.ToolErr("账号不存在: " + a.AccountID), nil
		}
		if acc.IsPaused() {
			return mcp.ToolErr("账号 Session 冷却中"), nil
		}

		ctxToken := a.ContextToken
		if ctxToken == "" {
			ctxToken = mgr.GetContextToken(a.AccountID, a.ToUserID)
		}
		if ctxToken == "" {
			return mcp.ToolErr("无 context_token：用户尚未发送过消息"), nil
		}

		// Read image bytes.
		var imgBytes []byte
		var err error
		if a.FilePath != "" {
			imgBytes, err = os.ReadFile(a.FilePath)
			if err != nil {
				return mcp.ToolErr("读取文件失败: " + err.Error()), nil
			}
		} else {
			imgBytes, err = base64.StdEncoding.DecodeString(a.Base64Data)
			if err != nil {
				imgBytes, err = base64.RawStdEncoding.DecodeString(a.Base64Data)
				if err != nil {
					return mcp.ToolErr("base64 解码失败: " + err.Error()), nil
				}
			}
		}

		client := wx.NewClient(acc.BaseURL, acc.CDNBaseURL, acc.Token)
		upload, err := client.UploadFile(imgBytes, a.ToUserID, wx.UploadImage)
		if err != nil {
			return mcp.ToolErr("CDN 上传失败: " + err.Error()), nil
		}

		clientID := genClientID()
		msg := buildImageMsg(a.ToUserID, ctxToken, clientID, upload)
		if err := client.SendMediaMessage(msg); err != nil {
			return mcp.ToolErr("发送失败: " + err.Error()), nil
		}

		mgr.AddOutboundMessage(a.AccountID, a.ToUserID, "[图片]", ctxToken)
		result := map[string]any{"success": true, "client_id": clientID}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// ── wx_send_file ──────────────────────────────────────────────────────────
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_send_file",
		Description: "向微信用户发送文件附件（本地文件路径）。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id":    {Type: "string", Description: "账号 ID"},
				"to_user_id":    {Type: "string", Description: "目标用户 ID"},
				"file_path":     {Type: "string", Description: "文件本地路径"},
				"context_token": {Type: "string", Description: "会话 Token（可选，自动获取）"},
			},
			Required: []string{"account_id", "to_user_id", "file_path"},
		},
	}, func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
		var a struct {
			AccountID    string `json:"account_id"`
			ToUserID     string `json:"to_user_id"`
			FilePath     string `json:"file_path"`
			ContextToken string `json:"context_token"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.AccountID == "" || a.ToUserID == "" || a.FilePath == "" {
			return mcp.ToolErr("缺少必填参数"), nil
		}

		acc, ok := mgr.GetAccount(a.AccountID)
		if !ok {
			return mcp.ToolErr("账号不存在: " + a.AccountID), nil
		}

		ctxToken := a.ContextToken
		if ctxToken == "" {
			ctxToken = mgr.GetContextToken(a.AccountID, a.ToUserID)
		}
		if ctxToken == "" {
			return mcp.ToolErr("无 context_token"), nil
		}

		fileBytes, err := os.ReadFile(a.FilePath)
		if err != nil {
			return mcp.ToolErr("读取文件失败: " + err.Error()), nil
		}
		fileName := filepath.Base(a.FilePath)

		client := wx.NewClient(acc.BaseURL, acc.CDNBaseURL, acc.Token)
		upload, err := client.UploadFile(fileBytes, a.ToUserID, wx.UploadFile)
		if err != nil {
			return mcp.ToolErr("CDN 上传失败: " + err.Error()), nil
		}

		clientID := genClientID()
		msg := buildFileMsg(a.ToUserID, ctxToken, clientID, fileName, upload)
		if err := client.SendMediaMessage(msg); err != nil {
			return mcp.ToolErr("发送失败: " + err.Error()), nil
		}

		mgr.AddOutboundMessage(a.AccountID, a.ToUserID, "[文件] "+fileName, ctxToken)
		result := map[string]any{"success": true, "client_id": clientID, "file_name": fileName}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})

	// ── wx_get_unread ─────────────────────────────────────────────────────────
	srv.RegisterTool(mcp.Tool{
		Name:        "wx_get_unread",
		Description: "获取账号所有会话的未读消息汇总。",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"account_id": {Type: "string", Description: "账号 ID"},
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
		convs := mgr.ListConversations(a.AccountID)
		total := 0
		type unreadItem struct {
			UserID string `json:"user_id"`
			Unread int    `json:"unread"`
			Last   string `json:"last_message"`
		}
		var items []unreadItem
		for _, c := range convs {
			if c.Unread > 0 {
				total += c.Unread
				items = append(items, unreadItem{UserID: c.UserID, Unread: c.Unread, Last: c.LastText})
			}
		}
		result := map[string]any{"total_unread": total, "conversations": items}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.ToolOK(string(data)), nil
	})
}

// ─── Message Builders ─────────────────────────────────────────────────────────

func buildImageMsg(toUserID, ctxToken, clientID string, u *wx.UploadResult) *wx.WeixinMessage {
	aesB64 := base64.StdEncoding.EncodeToString(hexDecode(u.AESKeyHex))
	return &wx.WeixinMessage{
		FromUserID:   "",
		ToUserID:     toUserID,
		ClientID:     clientID,
		MessageType:  wx.MsgTypeBot,
		MessageState: wx.MsgStateFinish,
		ContextToken: ctxToken,
		ItemList: []wx.MessageItem{{
			Type: wx.ItemTypeImage,
			ImageItem: &wx.ImageItem{
				Media: &wx.CDNMedia{
					EncryptQueryParam: u.DownloadQueryParam,
					AESKey:            aesB64,
					EncryptType:       1,
				},
				MidSize: u.CiphertextSize,
			},
		}},
	}
}

func buildFileMsg(toUserID, ctxToken, clientID, fileName string, u *wx.UploadResult) *wx.WeixinMessage {
	aesB64 := base64.StdEncoding.EncodeToString(hexDecode(u.AESKeyHex))
	return &wx.WeixinMessage{
		FromUserID:   "",
		ToUserID:     toUserID,
		ClientID:     clientID,
		MessageType:  wx.MsgTypeBot,
		MessageState: wx.MsgStateFinish,
		ContextToken: ctxToken,
		ItemList: []wx.MessageItem{{
			Type: wx.ItemTypeFile,
			FileItem: &wx.FileItem{
				Media: &wx.CDNMedia{
					EncryptQueryParam: u.DownloadQueryParam,
					AESKey:            aesB64,
					EncryptType:       1,
				},
				FileName: fileName,
				Len:      fmt.Sprintf("%d", u.PlaintextSize),
			},
		}},
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func genClientID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("wx-mcp:%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

func hexDecode(h string) []byte {
	b, _ := hex.DecodeString(h)
	return b
}
