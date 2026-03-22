// Package wx is a Go client for the WeChat iLink Bot API.
package wx

// ─── Protocol Constants ───────────────────────────────────────────────────────

const (
	DefaultBaseURL    = "https://ilinkai.weixin.qq.com"
	DefaultCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
	ChannelVersion    = "1.0.2"
	DefaultBotType    = "3"

	LongPollTimeoutMs  = 35_000
	DefaultAPITimeout  = 15_000
	DefaultCfgTimeout  = 10_000
	MaxUploadRetries   = 3
	SessionExpiredCode = -14
)

// Message type values.
const (
	MsgTypeNone = 0
	MsgTypeUser = 1
	MsgTypeBot  = 2
)

// MessageItem type values.
const (
	ItemTypeNone  = 0
	ItemTypeText  = 1
	ItemTypeImage = 2
	ItemTypeVoice = 3
	ItemTypeFile  = 4
	ItemTypeVideo = 5
)

// MessageState values.
const (
	MsgStateNew        = 0
	MsgStateGenerating = 1
	MsgStateFinish     = 2
)

// UploadMediaType values.
const (
	UploadImage = 1
	UploadVideo = 2
	UploadFile  = 3
	UploadVoice = 4
)

// ─── Request / Response Types ─────────────────────────────────────────────────

// BaseInfo is attached to every CGI request.
type BaseInfo struct {
	ChannelVersion string `json:"channel_version"`
}

// GetUpdatesRequest — POST /ilink/bot/getupdates.
type GetUpdatesRequest struct {
	GetUpdatesBuf string   `json:"get_updates_buf"`
	BaseInfo      BaseInfo `json:"base_info"`
}

// GetUpdatesResponse — POST /ilink/bot/getupdates response.
type GetUpdatesResponse struct {
	Ret                 int              `json:"ret"`
	ErrCode             int              `json:"errcode"`
	ErrMsg              string           `json:"errmsg"`
	Msgs                []WeixinMessage  `json:"msgs"`
	GetUpdatesBuf       string           `json:"get_updates_buf"`
	LongpollingTimeoutMs int             `json:"longpolling_timeout_ms"`
}

// WeixinMessage is a single message from getUpdates.
type WeixinMessage struct {
	Seq          int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMs int64         `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64         `json:"update_time_ms,omitempty"`
	DeleteTimeMs int64         `json:"delete_time_ms,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []MessageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

// MessageItem is one content element inside a WeixinMessage.
type MessageItem struct {
	Type         int        `json:"type"`
	CreateTimeMs int64      `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64      `json:"update_time_ms,omitempty"`
	IsCompleted  bool       `json:"is_completed,omitempty"`
	TextItem     *TextItem  `json:"text_item,omitempty"`
	ImageItem    *ImageItem `json:"image_item,omitempty"`
	VoiceItem    *VoiceItem `json:"voice_item,omitempty"`
	FileItem     *FileItem  `json:"file_item,omitempty"`
	VideoItem    *VideoItem `json:"video_item,omitempty"`
}

// TextItem holds plain text content.
type TextItem struct {
	Text string `json:"text,omitempty"`
}

// CDNMedia is a reference to a file on the WeChat CDN.
type CDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`  // base64-encoded AES key
	EncryptType       int    `json:"encrypt_type,omitempty"`
}

// ImageItem holds image media info.
type ImageItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	ThumbMedia *CDNMedia `json:"thumb_media,omitempty"`
	AESKey     string    `json:"aeskey,omitempty"` // hex-encoded (alternate form)
	URL        string    `json:"url,omitempty"`
	MidSize    int64     `json:"mid_size,omitempty"`
}

// VoiceItem holds voice media info.
type VoiceItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	EncodeType int       `json:"encode_type,omitempty"`
	SampleRate int       `json:"sample_rate,omitempty"`
	Playtime   int       `json:"playtime,omitempty"`
	Text       string    `json:"text,omitempty"` // STT result
}

// FileItem holds file attachment info.
type FileItem struct {
	Media    *CDNMedia `json:"media,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	MD5      string    `json:"md5,omitempty"`
	Len      string    `json:"len,omitempty"` // plaintext size as string
}

// VideoItem holds video media info.
type VideoItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	VideoSize  int64     `json:"video_size,omitempty"`
	PlayLength int       `json:"play_length,omitempty"`
}

// SendMessageRequest — POST /ilink/bot/sendmessage.
type SendMessageRequest struct {
	Msg      *WeixinMessage `json:"msg"`
	BaseInfo BaseInfo       `json:"base_info"`
}

// GetUploadURLRequest — POST /ilink/bot/getuploadurl.
type GetUploadURLRequest struct {
	FileKey    string   `json:"filekey"`
	MediaType  int      `json:"media_type"`
	ToUserID   string   `json:"to_user_id"`
	RawSize    int64    `json:"rawsize"`
	RawFileMD5 string   `json:"rawfilemd5"`
	FileSize   int64    `json:"filesize"`
	NoNeedThumb bool    `json:"no_need_thumb"`
	AESKey     string   `json:"aeskey"` // hex
	BaseInfo   BaseInfo `json:"base_info"`
}

// GetUploadURLResponse — POST /ilink/bot/getuploadurl response.
type GetUploadURLResponse struct {
	Ret         int    `json:"ret"`
	ErrMsg      string `json:"errmsg"`
	UploadParam string `json:"upload_param"`
}

// QRCodeResponse — GET /ilink/bot/get_bot_qrcode response.
type QRCodeResponse struct {
	QRCode           string `json:"qrcode"`            // raw token (for status polling)
	QRCodeImgContent string `json:"qrcode_img_content"` // WeChat-scannable URL
}

// QRCodeStatusResponse — GET /ilink/bot/get_qrcode_status response.
type QRCodeStatusResponse struct {
	Ret         int    `json:"ret"`
	Status      string `json:"status"` // wait | scaned | confirmed | expired
	BotToken    string `json:"bot_token"`
	ILinkBotID  string `json:"ilink_bot_id"`
	BaseURL     string `json:"baseurl"`
	ILinkUserID string `json:"ilink_user_id"`
}
