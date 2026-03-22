package wx

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ─── CDN Download ─────────────────────────────────────────────────────────────

// cdnDownloadURL builds the CDN download URL from encryptedQueryParam.
func cdnDownloadURL(cdnBaseURL, encryptedQueryParam string) string {
	base := strings.TrimRight(cdnBaseURL, "/")
	return fmt.Sprintf("%s/download?encrypted_query_param=%s", base, url.QueryEscape(encryptedQueryParam))
}

// cdnUploadURL builds the CDN upload URL.
func cdnUploadURL(cdnBaseURL, uploadParam, filekey string) string {
	base := strings.TrimRight(cdnBaseURL, "/")
	return fmt.Sprintf("%s/upload?encrypted_query_param=%s&filekey=%s",
		base, url.QueryEscape(uploadParam), url.QueryEscape(filekey))
}

// DownloadAndDecrypt downloads a CDN file and decrypts it with AES-128-ECB.
func DownloadAndDecrypt(cdnBaseURL, encryptedQueryParam, aesKeyBase64 string) ([]byte, error) {
	key, err := ParseAESKey(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("parse aes key: %w", err)
	}

	dlURL := cdnDownloadURL(cdnBaseURL, encryptedQueryParam)
	hc := &http.Client{Timeout: 60 * time.Second}
	resp, err := hc.Get(dlURL)
	if err != nil {
		return nil, fmt.Errorf("cdn get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cdn download HTTP %d: %s", resp.StatusCode, body)
	}

	ciphertext, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cdn read: %w", err)
	}

	return DecryptAESECB(ciphertext, key)
}

// ─── CDN Upload ───────────────────────────────────────────────────────────────

// UploadResult holds the CDN upload result.
type UploadResult struct {
	FileKey              string // 32-char hex
	DownloadQueryParam   string // x-encrypted-param from CDN response
	AESKeyHex            string // hex-encoded 16-byte key
	PlaintextSize        int64
	CiphertextSize       int64
}

// UploadFile encrypts a file and uploads it to the WeChat CDN.
// Returns the info needed to populate a MessageItem.
func (c *Client) UploadFile(plaintext []byte, toUserID string, mediaType int) (*UploadResult, error) {
	// Generate random AES key and file key.
	aesKey, err := NewAESKey()
	if err != nil {
		return nil, fmt.Errorf("gen aes key: %w", err)
	}
	fileKey := randomHex(16)
	md5sum := md5Hex(plaintext)

	ciphertext, err := EncryptAESECB(plaintext, aesKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	// Get CDN pre-signed upload URL.
	urlResp, err := c.GetUploadURL(&GetUploadURLRequest{
		FileKey:     fileKey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		RawSize:     int64(len(plaintext)),
		RawFileMD5:  md5sum,
		FileSize:    int64(len(ciphertext)),
		NoNeedThumb: true,
		AESKey:      hex.EncodeToString(aesKey),
	})
	if err != nil {
		return nil, fmt.Errorf("get upload url: %w", err)
	}
	if urlResp.UploadParam == "" {
		return nil, fmt.Errorf("get upload url returned empty upload_param (ret=%d err=%s)", urlResp.Ret, urlResp.ErrMsg)
	}

	// Upload encrypted bytes to CDN.
	downloadParam, err := postToCDN(c.CDNBaseURL, urlResp.UploadParam, fileKey, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("cdn upload: %w", err)
	}

	return &UploadResult{
		FileKey:            fileKey,
		DownloadQueryParam: downloadParam,
		AESKeyHex:          hex.EncodeToString(aesKey),
		PlaintextSize:      int64(len(plaintext)),
		CiphertextSize:     int64(len(ciphertext)),
	}, nil
}

// postToCDN uploads ciphertext to the CDN and returns the x-encrypted-param.
func postToCDN(cdnBaseURL, uploadParam, filekey string, ciphertext []byte) (string, error) {
	cdnURL := cdnUploadURL(cdnBaseURL, uploadParam, filekey)

	var lastErr error
	for attempt := 1; attempt <= MaxUploadRetries; attempt++ {
		hc := &http.Client{Timeout: 120 * time.Second}
		resp, err := hc.Post(cdnURL, "application/octet-stream",
			strings.NewReader(string(ciphertext)))
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// Client error — do not retry.
			return "", fmt.Errorf("cdn upload client error HTTP %d: %s", resp.StatusCode, body)
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("cdn upload server error HTTP %d: %s", resp.StatusCode, body)
			continue
		}

		param := resp.Header.Get("x-encrypted-param")
		if param == "" {
			lastErr = fmt.Errorf("cdn upload response missing x-encrypted-param header")
			continue
		}
		return param, nil
	}
	return "", fmt.Errorf("cdn upload failed after %d attempts: %w", MaxUploadRetries, lastErr)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func md5Hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	key, _ := NewAESKey()
	copy(b, key)
	return hex.EncodeToString(b)
}
