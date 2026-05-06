package handler

import (
	"encoding/json"
	"license-server/internal/config"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/crypto"
	"license-server/internal/pkg/response"
	"license-server/internal/pkg/utils"
	"license-server/internal/service"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm/clause"
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
				go h.recordConnection(old, "disconnected")
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
		if err := model.DB.Create(&conn).Error; err != nil {
			log.Printf("记录设备连接失败: session=%s err=%v", client.sessionID, err)
		}
	} else {
		if err := model.DB.Model(&model.DeviceConnection{}).
			Where("session_id = ?", client.sessionID).
			Update("status", status).Error; err != nil {
			log.Printf("更新设备连接状态失败: session=%s status=%s err=%v", client.sessionID, status, err)
		}
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
func (h *WebSocketHub) BroadcastToApp(appID string, message []byte) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sentCount := 0
	if appClients, ok := h.clients[appID]; ok {
		for _, client := range appClients {
			select {
			case client.send <- message:
				sentCount++
			default:
			}
		}
	}
	return sentCount
}

func (h *WebSocketHub) NotifyTaskStatus(task *model.GenerationTask) {
	if h == nil || task == nil || task.AppID == "" {
		return
	}
	eventType := "task_update"
	switch task.Status {
	case model.GenerationSucceeded:
		eventType = "task_succeeded"
	case model.GenerationFailed:
		eventType = "task_failed"
	}
	payload, _ := json.Marshal(gin.H{
		"task_id":         task.ID,
		"status":          task.Status,
		"progress":        task.Progress,
		"result_json":     task.ResultJSON,
		"error_json":      task.ErrorJSON,
		"upstream_status": task.UpstreamStatus,
		"upstream_error":  task.UpstreamError,
		"refund_status":   task.RefundStatus,
		"refund_amount":   task.RefundAmount,
		"refunded_at":     task.RefundedAt,
		"completed_at":    task.CompletedAt,
	})
	message, _ := json.Marshal(WSMessage{
		Type:    eventType,
		ID:      task.ID,
		Payload: payload,
	})
	h.BroadcastToApp(task.AppID, message)
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
	if err := model.DB.First(&app, "id = ? AND app_key = ? AND status = ?",
		middleware.GetClientAppID(c), authPayload.AppKey, model.AppStatusActive).Error; err != nil {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"无效的应用"}`)})
		conn.Close()
		return
	}
	if authPayload.MachineID != middleware.GetClientMachineID(c) || app.TenantID != middleware.GetClientTenantID(c) {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"客户端会话不匹配"}`)})
		conn.Close()
		return
	}

	// 验证设备
	device, err := loadActiveDeviceForApp(authPayload.MachineID, &app)
	if err != nil {
		payload, _ := json.Marshal(map[string]string{"message": err.Error()})
		conn.WriteJSON(WSMessage{Type: "error", Payload: payload})
		conn.Close()
		return
	}
	if sessionDeviceID := middleware.GetClientDeviceID(c); sessionDeviceID != "" && device.ID != sessionDeviceID {
		conn.WriteJSON(WSMessage{Type: "error", Payload: json.RawMessage(`{"message":"客户端会话与设备不匹配"}`)})
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
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
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

func normalizeInstructionResultStatus(status string) (model.InstructionStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return model.InstructionStatusExecuted, true
	case string(model.InstructionStatusAcked):
		return model.InstructionStatusAcked, true
	case string(model.InstructionStatusExecuted):
		return model.InstructionStatusExecuted, true
	case string(model.InstructionStatusFailed):
		return model.InstructionStatusFailed, true
	default:
		return "", false
	}
}

