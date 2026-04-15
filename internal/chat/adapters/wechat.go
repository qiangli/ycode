package adapters

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// WeChatChannel bridges messages between the hub and WeChat via the
// WeCom (Enterprise WeChat) REST API. Pure HTTP, no external dependency.
type WeChatChannel struct {
	healthy  atomic.Bool
	logger   *slog.Logger
	accounts []wecomAccount
	inbound  chan<- channel.InboundMessage
	cancel   context.CancelFunc

	mu          sync.RWMutex
	accessToken map[string]wecomToken // accountID -> token
	server      *http.Server
}

type wecomAccount struct {
	id       string
	corpID   string
	agentID  string
	secret   string
	token    string // callback verification token
	aesKey   string // callback EncodingAESKey (optional for now)
}

type wecomToken struct {
	token     string
	expiresAt time.Time
}

// NewWeChatChannel creates a WeCom channel adapter.
func NewWeChatChannel() *WeChatChannel {
	return &WeChatChannel{
		logger:      slog.Default(),
		accessToken: make(map[string]wecomToken),
	}
}

func (w *WeChatChannel) ID() channel.ChannelID { return channel.ChannelWeChat }

func (w *WeChatChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:    false,
		Reactions:  false,
		Media:      true,
		Markdown:   true,
		MaxTextLen: 2048,
	}
}

func (w *WeChatChannel) Start(ctx context.Context, accounts []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	w.inbound = inbound
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		corpID := a.Config["corp_id"]
		secret := a.Config["secret"]
		if corpID == "" || secret == "" {
			w.logger.Warn("wechat: account missing corp_id or secret", "account", a.AccountID)
			continue
		}
		wa := wecomAccount{
			id:      a.AccountID,
			corpID:  corpID,
			agentID: a.Config["agent_id"],
			secret:  secret,
			token:   a.Config["callback_token"],
			aesKey:  a.Config["callback_aes_key"],
		}
		w.accounts = append(w.accounts, wa)

		// Refresh access token.
		go w.tokenRefreshLoop(ctx, wa)
	}

	// Start callback HTTP server for receiving messages from WeCom.
	if len(w.accounts) > 0 {
		go w.startCallbackServer(ctx)
		w.healthy.Store(true)
	}
	return nil
}

func (w *WeChatChannel) Stop(_ context.Context) error {
	w.healthy.Store(false)
	if w.cancel != nil {
		w.cancel()
	}
	if w.server != nil {
		w.server.Close()
	}
	return nil
}

func (w *WeChatChannel) Healthy() bool { return w.healthy.Load() }

// Send delivers a message to a WeCom user/group via the REST API.
func (w *WeChatChannel) Send(_ context.Context, target channel.OutboundTarget, msg channel.OutboundMessage) error {
	var account *wecomAccount
	for i := range w.accounts {
		if w.accounts[i].id == target.AccountID || target.AccountID == "" {
			account = &w.accounts[i]
			break
		}
	}
	if account == nil {
		return fmt.Errorf("wechat: no account for %q", target.AccountID)
	}

	token, err := w.getAccessToken(account)
	if err != nil {
		return fmt.Errorf("wechat: get token: %w", err)
	}

	payload := map[string]any{
		"touser":  target.ChatID,
		"msgtype": "text",
		"agentid": account.agentID,
		"text":    map[string]string{"content": msg.Content.Text},
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)
	resp, err := http.Post(url, "application/json", jsonReader(data))
	if err != nil {
		return fmt.Errorf("wechat: send: %w", err)
	}
	resp.Body.Close()
	return nil
}

// getAccessToken returns a cached or refreshed WeCom access token.
func (w *WeChatChannel) getAccessToken(account *wecomAccount) (string, error) {
	w.mu.RLock()
	cached, ok := w.accessToken[account.id]
	w.mu.RUnlock()
	if ok && time.Now().Before(cached.expiresAt) {
		return cached.token, nil
	}

	return w.refreshAccessToken(account)
}

