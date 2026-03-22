// Package state manages in-memory accounts, conversations and messages
// for the wx-mcp MCP server.
package state

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yindex/wx-mcp/internal/wx"
)

// ─── Data Models ─────────────────────────────────────────────────────────────

// Account represents a logged-in WeChat bot account.
type Account struct {
	ID          string    `json:"id"`
	Token       string    `json:"token"`
	BaseURL     string    `json:"base_url"`
	CDNBaseURL  string    `json:"cdn_base_url"`
	UserID      string    `json:"user_id"`
	LoggedAt    time.Time `json:"logged_at"`
	Status      string    `json:"status"` // "active" | "paused" | "error"
	PausedUntil time.Time `json:"paused_until,omitempty"`
}

// IsPaused returns true if the account session is in cooldown.
func (a *Account) IsPaused() bool {
	return a.Status == "paused" && time.Now().Before(a.PausedUntil)
}

// Conversation groups messages between the bot and one user.
type Conversation struct {
	AccountID    string    `json:"account_id"`
	UserID       string    `json:"user_id"`
	ContextToken string    `json:"context_token"` // latest context token for replies
	UpdatedAt    time.Time `json:"updated_at"`
	Unread       int       `json:"unread"`
	LastText     string    `json:"last_text"`
}

// Message is a stored chat message.
type Message struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"account_id"`
	UserID       string    `json:"user_id"`
	Direction    string    `json:"direction"` // "inbound" | "outbound"
	Type         string    `json:"type"`      // "text" | "image" | "voice" | "file" | "video"
	Text         string    `json:"text,omitempty"`
	MediaInfo    string    `json:"media_info,omitempty"` // file name / description
	ContextToken string    `json:"context_token,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

const maxMessagesPerConv = 500

// ─── Manager ─────────────────────────────────────────────────────────────────

// Manager is a thread-safe in-memory store for all runtime state.
type Manager struct {
	mu            sync.RWMutex
	accounts      map[string]*Account      // accountID → Account
	conversations map[string]*Conversation // accountID:userID → Conversation
	messages      map[string][]*Message    // accountID:userID → []Message

	// pollBuf holds the get_updates_buf cursor for each account.
	pollBuf map[string]string

	// subscribers are notified on new inbound messages.
	subMu       sync.Mutex
	subscribers []func(*Message)
}

// NewManager creates a new empty Manager.
func NewManager() *Manager {
	return &Manager{
		accounts:      make(map[string]*Account),
		conversations: make(map[string]*Conversation),
		messages:      make(map[string][]*Message),
		pollBuf:       make(map[string]string),
	}
}

// Subscribe registers a callback called on every new inbound message.
func (m *Manager) Subscribe(fn func(*Message)) {
	m.subMu.Lock()
	m.subscribers = append(m.subscribers, fn)
	m.subMu.Unlock()
}

func (m *Manager) notify(msg *Message) {
	m.subMu.Lock()
	subs := make([]func(*Message), len(m.subscribers))
	copy(subs, m.subscribers)
	m.subMu.Unlock()
	for _, fn := range subs {
		fn(msg)
	}
}

// ─── Accounts ─────────────────────────────────────────────────────────────────

// AddAccount stores an account and starts its background polling goroutine.
func (m *Manager) AddAccount(acc *Account) {
	m.mu.Lock()
	m.accounts[acc.ID] = acc
	m.mu.Unlock()
	go m.pollAccount(acc.ID)
}

// GetAccount returns the account with the given ID.
func (m *Manager) GetAccount(id string) (*Account, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.accounts[id]
	return a, ok
}

// ListAccounts returns all known accounts.
func (m *Manager) ListAccounts() []*Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Account, 0, len(m.accounts))
	for _, a := range m.accounts {
		out = append(out, a)
	}
	return out
}

// RemoveAccount deletes an account (the polling goroutine will exit on next iteration).
func (m *Manager) RemoveAccount(id string) {
	m.mu.Lock()
	delete(m.accounts, id)
	delete(m.pollBuf, id)
	m.mu.Unlock()
}

