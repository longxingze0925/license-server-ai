package handler

import (
	"encoding/json"
	"license-server/internal/config"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/service"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return isWebSocketOriginAllowed(r.Header.Get("Origin"))
	},
}

func isWebSocketOriginAllowed(origin string) bool {
	// 非浏览器客户端通常不带 Origin，允许通过
	if origin == "" {
		return true
	}

	cfg := config.Get()
	if cfg == nil {
		return false
	}

	normalizedOrigin := strings.TrimRight(strings.ToLower(strings.TrimSpace(origin)), "/")
	for _, allowed := range cfg.Security.AllowedOrigins {
		normalizedAllowed := strings.TrimRight(strings.ToLower(strings.TrimSpace(allowed)), "/")
		if normalizedAllowed == "*" || normalizedAllowed == normalizedOrigin {
			return true
		}
	}
	return false
}

// DeviceClient 设备客户端连接
type DeviceClient struct {
	conn        *websocket.Conn
	send        chan []byte
	appID       string
	deviceID    string
	machineID   string
	sessionID   string
	connectedAt time.Time
	lastPingAt  time.Time
	mu          sync.Mutex
}

// WebSocketHub 管理所有 WebSocket 连接
type WebSocketHub struct {
	// 按应用ID分组的客户端
	clients map[string]map[string]*DeviceClient // appID -> machineID -> client
	// 按会话ID索引
	sessions   map[string]*DeviceClient // sessionID -> client
	register   chan *DeviceClient
	unregister chan *DeviceClient
	broadcast  chan *BroadcastMessage
	mu         sync.RWMutex
}

// BroadcastMessage 广播消息
type BroadcastMessage struct {
	AppID     string
	DeviceID  string // 空表示广播给应用下所有设备
	MachineID string
	Message   []byte
}

var hub *WebSocketHub

func init() {
	hub = NewWebSocketHub()
	go hub.Run()
}

// GetHub 获取 Hub 实例
func GetHub() *WebSocketHub {
	return hub
}

// NewWebSocketHub 创建 Hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[string]map[string]*DeviceClient),
		sessions:   make(map[string]*DeviceClient),
		register:   make(chan *DeviceClient),
		unregister: make(chan *DeviceClient),
		broadcast:  make(chan *BroadcastMessage, 256),
	}
}

// Run 运行 Hub
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.appID] == nil {
				h.clients[client.appID] = make(map[string]*DeviceClient)
			}
			// 如果已有连接，先关闭旧连接
			if old, ok := h.clients[client.appID][client.machineID]; ok {
				close(old.send)
				delete(h.sessions, old.sessionID)
			}
			h.clients[client.appID][client.machineID] = client
			h.sessions[client.sessionID] = client
			h.mu.Unlock()

			// 记录连接
			h.recordConnection(client, "connected")
			log.Printf("WebSocket: 设备连接 app=%s machine=%s session=%s", client.appID, client.machineID, client.sessionID)

		case client := <-h.unregister:
			h.mu.Lock()
			if appClients, ok := h.clients[client.appID]; ok {
				if c, ok := appClients[client.machineID]; ok && c.sessionID == client.sessionID {
					delete(appClients, client.machineID)
					delete(h.sessions, client.sessionID)
					close(client.send)
				}
			}
			h.mu.Unlock()

			// 记录断开
			h.recordConnection(client, "disconnected")
			log.Printf("WebSocket: 设备断开 app=%s machine=%s", client.appID, client.machineID)

		case msg := <-h.broadcast:
			h.mu.RLock()
			if msg.MachineID != "" {
				// 发送给特定设备
				if appClients, ok := h.clients[msg.AppID]; ok {
					if client, ok := appClients[msg.MachineID]; ok {
						select {
						case client.send <- msg.Message:
						default:
							// 发送缓冲区满，跳过
						}
					}
				}
			} else {
				// 广播给应用下所有设备
				if appClients, ok := h.clients[msg.AppID]; ok {
					for _, client := range appClients {
						select {
						case client.send <- msg.Message:
						default:
						}
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// recordConnection 记录连接状态到数据库
func (h *WebSocketHub) recordConnection(client *DeviceClient, status string) {
	if status == "connected" {
		conn := model.DeviceConnection{
			AppID:       client.appID,
			DeviceID:    client.deviceID,
			MachineID:   client.machineID,
			SessionID:   client.sessionID,
			ConnectedAt: client.connectedAt,
			LastPingAt:  client.connectedAt,
			Status:      status,
		}
		model.DB.Create(&conn)
	} else {
		model.DB.Model(&model.DeviceConnection{}).
			Where("session_id = ?", client.sessionID).
			Update("status", status)
	}
}

// SendToDevice 发送消息给特定设备
func (h *WebSocketHub) SendToDevice(appID, machineID string, message []byte) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if appClients, ok := h.clients[appID]; ok {
		if client, ok := appClients[machineID]; ok {
			select {
			case client.send <- message:
				return true
			default:
				return false
			}
		}
	}
	return false
}

// BroadcastToApp 广播消息给应用下所有设备
func (h *WebSocketHub) BroadcastToApp(appID string, message []byte) {
	h.broadcast <- &BroadcastMessage{
		AppID:   appID,
		Message: message,
	}
}

// GetOnlineDevices 获取在线设备列表
func (h *WebSocketHub) GetOnlineDevices(appID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var devices []string
	if appClients, ok := h.clients[appID]; ok {
		for machineID := range appClients {
			devices = append(devices, machineID)
		}
	}
	return devices
}

// IsDeviceOnline 检查设备是否在线
func (h *WebSocketHub) IsDeviceOnline(appID, machineID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if appClients, ok := h.clients[appID]; ok {
		_, ok := appClients[machineID]
		return ok
	}
	return false
}

// WebSocketHandler WebSocket 处理器
type WebSocketHandler struct {
	scriptService *service.SecureScriptService
}

func NewWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		scriptService: service.NewSecureScriptService(),
	}
}