func (w *WeChatChannel) refreshAccessToken(account *wecomAccount) (string, error) {
	url := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		account.corpID, account.secret,
	)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat: gettoken: %d %s", result.ErrCode, result.ErrMsg)
	}

	w.mu.Lock()
	w.accessToken[account.id] = wecomToken{
		token:     result.AccessToken,
		expiresAt: time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second),
	}
	w.mu.Unlock()

	return result.AccessToken, nil
}

func (w *WeChatChannel) tokenRefreshLoop(ctx context.Context, account wecomAccount) {
	ticker := time.NewTicker(90 * time.Minute)
	defer ticker.Stop()

	// Initial refresh.
	if _, err := w.refreshAccessToken(&account); err != nil {
		w.logger.Error("wechat: initial token refresh", "error", err, "account", account.id)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.refreshAccessToken(&account); err != nil {
				w.logger.Error("wechat: token refresh", "error", err, "account", account.id)
			}
		}
	}
}

// startCallbackServer starts an HTTP server to receive WeCom callback messages.
func (w *WeChatChannel) startCallbackServer(ctx context.Context) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		w.logger.Error("wechat: callback listen", "error", err)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	w.logger.Info("wechat: callback server started", "port", port)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", w.handleCallback)

	w.server = &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		w.server.Close()
	}()
	w.server.Serve(listener)
}

// handleCallback processes WeCom callback verification and message delivery.
func (w *WeChatChannel) handleCallback(rw http.ResponseWriter, r *http.Request) {
	// URL verification (GET request).
	if r.Method == http.MethodGet {
		w.handleVerification(rw, r)
		return
	}

	// Message callback (POST request).
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, "read error", http.StatusBadRequest)
		return
	}

	var xmlMsg struct {
		XMLName    xml.Name `xml:"xml"`
		ToUserName string   `xml:"ToUserName"`
		FromUser   string   `xml:"FromUserName"`
		MsgType    string   `xml:"MsgType"`
		Content    string   `xml:"Content"`
		MsgID      string   `xml:"MsgId"`
		AgentID    string   `xml:"AgentID"`
		CreateTime int64    `xml:"CreateTime"`
	}
	if err := xml.Unmarshal(body, &xmlMsg); err != nil {
		w.logger.Error("wechat: parse callback xml", "error", err)
		http.Error(rw, "parse error", http.StatusBadRequest)
		return
	}

	if xmlMsg.MsgType != "text" || xmlMsg.Content == "" {
		rw.WriteHeader(http.StatusOK)
		return
	}

	// Find account by agent ID.
	accountID := "default"
	for _, a := range w.accounts {
		if a.agentID == xmlMsg.AgentID || a.corpID == xmlMsg.ToUserName {
			accountID = a.id
			break
		}
	}

	w.inbound <- channel.InboundMessage{
		ChannelID:  channel.ChannelWeChat,
		AccountID:  accountID,
		SenderID:   xmlMsg.FromUser,
		SenderName: xmlMsg.FromUser, // WeCom uses user IDs; display name requires API lookup
		Content:    channel.MessageContent{Text: xmlMsg.Content},
		PlatformID: xmlMsg.MsgID,
		ChatID:     xmlMsg.FromUser, // 1:1 chat keyed by sender
		Timestamp:  time.Unix(xmlMsg.CreateTime, 0),
	}

	rw.WriteHeader(http.StatusOK)
}

// handleVerification handles WeCom URL verification for callback setup.
func (w *WeChatChannel) handleVerification(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	signature := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echoStr := q.Get("echostr")

	// Find an account to verify against.
	var token string
	for _, a := range w.accounts {
		if a.token != "" {
			token = a.token
			break
		}
	}

	// Verify signature.
	params := []string{token, timestamp, nonce, echoStr}
	sort.Strings(params)
	h := sha1.New()
	h.Write([]byte(strings.Join(params, "")))
	computed := fmt.Sprintf("%x", h.Sum(nil))

	if computed != signature {
		http.Error(rw, "invalid signature", http.StatusForbidden)
		return
	}

	// Return echostr to confirm.
	rw.Write([]byte(echoStr))
}