// PauseAccount puts the account into session-expired cooldown.
func (m *Manager) PauseAccount(id string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.accounts[id]; ok {
		a.Status = "paused"
		a.PausedUntil = time.Now().Add(d)
	}
}

// ResumeAccount marks the account active again.
func (m *Manager) ResumeAccount(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.accounts[id]; ok {
		a.Status = "active"
		a.PausedUntil = time.Time{}
	}
}

// ─── Conversations & Messages ─────────────────────────────────────────────────

func convKey(accountID, userID string) string { return accountID + ":" + userID }

// ListConversations returns all conversations for an account.
func (m *Manager) ListConversations(accountID string) []*Conversation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Conversation
	for k, c := range m.conversations {
		if len(k) > len(accountID) && k[:len(accountID)] == accountID {
			out = append(out, c)
		}
	}
	return out
}

// GetConversation returns the conversation for accountID+userID.
func (m *Manager) GetConversation(accountID, userID string) (*Conversation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.conversations[convKey(accountID, userID)]
	return c, ok
}

// GetContextToken returns the latest context token for replying to a user.
func (m *Manager) GetContextToken(accountID, userID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if c, ok := m.conversations[convKey(accountID, userID)]; ok {
		return c.ContextToken
	}
	return ""
}

// GetMessages returns the stored messages for a conversation (most recent last).
func (m *Manager) GetMessages(accountID, userID string, limit int) []*Message {
	m.mu.RLock()
	msgs := m.messages[convKey(accountID, userID)]
	m.mu.RUnlock()

	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}
	start := len(msgs) - limit
	out := make([]*Message, limit)
	copy(out, msgs[start:])
	return out
}

// storeMessage appends a message to the conversation buffer.
func (m *Manager) storeMessage(msg *Message) {
	k := convKey(msg.AccountID, msg.UserID)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update / create conversation.
	c, ok := m.conversations[k]
	if !ok {
		c = &Conversation{AccountID: msg.AccountID, UserID: msg.UserID}
		m.conversations[k] = c
	}
	c.UpdatedAt = msg.Timestamp
	if msg.Text != "" {
		c.LastText = msg.Text
	}
	if msg.Direction == "inbound" {
		if msg.ContextToken != "" {
			c.ContextToken = msg.ContextToken
		}
		c.Unread++
	}

	// Append message, cap at max.
	m.messages[k] = append(m.messages[k], msg)
	if len(m.messages[k]) > maxMessagesPerConv {
		m.messages[k] = m.messages[k][len(m.messages[k])-maxMessagesPerConv:]
	}
}

// AddOutboundMessage records a message sent by the bot.
func (m *Manager) AddOutboundMessage(accountID, userID, text, contextToken string) {
	msg := &Message{
		ID:           fmt.Sprintf("out-%d", time.Now().UnixNano()),
		AccountID:    accountID,
		UserID:       userID,
		Direction:    "outbound",
		Type:         "text",
		Text:         text,
		ContextToken: contextToken,
		Timestamp:    time.Now(),
	}
	m.storeMessage(msg)
}

// ─── Polling Loop ─────────────────────────────────────────────────────────────

const (
	maxConsecutiveFails = 3
	retryDelay          = 2 * time.Second
	backoffDelay        = 30 * time.Second
	sessionPauseDur     = 60 * time.Minute
)