func isValidInstructionType(instructionType model.InstructionType) bool {
	switch instructionType {
	case model.InstructionTypeClick,
		model.InstructionTypeDoubleClick,
		model.InstructionTypeRightClick,
		model.InstructionTypeInput,
		model.InstructionTypeKeyPress,
		model.InstructionTypeScroll,
		model.InstructionTypeScreenshot,
		model.InstructionTypeFindImage,
		model.InstructionTypeOCR,
		model.InstructionTypeWait,
		model.InstructionTypeCondition,
		model.InstructionTypeExecScript,
		model.InstructionTypeGetStatus,
		model.InstructionTypeRestart,
		model.InstructionTypeShutdown,
		model.InstructionTypeCustom:
		return true
	default:
		return false
	}
}

// handleInstructionResult 处理指令执行结果
func (h *WebSocketHandler) handleInstructionResult(client *DeviceClient, msg *WSMessage) {
	var result struct {
		InstructionID string          `json:"instruction_id"`
		Status        string          `json:"status"` // success/failed
		Result        json.RawMessage `json:"result"`
		Error         string          `json:"error"`
	}

	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		return
	}
	if strings.TrimSpace(result.InstructionID) == "" {
		log.Printf("WebSocket: instruction_result 缺少 instruction_id machine=%s", client.machineID)
		return
	}

	status, ok := normalizeInstructionResultStatus(result.Status)
	if !ok {
		log.Printf("WebSocket: instruction_result 状态非法 instruction=%s status=%s machine=%s", result.InstructionID, result.Status, client.machineID)
		return
	}

	var instruction model.RealtimeInstruction
	if err := model.DB.Where("id = ? AND app_id = ?", result.InstructionID, client.appID).
		Where("(device_id = ? OR device_id = '')", client.deviceID).
		First(&instruction).Error; err != nil {
		log.Printf("WebSocket: 拒绝无归属指令回执 instruction=%s app=%s device=%s machine=%s", result.InstructionID, client.appID, client.deviceID, client.machineID)
		return
	}
	if time.Now().After(instruction.ExpiresAt) {
		model.DB.Model(&model.RealtimeInstruction{}).
			Where("id = ? AND status IN ?", instruction.ID, []model.InstructionStatus{model.InstructionStatusPending, model.InstructionStatusSent}).
			Update("status", model.InstructionStatusExpired)
		log.Printf("WebSocket: 拒绝过期指令回执 instruction=%s app=%s device=%s machine=%s", result.InstructionID, client.appID, client.deviceID, client.machineID)
		return
	}

	resultText := normalizeInstructionResultText(result.Result, result.Error)

	now := time.Now()
	instructionResult := model.RealtimeInstructionResult{
		InstructionID: instruction.ID,
		AppID:         client.appID,
		DeviceID:      client.deviceID,
		MachineID:     client.machineID,
		Status:        status,
		Result:        resultText,
		AckedAt:       &now,
	}
	if err := model.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "instruction_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"app_id",
			"machine_id",
			"status",
			"result",
			"acked_at",
			"updated_at",
		}),
	}).Create(&instructionResult).Error; err != nil {
		log.Printf("WebSocket: 保存指令设备结果失败 instruction=%s device=%s err=%v", instruction.ID, client.deviceID, err)
		return
	}

	if instruction.DeviceID == "" {
		return
	}

	// 指定设备指令保留主表状态，广播指令结果进入 realtime_instruction_results。
	updates := map[string]interface{}{
		"status": status,
	}
	if resultText != "" {
		updates["result"] = resultText
	}
	if status == model.InstructionStatusAcked || status == model.InstructionStatusExecuted || status == model.InstructionStatusFailed {
		updates["acked_at"] = &now
	}

	tx := model.DB.Model(&model.RealtimeInstruction{}).
		Where("id = ? AND app_id = ?", result.InstructionID, client.appID).
		Where("device_id = ?", client.deviceID).
		Updates(updates)
	if tx.Error != nil {
		log.Printf("WebSocket: 更新指令结果失败 instruction=%s machine=%s err=%v", result.InstructionID, client.machineID, tx.Error)
		return
	}
	if tx.RowsAffected == 0 {
		log.Printf("WebSocket: 拒绝无归属指令回执 instruction=%s app=%s device=%s machine=%s", result.InstructionID, client.appID, client.deviceID, client.machineID)
	}
}