// WSMessage WebSocket 消息结构
type WSMessage struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WSAuthPayload 认证消息
type WSAuthPayload struct {
	AppKey    string `json:"app_key"`
	MachineID string `json:"machine_id"`
	Token     string `json:"token"` // 可选，用于额外验证
}

// HandleWebSocket 处理 WebSocket 连接
func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// 等待认证消息 (10秒超时)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	var authMsg WSMessage
	if err := json.Unmarshal(message, &authMsg); err != nil || authMsg.Type != "auth" {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"需要认证"}`)})
		conn.Close()
		return
	}

	var authPayload WSAuthPayload
	if err := json.Unmarshal(authMsg.Payload, &authPayload); err != nil {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"认证参数错误"}`)})
		conn.Close()
		return
	}
	if authPayload.AppKey == "" || authPayload.MachineID == "" {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"缺少认证参数"}`)})
		conn.Close()
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "app_key = ? AND status = ?", authPayload.AppKey, model.AppStatusActive).Error; err != nil {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"无效的应用"}`)})
		conn.Close()
		return
	}

	// 可选 Token 验证（用于额外校验）
	if authPayload.Token != "" {
		claims, err := crypto.ParseToken(authPayload.Token, config.Get().JWT.Secret)
		if err != nil || claims.TenantID != app.TenantID {
			conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"Token 无效"}`)})
			conn.Close()
			return
		}
	}

	// 验证设备
	var device model.Device
	if err := model.DB.Preload("License").Preload("Subscription").
		Where("machine_id = ? AND tenant_id = ?", authPayload.MachineID, app.TenantID).
		Order("created_at DESC").
		First(&device).Error; err != nil {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"设备未授权"}`)})
		conn.Close()
		return
	}
	if (device.License == nil || device.License.AppID != app.ID) &&
		(device.Subscription == nil || device.Subscription.AppID != app.ID) {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"设备未绑定当前应用"}`)})
		conn.Close()
		return
	}

	// 生成会话ID
	sessionID, _ := crypto.GenerateNonce(16)

	// 创建客户端
	client := &DeviceClient{
		conn:        conn,
		send:        make(chan []byte, 256),
		appID:       app.ID,
		deviceID:    device.ID,
		machineID:   authPayload.MachineID,
		sessionID:   sessionID,
		connectedAt: time.Now(),
		lastPingAt:  time.Now(),
	}

	// 注册客户端
	hub.register <- client

	// 发送认证成功
	authOK, _ := json.Marshal(map[string]string{
		"session_id": sessionID,
		"message":    "认证成功",
	})
	conn.WriteJSON(WSMessage{Type: "auth_ok", Payload: authOK})

	// 重置读取超时
	conn.SetReadDeadline(time.Time{})

	// 启动读写协程
	go client.writePump()
	go client.readPump(h)
}

// readPump 读取消息
func (c *DeviceClient) readPump(h *WebSocketHandler) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(64 * 1024) // 64KB
	c.conn.SetPongHandler(func(string) error {
		c.mu.Lock()
		c.lastPingAt = time.Now()
		c.mu.Unlock()
		// 更新数据库
		model.DB.Model(&model.DeviceConnection{}).
			Where("session_id = ?", c.sessionID).
			Update("last_ping_at", time.Now())
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		h.handleMessage(c, &msg)
	}
}

// writePump 写入消息
func (c *DeviceClient) writePump() {
	ticker := time.NewTicker(30 * time.Second) // 心跳间隔
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage 处理客户端消息
func (h *WebSocketHandler) handleMessage(client *DeviceClient, msg *WSMessage) {
	switch msg.Type {
	case "ping":
		// 心跳响应
		pong, _ := json.Marshal(map[string]int64{"ts": time.Now().Unix()})
		response := WSMessage{Type: "pong", Payload: pong}
		data, _ := json.Marshal(response)
		client.send <- data

	case "instruction_result":
		// 指令执行结果
		h.handleInstructionResult(client, msg)

	case "script_result":
		// 脚本执行结果
		h.handleScriptResult(client, msg)

	case "status":
		// 状态上报
		h.handleStatusReport(client, msg)

	default:
		log.Printf("WebSocket: 未知消息类型 %s", msg.Type)
	}
}

// handleInstructionResult 处理指令执行结果
func (h *WebSocketHandler) handleInstructionResult(client *DeviceClient, msg *WSMessage) {
	var result struct {
		InstructionID string `json:"instruction_id"`
		Status        string `json:"status"` // success/failed
		Result        string `json:"result"`
		Error         string `json:"error"`
	}

	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		return
	}

	// 更新指令状态
	updates := map[string]interface{}{
		"status": result.Status,
		"result": result.Result,
	}
	if result.Status == "success" || result.Status == "failed" {
		now := time.Now()
		updates["acked_at"] = &now
	}

	model.DB.Model(&model.RealtimeInstruction{}).
		Where("id = ?", result.InstructionID).
		Updates(updates)
}

// handleScriptResult 处理脚本执行结果
func (h *WebSocketHandler) handleScriptResult(client *DeviceClient, msg *WSMessage) {
	var result struct {
		ScriptID   string `json:"script_id"`
		DeliveryID string `json:"delivery_id"`
		Status     string `json:"status"`
		Result     string `json:"result"`
		Error      string `json:"error"`
		Duration   int    `json:"duration"`
	}

	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		return
	}

	// 更新下发状态
	status := model.ScriptDeliveryStatus(result.Status)
	h.scriptService.UpdateDeliveryStatus(result.DeliveryID, status, result.Result, result.Error, result.Duration)
}

// handleStatusReport 处理状态上报
func (h *WebSocketHandler) handleStatusReport(client *DeviceClient, msg *WSMessage) {
	// 可以扩展处理设备状态上报
	log.Printf("WebSocket: 设备状态上报 machine=%s payload=%s", client.machineID, string(msg.Payload))
}

// ==================== 管理端接口 ====================

// SendInstruction 发送实时指令
func (h *WebSocketHandler) SendInstruction(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req struct {
		AppID     string `json:"app_id" binding:"required"`
		MachineID string `json:"machine_id"` // 空表示广播
		Type      string `json:"type" binding:"required"`
		Payload   string `json:"payload" binding:"required"`
		Priority  int    `json:"priority"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", req.AppID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 生成指令
	nonce, _ := crypto.GenerateNonce(16)
	timestamp := time.Now().Unix()
	expiresAt := time.Now().Add(5 * time.Minute)

	instruction := model.RealtimeInstruction{
		AppID:     req.AppID,
		DeviceID:  "",
		Type:      model.InstructionType(req.Type),
		Payload:   req.Payload,
		Priority:  req.Priority,
		Timestamp: timestamp,
		Nonce:     nonce,
		ExpiresAt: expiresAt,
		Status:    model.InstructionStatusPending,
	}

	// 签名
	signData := []byte(instruction.ID + ":" + req.Type + ":" + req.Payload + ":" + nonce)
	signature, err := crypto.Sign(app.PrivateKey, signData)
	if err != nil {
		response.ServerError(c, "签名失败")
		return
	}
	instruction.Signature = signature

	// 保存指令
	if err := model.DB.Create(&instruction).Error; err != nil {
		response.ServerError(c, "创建指令失败")
		return
	}

	// 构建消息
	msgPayload, _ := json.Marshal(map[string]interface{}{
		"id":        instruction.ID,
		"type":      req.Type,
		"payload":   json.RawMessage(req.Payload),
		"timestamp": timestamp,
		"nonce":     nonce,
		"signature": signature,
		"expires":   expiresAt.Unix(),
	})
	wsMsg := WSMessage{
		Type:    "instruction",
		ID:      instruction.ID,
		Payload: msgPayload,
	}
	msgData, _ := json.Marshal(wsMsg)

	// 发送
	var sent bool
	if req.MachineID != "" {
		// 发送给特定设备
		sent = hub.SendToDevice(req.AppID, req.MachineID, msgData)
		if sent {
			instruction.Status = model.InstructionStatusSent
			now := time.Now()
			instruction.SentAt = &now
			model.DB.Save(&instruction)
		}
	} else {
		// 广播
		hub.BroadcastToApp(req.AppID, msgData)
		sent = true
		instruction.Status = model.InstructionStatusSent
		now := time.Now()
		instruction.SentAt = &now
		model.DB.Save(&instruction)
	}

	response.Success(c, gin.H{
		"instruction_id": instruction.ID,
		"sent":           sent,
		"expires_at":     expiresAt,
	})
}

