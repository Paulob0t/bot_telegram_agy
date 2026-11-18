package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	token string
	http  *http.Client
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type User struct {
	ID int64 `json:"id"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
}

type Document struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
}

type Message struct {
	MessageID int64       `json:"message_id"`
	From      *User       `json:"from,omitempty"`
	Chat      Chat        `json:"chat"`
	Text      string      `json:"text,omitempty"`
	Caption   string      `json:"caption,omitempty"`
	Photo     []PhotoSize `json:"photo,omitempty"`
	Document  *Document   `json:"document,omitempty"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

func New(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 40 * time.Second},
	}
}

func (c *Client) apiURL(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.token, method)
}

func (c *Client) GetUpdates(offset int64, timeout int) ([]Update, error) {
	payload := map[string]any{"offset": offset, "timeout": timeout}
	raw, err := c.postJSON("getUpdates", payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func (c *Client) SendMessage(chatID int64, text string, kb *InlineKeyboard) (int64, error) {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "Markdown",
		"disable_web_page_preview": true,
	}
	if kb != nil {
		payload["reply_markup"] = kb
	}
	raw, err := c.postJSON("sendMessage", payload)
	if err != nil {
		// Fallback sin Markdown si falla formato
		delete(payload, "parse_mode")
		raw, err = c.postJSON("sendMessage", payload)
		if err != nil {
			return 0, err
		}
	}
	var resp struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	_ = json.Unmarshal(raw, &resp)
	return resp.Result.MessageID, nil
}

func (c *Client) EditMessage(chatID, msgID int64, text string, kb *InlineKeyboard) {
	payload := map[string]any{
		"chat_id":                  chatID,
		"message_id":               msgID,
		"text":                     text,
		"parse_mode":               "Markdown",
		"disable_web_page_preview": true,
	}
	if kb != nil {
		payload["reply_markup"] = kb
	}
	if _, err := c.postJSON("editMessageText", payload); err != nil {
		delete(payload, "parse_mode")
		_, _ = c.postJSON("editMessageText", payload)
	}
}

func (c *Client) AnswerCallback(cbID, text string) {
	_, _ = c.postJSON("answerCallbackQuery", map[string]any{
		"callback_query_id": cbID,
		"text":              text,
	})
}

func (c *Client) SendPhoto(chatID int64, filePath, caption string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	if caption != "" {
		_ = w.WriteField("caption", caption)
	}
	part, err := w.CreateFormFile("photo", filepath.Base(filePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, c.apiURL("sendPhoto"), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) DownloadFile(fileID, destPath string) error {
	raw, err := c.postJSON("getFile", map[string]any{"file_id": fileID})
	if err != nil {
		return err
	}
	var resp struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Result.FilePath == "" {
		return fmt.Errorf("no se pudo resolver file_path para %s", fileID)
	}
	dlURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.token, resp.Result.FilePath)
	httpResp, err := c.http.Get(dlURL)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("error descargando archivo (%d)", httpResp.StatusCode)
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, httpResp.Body)
	return err
}

func (c *Client) postJSON(method string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Post(c.apiURL(method), "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("telegram %s status %d: %s", method, resp.StatusCode, string(raw))
	}
	return raw, nil
}