// pollAccount is the long-poll event loop for one account (runs in a goroutine).
func (m *Manager) pollAccount(accountID string) {
	log.Printf("[poll] account=%s starting", accountID)

	m.mu.RLock()
	buf := m.pollBuf[accountID]
	m.mu.RUnlock()

	consecutiveFails := 0
	ctx := context.Background()
	_ = ctx // reserved for future cancellation

	for {
		acc, ok := m.GetAccount(accountID)
		if !ok {
			log.Printf("[poll] account=%s removed, stopping", accountID)
			return
		}

		// Respect session pause.
		if acc.IsPaused() {
			remaining := time.Until(acc.PausedUntil)
			log.Printf("[poll] account=%s paused %.0f min, sleeping", accountID, remaining.Minutes())
			time.Sleep(remaining)
			m.ResumeAccount(accountID)
			continue
		}

		client := wx.NewClient(acc.BaseURL, acc.CDNBaseURL, acc.Token)
		resp, err := client.GetUpdates(buf)
		if err != nil {
			consecutiveFails++
			log.Printf("[poll] account=%s getUpdates error (%d/%d): %v",
				accountID, consecutiveFails, maxConsecutiveFails, err)
			if consecutiveFails >= maxConsecutiveFails {
				consecutiveFails = 0
				time.Sleep(backoffDelay)
			} else {
				time.Sleep(retryDelay)
			}
			continue
		}

		// Check API-level errors.
		isAPIError := (resp.Ret != 0) || (resp.ErrCode != 0)
		if isAPIError {
			if resp.ErrCode == wx.SessionExpiredCode || resp.Ret == wx.SessionExpiredCode {
				log.Printf("[poll] account=%s session expired, pausing 60 min", accountID)
				m.PauseAccount(accountID, sessionPauseDur)
				consecutiveFails = 0
				continue
			}
			consecutiveFails++
			log.Printf("[poll] account=%s API error ret=%d errcode=%d errmsg=%s (%d/%d)",
				accountID, resp.Ret, resp.ErrCode, resp.ErrMsg,
				consecutiveFails, maxConsecutiveFails)
			if consecutiveFails >= maxConsecutiveFails {
				consecutiveFails = 0
				time.Sleep(backoffDelay)
			} else {
				time.Sleep(retryDelay)
			}
			continue
		}

		consecutiveFails = 0

		// Persist cursor.
		if resp.GetUpdatesBuf != "" {
			buf = resp.GetUpdatesBuf
			m.mu.Lock()
			m.pollBuf[accountID] = buf
			m.mu.Unlock()
		}

		// Process inbound messages.
		for _, wxMsg := range resp.Msgs {
			m.processInbound(acc, &wxMsg)
		}
	}
}

// processInbound normalises a WeixinMessage and stores it.
func (m *Manager) processInbound(acc *Account, wxMsg *wx.WeixinMessage) {
	// Skip messages sent by the bot itself.
	if wxMsg.MessageType == wx.MsgTypeBot {
		return
	}

	userID := wxMsg.FromUserID
	if userID == "" {
		return
	}

	msg := &Message{
		ID:           fmt.Sprintf("in-%d-%d", wxMsg.MessageID, time.Now().UnixNano()),
		AccountID:    acc.ID,
		UserID:       userID,
		Direction:    "inbound",
		ContextToken: wxMsg.ContextToken,
		Timestamp:    time.UnixMilli(wxMsg.CreateTimeMs),
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Extract content from item_list.
	for _, item := range wxMsg.ItemList {
		switch item.Type {
		case wx.ItemTypeText:
			if item.TextItem != nil {
				msg.Type = "text"
				msg.Text = item.TextItem.Text
			}
		case wx.ItemTypeImage:
			msg.Type = "image"
			msg.Text = "[图片]"
			if item.ImageItem != nil && item.ImageItem.Media != nil {
				msg.MediaInfo = item.ImageItem.Media.EncryptQueryParam
			}
		case wx.ItemTypeVoice:
			msg.Type = "voice"
			if item.VoiceItem != nil && item.VoiceItem.Text != "" {
				msg.Text = "[语音] " + item.VoiceItem.Text
			} else {
				msg.Text = "[语音]"
			}
		case wx.ItemTypeFile:
			msg.Type = "file"
			if item.FileItem != nil {
				msg.Text = "[文件] " + item.FileItem.FileName
				msg.MediaInfo = item.FileItem.FileName
			}
		case wx.ItemTypeVideo:
			msg.Type = "video"
			msg.Text = "[视频]"
		}
	}

	if msg.Type == "" {
		return // unknown / empty message
	}

	log.Printf("[poll] account=%s from=%s type=%s text=%q ctx=%s",
		acc.ID, userID, msg.Type, truncate(msg.Text, 60), msg.ContextToken)

	m.storeMessage(msg)
	m.notify(msg)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