// GetOnlineDevices 获取在线设备列表
func (h *WebSocketHandler) GetOnlineDevices(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	appID := c.Param("id")

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	devices := hub.GetOnlineDevices(appID)

	// 获取设备详情
	var result []gin.H
	for _, machineID := range devices {
		var device model.Device
		if err := model.DB.First(&device, "machine_id = ? AND tenant_id = ?", machineID, tenantID).Error; err == nil {
			var conn model.DeviceConnection
			model.DB.Where("machine_id = ? AND app_id = ? AND status = ?", machineID, appID, "connected").
				Order("connected_at DESC").First(&conn)

			result = append(result, gin.H{
				"device_id":    device.ID,
				"machine_id":   machineID,
				"name":         device.DeviceName,
				"os":           device.OSType,
				"session_id":   conn.SessionID,
				"connected_at": conn.ConnectedAt,
				"last_ping_at": conn.LastPingAt,
			})
		}
	}

	response.Success(c, gin.H{
		"online_count": len(devices),
		"devices":      result,
	})
}

// GetInstructionStatus 获取指令状态
func (h *WebSocketHandler) GetInstructionStatus(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id := c.Param("id")

	var instruction model.RealtimeInstruction
	if err := model.DB.Model(&model.RealtimeInstruction{}).
		Joins("JOIN applications ON applications.id = realtime_instructions.app_id").
		Where("realtime_instructions.id = ? AND applications.tenant_id = ?", id, tenantID).
		First(&instruction).Error; err != nil {
		response.NotFound(c, "指令不存在")
		return
	}

	response.Success(c, gin.H{
		"id":         instruction.ID,
		"type":       instruction.Type,
		"payload":    instruction.Payload,
		"status":     instruction.Status,
		"sent_at":    instruction.SentAt,
		"acked_at":   instruction.AckedAt,
		"result":     instruction.Result,
		"expires_at": instruction.ExpiresAt,
		"created_at": instruction.CreatedAt,
	})
}

