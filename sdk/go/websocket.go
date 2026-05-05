package license

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient WebSocket 客户端
type WSClient struct {
	client            *Client
	conn              *websocket.Conn
	serverURL         string
	sessionID         string
	connected         bool
	reconnect         bool
	manualClose       bool
	reconnecting      bool
	reconnectInterval time.Duration
	publicKey         *rsa.PublicKey

	// 消息处理
	handlers     map[string]InstructionHandler
	onConnect    func()
	onDisconnect func(error)
	onError      func(error)

	// 内部
	send chan []byte
	done chan struct{}
	mu   sync.RWMutex
}

// WSMessage WebSocket 消息
type WSMessage struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Instruction 指令
type Instruction struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
	Nonce     string          `json:"nonce"`
	Signature string          `json:"signature"`
	ExpiresAt int64           `json:"expires"`
}

// InstructionHandler 指令处理函数
type InstructionHandler func(instruction *Instruction) (result interface{}, err error)

// WSClientOption 配置选项
type WSClientOption func(*WSClient)

// WithReconnect 设置自动重连
func WithReconnect(enabled bool, interval time.Duration) WSClientOption {
	return func(c *WSClient) {
		c.reconnect = enabled
		c.reconnectInterval = interval
	}
}

// WithConnectCallback 设置连接回调
func WithConnectCallback(callback func()) WSClientOption {
	return func(c *WSClient) {
		c.onConnect = callback
	}
}

// WithDisconnectCallback 设置断开回调
func WithDisconnectCallback(callback func(error)) WSClientOption {
	return func(c *WSClient) {
		c.onDisconnect = callback
	}
}

// WithErrorCallback 设置错误回调
func WithErrorCallback(callback func(error)) WSClientOption {
	return func(c *WSClient) {
		c.onError = callback
	}
}

// WithWSPublicKey 设置公钥 (用于签名验证)
func WithWSPublicKey(publicKey *rsa.PublicKey) WSClientOption {
	return func(c *WSClient) {
		c.publicKey = publicKey
	}
}

// NewWSClient 创建 WebSocket 客户端
func NewWSClient(client *Client, opts ...WSClientOption) *WSClient {
	// 将 HTTP URL 转换为 WebSocket URL
	serverURL := client.GetServerURL()
	u, _ := url.Parse(serverURL)
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api/client/ws"

	wsc := &WSClient{
		client:            client,
		serverURL:         u.String(),
		reconnect:         true,
		reconnectInterval: 5 * time.Second,
		handlers:          make(map[string]InstructionHandler),
		send:              make(chan []byte, 256),
		done:              make(chan struct{}),
	}

	for _, opt := range opts {
		opt(wsc)
	}

	return wsc
}

// Connect 连接服务器
func (c *WSClient) Connect() error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	accessToken, err := c.client.getValidAccessToken()
	if err != nil {
		return err
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)

	// 建立连接
	conn, _, err := websocket.DefaultDialer.Dial(c.serverURL, headers)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}

	// 发送认证消息
	authPayload, _ := json.Marshal(map[string]string{
		"app_key":    c.client.GetAppKey(),
		"machine_id": c.client.GetMachineID(),
	})
	authMsg := WSMessage{
		Type:    "auth",
		Payload: authPayload,
	}
	if err := conn.WriteJSON(authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("发送认证失败: %w", err)
	}

	// 等待认证响应
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var response WSMessage
	if err := conn.ReadJSON(&response); err != nil {
		conn.Close()
		return fmt.Errorf("读取认证响应失败: %w", err)
	}

	if response.Type == "error" {
		conn.Close()
		var errPayload struct {
			Message string `json:"message"`
		}
		json.Unmarshal(response.Payload, &errPayload)
		return fmt.Errorf("认证失败: %s", errPayload.Message)
	}

	if response.Type != "auth_ok" {
		conn.Close()
		return errors.New("认证失败: 未知响应")
	}

	// 解析会话ID
	var authOK struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(response.Payload, &authOK)
	c.sessionID = authOK.SessionID

	// 重置读取超时
	conn.SetReadDeadline(time.Time{})

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.manualClose = false
	c.reconnecting = false
	c.done = make(chan struct{})
	c.mu.Unlock()

	// 启动读写协程
	go c.readPump()
	go c.writePump()

	// 回调
	if c.onConnect != nil {
		c.onConnect()
	}

	log.Printf("WebSocket: 已连接 session=%s", c.sessionID)
	return nil
}

