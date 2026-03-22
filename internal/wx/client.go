package wx

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is an authenticated WeChat iLink Bot API client.
type Client struct {
	BaseURL    string
	CDNBaseURL string
	Token      string
}

// NewClient creates a new API client.
func NewClient(baseURL, cdnBaseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if cdnBaseURL == "" {
		cdnBaseURL = DefaultCDNBaseURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		CDNBaseURL: strings.TrimRight(cdnBaseURL, "/"),
		Token:      token,
	}
}

// randomWechatUIN generates the X-WECHAT-UIN header value:
// random uint32 → decimal string → base64.
func randomWechatUIN() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	n := binary.BigEndian.Uint32(b)
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", n)))
}

func baseInfo() BaseInfo { return BaseInfo{ChannelVersion: ChannelVersion} }

// post performs an authenticated JSON POST to the given endpoint path.
func (c *Client) post(endpoint string, payload any, timeoutMs int) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := c.BaseURL + "/" + strings.TrimLeft(endpoint, "/")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	req.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	hc := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	return data, nil
}

// ─── GetUpdates ───────────────────────────────────────────────────────────────

// GetUpdates long-polls for new messages.
// On client-side timeout it returns an empty response (normal for long-poll).
func (c *Client) GetUpdates(buf string) (*GetUpdatesResponse, error) {
	data, err := c.post("ilink/bot/getupdates", GetUpdatesRequest{
		GetUpdatesBuf: buf,
		BaseInfo:      baseInfo(),
	}, LongPollTimeoutMs+5000) // give 5s extra over the server timeout
	if err != nil {
		// Timeout = normal; return empty response so caller retries.
		if isTimeoutError(err) {
			return &GetUpdatesResponse{GetUpdatesBuf: buf}, nil
		}
		return nil, err
	}
	var resp GetUpdatesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &resp, nil
}

// ─── SendMessage ──────────────────────────────────────────────────────────────

// SendText sends a plain text message to toUserID.
func (c *Client) SendText(toUserID, text, contextToken, clientID string) error {
	msg := &WeixinMessage{
		FromUserID:   "",
		ToUserID:     toUserID,
		ClientID:     clientID,
		MessageType:  MsgTypeBot,
		MessageState: MsgStateFinish,
		ContextToken: contextToken,
		ItemList: []MessageItem{
			{Type: ItemTypeText, TextItem: &TextItem{Text: text}},
		},
	}
	_, err := c.post("ilink/bot/sendmessage", SendMessageRequest{Msg: msg, BaseInfo: baseInfo()}, DefaultAPITimeout)
	return err
}

// SendMediaMessage sends a pre-built WeixinMessage (image, video, file).
func (c *Client) SendMediaMessage(msg *WeixinMessage) error {
	_, err := c.post("ilink/bot/sendmessage", SendMessageRequest{Msg: msg, BaseInfo: baseInfo()}, DefaultAPITimeout)
	return err
}

// ─── CDN Upload URL ───────────────────────────────────────────────────────────

// GetUploadURL requests a CDN pre-signed upload URL.
func (c *Client) GetUploadURL(req *GetUploadURLRequest) (*GetUploadURLResponse, error) {
	req.BaseInfo = baseInfo()
	data, err := c.post("ilink/bot/getuploadurl", req, DefaultAPITimeout)
	if err != nil {
		return nil, err
	}
	var resp GetUploadURLResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &resp, nil
}

// ─── QR Login ─────────────────────────────────────────────────────────────────

// GetQRCode fetches a bot login QR code.
func GetQRCode(apiBaseURL, botType string) (*QRCodeResponse, error) {
	if apiBaseURL == "" {
		apiBaseURL = DefaultBaseURL
	}
	if botType == "" {
		botType = DefaultBotType
	}
	base := strings.TrimRight(apiBaseURL, "/")
	url := fmt.Sprintf("%s/ilink/bot/get_bot_qrcode?bot_type=%s", base, botType)

	hc := &http.Client{Timeout: 15 * time.Second}
	resp, err := hc.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get qrcode: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get qrcode HTTP %d: %s", resp.StatusCode, data)
	}
	var qr QRCodeResponse
	if err := json.Unmarshal(data, &qr); err != nil {
		return nil, fmt.Errorf("unmarshal qrcode: %w", err)
	}
	return &qr, nil
}

// PollQRStatus polls the QR code login status (long-poll, 35 s server hold).
func PollQRStatus(apiBaseURL, qrcode string) (*QRCodeStatusResponse, error) {
	base := strings.TrimRight(apiBaseURL, "/")
	url := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", base, qrcode)

	hc := &http.Client{Timeout: (LongPollTimeoutMs + 5000) * time.Millisecond}
	resp, err := hc.Get(url)
	if err != nil {
		if isTimeoutError(err) {
			return &QRCodeStatusResponse{Status: "wait"}, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qr status HTTP %d: %s", resp.StatusCode, data)
	}
	var s QRCodeStatusResponse
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal qr status: %w", err)
	}
	return &s, nil
}

// isTimeoutError returns true for net/http timeout errors.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline exceeded") ||
		strings.Contains(err.Error(), "context deadline")
}