// ListInstructions 获取指令列表
func (h *WebSocketHandler) ListInstructions(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	appID := c.Query("app_id")

	var instructions []model.RealtimeInstruction
	query := model.DB.Model(&model.RealtimeInstruction{}).
		Joins("JOIN applications ON applications.id = realtime_instructions.app_id").
		Where("applications.tenant_id = ?", tenantID).
		Order("realtime_instructions.created_at DESC")

	if appID != "" {
		query = query.Where("realtime_instructions.app_id = ?", appID)
	}

	// 分页
	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	if ps := c.Query("page_size"); ps != "" {
		pageSize, _ = strconv.Atoi(ps)
	}
	offset := (page - 1) * pageSize

	var total int64
	query.Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&instructions)

	var result []gin.H
	for _, inst := range instructions {
		result = append(result, gin.H{
			"id":         inst.ID,
			"app_id":     inst.AppID,
			"device_id":  inst.DeviceID,
			"type":       inst.Type,
			"payload":    inst.Payload,
			"priority":   inst.Priority,
			"status":     inst.Status,
			"sent_at":    inst.SentAt,
			"acked_at":   inst.AckedAt,
			"result":     inst.Result,
			"expires_at": inst.ExpiresAt,
			"created_at": inst.CreatedAt,
		})
	}

	response.Success(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