// Disconnect 断开连接
func (c *WSClient) Disconnect() {
	c.mu.Lock()
	if c.manualClose {
		c.mu.Unlock()
		return
	}
	c.manualClose = true
	c.connected = false
	close(c.done)
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

// IsConnected 是否已连接
func (c *WSClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetSessionID 获取会话ID
func (c *WSClient) GetSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// RegisterHandler 注册指令处理器
func (c *WSClient) RegisterHandler(instructionType string, handler InstructionHandler) {
	c.mu.Lock()
	c.handlers[instructionType] = handler
	c.mu.Unlock()
}

// RegisterHandlers 批量注册指令处理器
func (c *WSClient) RegisterHandlers(handlers map[string]InstructionHandler) {
	c.mu.Lock()
	for t, h := range handlers {
		c.handlers[t] = h
	}
	c.mu.Unlock()
}

// SendStatus 发送状态上报
func (c *WSClient) SendStatus(status map[string]interface{}) error {
	payload, _ := json.Marshal(status)
	msg := WSMessage{
		Type:    "status",
		Payload: payload,
	}
	return c.sendMessage(msg)
}

// readPump 读取消息
func (c *WSClient) readPump() {
	defer func() {
		c.handleDisconnect(nil)
	}()

	c.conn.SetPongHandler(func(string) error {
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		c.handleMessage(&msg)
	}
}

// writePump 写入消息
func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			// 发送 ping
			pingPayload, _ := json.Marshal(map[string]int64{"ts": time.Now().Unix()})
			msg := WSMessage{Type: "ping", Payload: pingPayload}
			data, _ := json.Marshal(msg)
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

// handleMessage 处理消息
func (c *WSClient) handleMessage(msg *WSMessage) {
	switch msg.Type {
	case "pong":
		// 心跳响应，忽略

	case "instruction":
		c.handleInstruction(msg)

	case "error":
		var errPayload struct {
			Message string `json:"message"`
		}
		json.Unmarshal(msg.Payload, &errPayload)
		if c.onError != nil {
			c.onError(errors.New(errPayload.Message))
		}

	default:
		log.Printf("WebSocket: 未知消息类型 %s", msg.Type)
	}
}

// handleInstruction 处理指令
func (c *WSClient) handleInstruction(msg *WSMessage) {
	var inst Instruction
	if err := json.Unmarshal(msg.Payload, &inst); err != nil {
		log.Printf("WebSocket: 解析指令失败: %v", err)
		return
	}

	// 验证过期时间
	if inst.ExpiresAt > 0 && time.Now().Unix() > inst.ExpiresAt {
		log.Printf("WebSocket: 指令已过期 id=%s", inst.ID)
		c.sendInstructionResult(inst.ID, "failed", nil, "指令已过期")
		return
	}

	// 验证签名
	signData := fmt.Sprintf("%s:%s:%s:%s", inst.ID, inst.Type, string(inst.Payload), inst.Nonce)
	if err := c.verifySignature([]byte(signData), inst.Signature); err != nil {
		log.Printf("WebSocket: 指令签名验证失败 id=%s", inst.ID)
		c.sendInstructionResult(inst.ID, "failed", nil, "签名验证失败")
		return
	}

	// 查找处理器
	c.mu.RLock()
	handler, ok := c.handlers[inst.Type]
	c.mu.RUnlock()

	if !ok {
		log.Printf("WebSocket: 未知指令类型 %s", inst.Type)
		c.sendInstructionResult(inst.ID, "failed", nil, "未知指令类型")
		return
	}

	// 执行指令
	go func() {
		result, err := handler(&inst)
		if err != nil {
			c.sendInstructionResult(inst.ID, "failed", nil, err.Error())
		} else {
			c.sendInstructionResult(inst.ID, "success", result, "")
		}
	}()
}

// sendInstructionResult 发送指令执行结果
func (c *WSClient) sendInstructionResult(instructionID, status string, result interface{}, errMsg string) {
	payload := map[string]interface{}{
		"instruction_id": instructionID,
		"status":         status,
	}
	if result != nil {
		payload["result"] = result
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}

	payloadBytes, _ := json.Marshal(payload)
	msg := WSMessage{
		Type:    "instruction_result",
		ID:      instructionID,
		Payload: payloadBytes,
	}
	c.sendMessage(msg)
}

// sendMessage 发送消息
func (c *WSClient) sendMessage(msg WSMessage) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return errors.New("未连接")
	}
	c.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case c.send <- data:
		return nil
	default:
		return errors.New("发送缓冲区满")
	}
}

// handleDisconnect 处理断开连接
func (c *WSClient) handleDisconnect(err error) {
	c.mu.Lock()
	wasConnected := c.connected
	c.connected = false
	shouldReconnect := c.reconnect && !c.manualClose && !c.reconnecting
	if shouldReconnect {
		c.reconnecting = true
	}
	c.mu.Unlock()

	if wasConnected {
		log.Printf("WebSocket: 连接断开")
		if c.onDisconnect != nil {
			c.onDisconnect(err)
		}
	}

	// 自动重连
	if shouldReconnect {
		go c.reconnectLoop()
	}
}

func (c *WSClient) reconnectLoop() {
	for {
		time.Sleep(c.reconnectInterval)
		c.mu.RLock()
		shouldContinue := c.reconnect && !c.manualClose && !c.connected
		c.mu.RUnlock()
		if !shouldContinue {
			c.mu.Lock()
			c.reconnecting = false
			c.mu.Unlock()
			return
		}

		log.Printf("WebSocket: 尝试重连...")
		if err := c.Connect(); err != nil {
			log.Printf("WebSocket: 重连失败: %v", err)
			continue
		}
		return
	}
}

// verifySignature 验证签名
func (c *WSClient) verifySignature(data []byte, signatureBase64 string) error {
	if c.publicKey == nil {
		return nil // 如果没有设置公钥，跳过验证
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return err
	}

	hashed := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(c.publicKey, crypto.SHA256, hashed[:], signature)
}