func normalizeInstructionResultText(result json.RawMessage, errorText string) string {
	if len(result) == 0 || string(result) == "null" {
		return errorText
	}
	var text string
	if err := json.Unmarshal(result, &text); err == nil {
		if text != "" {
			return text
		}
		return errorText
	}
	var compacted json.RawMessage
	if err := json.Unmarshal(result, &compacted); err == nil {
		return string(compacted)
	}
	return string(result)
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
	if strings.TrimSpace(result.DeliveryID) == "" {
		log.Printf("WebSocket: script_result 缺少 delivery_id machine=%s", client.machineID)
		return
	}

	// 更新下发状态
	status, ok := normalizeClientScriptDeliveryStatus(result.Status)
	if !ok {
		log.Printf("WebSocket: script_result 状态非法 delivery=%s status=%s machine=%s", result.DeliveryID, result.Status, client.machineID)
		return
	}
	if err := h.scriptService.UpdateDeliveryStatusForDevice(
		result.DeliveryID,
		client.appID,
		client.deviceID,
		client.machineID,
		status,
		result.Result,
		result.Error,
		result.Duration,
	); err != nil {
		log.Printf("WebSocket: 更新脚本回执失败 delivery=%s app=%s device=%s machine=%s err=%v", result.DeliveryID, client.appID, client.deviceID, client.machineID, err)
	}
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
	req.MachineID = strings.TrimSpace(req.MachineID)
	req.Payload = strings.TrimSpace(req.Payload)

	// 验证应用
	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", req.AppID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	instructionType := model.InstructionType(strings.TrimSpace(req.Type))
	if !isValidInstructionType(instructionType) {
		response.BadRequest(c, "不支持的指令类型")
		return
	}
	if !json.Valid([]byte(req.Payload)) {
		response.BadRequest(c, "指令内容必须是合法 JSON")
		return
	}

	deviceID := ""
	if req.MachineID != "" {
		device, err := loadActiveDeviceForApp(req.MachineID, &app)
		if err != nil {
			response.Error(c, 403, err.Error())
			return
		}
		deviceID = device.ID
	}

	// 生成指令
	instructionID := utils.GenerateUUID()
	nonce, _ := crypto.GenerateNonce(16)
	timestamp := time.Now().Unix()
	expiresAt := time.Now().Add(5 * time.Minute)

	instruction := model.RealtimeInstruction{
		BaseModel: model.BaseModel{ID: instructionID},
		AppID:     req.AppID,
		DeviceID:  deviceID,
		Type:      instructionType,
		Payload:   req.Payload,
		Priority:  req.Priority,
		Timestamp: timestamp,
		Nonce:     nonce,
		ExpiresAt: expiresAt,
		Status:    model.InstructionStatusPending,
	}

	// 签名
	signData := []byte(instructionID + ":" + string(instructionType) + ":" + req.Payload + ":" + nonce)
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
		"id":          instruction.ID,
		"type":        instructionType,
		"payload":     json.RawMessage(req.Payload),
		"payload_raw": req.Payload,
		"timestamp":   timestamp,
		"nonce":       nonce,
		"signature":   signature,
		"expires":     expiresAt.Unix(),
	})
	wsMsg := WSMessage{
		Type:    "instruction",
		ID:      instruction.ID,
		Payload: msgPayload,
	}
	msgData, _ := json.Marshal(wsMsg)

	// 发送
	var sent bool
	sentCount := 0
	if req.MachineID != "" {
		// 发送给特定设备
		sent = hub.SendToDevice(req.AppID, req.MachineID, msgData)
		if sent {
			sentCount = 1
		}
		if sent {
			instruction.Status = model.InstructionStatusSent
			now := time.Now()
			instruction.SentAt = &now
			if err := model.DB.Save(&instruction).Error; err != nil {
				response.ServerError(c, "更新指令发送状态失败: "+err.Error())
				return
			}
		}
	} else {
		// 广播
		sentCount = hub.BroadcastToApp(req.AppID, msgData)
		sent = sentCount > 0
		if sent {
			instruction.Status = model.InstructionStatusSent
			now := time.Now()
			instruction.SentAt = &now
			if err := model.DB.Save(&instruction).Error; err != nil {
				response.ServerError(c, "更新指令发送状态失败: "+err.Error())
				return
			}
		}
	}

	response.Success(c, gin.H{
		"instruction_id": instruction.ID,
		"sent":           sent,
		"sent_count":     sentCount,
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
		if device, err := loadActiveDeviceForApp(machineID, &app); err == nil {
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

func getInstructionResultSummary(instructionID string) (int64, int64, int64, int64) {
	var resultCount int64
	var ackedCount int64
	var executedCount int64
	var failedCount int64
	model.DB.Model(&model.RealtimeInstructionResult{}).Where("instruction_id = ?", instructionID).Count(&resultCount)
	model.DB.Model(&model.RealtimeInstructionResult{}).Where("instruction_id = ? AND status = ?", instructionID, model.InstructionStatusAcked).Count(&ackedCount)
	model.DB.Model(&model.RealtimeInstructionResult{}).Where("instruction_id = ? AND status = ?", instructionID, model.InstructionStatusExecuted).Count(&executedCount)
	model.DB.Model(&model.RealtimeInstructionResult{}).Where("instruction_id = ? AND status = ?", instructionID, model.InstructionStatusFailed).Count(&failedCount)
	return resultCount, ackedCount, executedCount, failedCount
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

	var instructionResults []model.RealtimeInstructionResult
	model.DB.Where("instruction_id = ?", instruction.ID).Order("created_at ASC").Find(&instructionResults)
	results := make([]gin.H, 0, len(instructionResults))
	for _, item := range instructionResults {
		results = append(results, gin.H{
			"id":         item.ID,
			"device_id":  item.DeviceID,
			"machine_id": item.MachineID,
			"status":     item.Status,
			"result":     item.Result,
			"acked_at":   item.AckedAt,
			"created_at": item.CreatedAt,
			"updated_at": item.UpdatedAt,
		})
	}
	resultCount, ackedCount, executedCount, failedCount := getInstructionResultSummary(instruction.ID)

	response.Success(c, gin.H{
		"id":             instruction.ID,
		"type":           instruction.Type,
		"payload":        instruction.Payload,
		"status":         instruction.Status,
		"sent_at":        instruction.SentAt,
		"acked_at":       instruction.AckedAt,
		"result":         instruction.Result,
		"results":        results,
		"result_count":   resultCount,
		"acked_count":    ackedCount,
		"executed_count": executedCount,
		"failed_count":   failedCount,
		"expires_at":     instruction.ExpiresAt,
		"created_at":     instruction.CreatedAt,
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
	page, pageSize := parsePageParams(c, 20, 100)
	offset := (page - 1) * pageSize

	var total int64
	query.Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&instructions)

	var result []gin.H
	for _, inst := range instructions {
		resultCount, ackedCount, executedCount, failedCount := getInstructionResultSummary(inst.ID)
		result = append(result, gin.H{
			"id":             inst.ID,
			"app_id":         inst.AppID,
			"device_id":      inst.DeviceID,
			"type":           inst.Type,
			"payload":        inst.Payload,
			"priority":       inst.Priority,
			"status":         inst.Status,
			"sent_at":        inst.SentAt,
			"acked_at":       inst.AckedAt,
			"result":         inst.Result,
			"result_count":   resultCount,
			"acked_count":    ackedCount,
			"executed_count": executedCount,
			"failed_count":   failedCount,
			"expires_at":     inst.ExpiresAt,
			"created_at":     inst.CreatedAt,
		})
	}

	response.Success(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
