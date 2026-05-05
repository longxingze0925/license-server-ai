package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type wsMessage struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:8081/api", "API base URL")
	wsURL := flag.String("ws-url", "ws://127.0.0.1:8081/api/client/ws", "client WebSocket URL")
	adminEmail := flag.String("admin-email", "admin@example.com", "admin email")
	adminPassword := flag.String("admin-password", "admin123", "admin password")
	flag.Parse()

	if err := run(strings.TrimRight(*baseURL, "/"), *wsURL, *adminEmail, *adminPassword); err != nil {
		fmt.Fprintln(os.Stderr, "ws smoke failed:", err)
		os.Exit(1)
	}
}

func run(baseURL, wsURL, adminEmail, adminPassword string) error {
	stamp := time.Now().Format("20060102150405")

	var login struct {
		Token string `json:"token"`
		User  struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := api(baseURL, http.MethodPost, "/auth/login", map[string]any{
		"email":    adminEmail,
		"password": adminPassword,
	}, "", &login); err != nil {
		return err
	}
	token := login.Token

	var appID, customerID string
	defer func() {
		if customerID != "" {
			_ = api(baseURL, http.MethodDelete, "/admin/customers/"+customerID, nil, token, nil)
		}
		if appID != "" {
			_ = api(baseURL, http.MethodDelete, "/admin/apps/"+appID, nil, token, nil)
		}
	}()

	var app struct {
		ID     string `json:"id"`
		AppKey string `json:"app_key"`
	}
	if err := api(baseURL, http.MethodPost, "/admin/apps", map[string]any{
		"name":                "codex-ws-realtest-" + stamp,
		"max_devices_default": 2,
		"features":            []string{"ws"},
	}, token, &app); err != nil {
		return err
	}
	appID = app.ID

	email := "codex-ws-realtest-" + stamp + "@example.test"
	password := "SmokePass1!"
	var customer struct {
		ID string `json:"id"`
	}
	if err := api(baseURL, http.MethodPost, "/admin/customers", map[string]any{
		"email":    email,
		"password": password,
		"name":     "Codex WS Test",
	}, token, &customer); err != nil {
		return err
	}
	customerID = customer.ID

	if err := api(baseURL, http.MethodPost, "/admin/subscriptions", map[string]any{
		"customer_id": customerID,
		"app_id":      appID,
		"plan_type":   "pro",
		"max_devices": 2,
		"days":        7,
		"features":    []string{"ws"},
	}, token, nil); err != nil {
		return err
	}

	machineID := "codex-ws-machine-" + stamp
	var clientLogin struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := api(baseURL, http.MethodPost, "/client/auth/login", map[string]any{
		"app_key":         app.AppKey,
		"email":           email,
		"password":        password,
		"password_hashed": false,
		"machine_id":      machineID,
		"device_info": map[string]any{
			"name":        "Codex WS Device",
			"hostname":    "codex-ws-host",
			"os":          "Windows",
			"os_version":  "test",
			"app_version": "1.0.0",
		},
	}, "", &clientLogin); err != nil {
		return err
	}
	if clientLogin.AccessToken == "" {
		return errors.New("client login did not return access_token")
	}
	tokenType := strings.TrimSpace(clientLogin.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin":        []string{"http://127.0.0.1:3000"},
		"Authorization": []string{tokenType + " " + clientLogin.AccessToken},
	})
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close()

	authPayload, _ := json.Marshal(map[string]string{
		"app_key":    app.AppKey,
		"machine_id": machineID,
	})
	if err := conn.WriteJSON(wsMessage{Type: "auth", Payload: authPayload}); err != nil {
		return fmt.Errorf("send websocket auth: %w", err)
	}

	var authMsg wsMessage
	if err := readWS(conn, &authMsg); err != nil {
		return err
	}
	if authMsg.Type != "auth_ok" {
		return fmt.Errorf("unexpected websocket auth response: %s", mustJSON(authMsg))
	}

	var online struct {
		OnlineCount int `json:"online_count"`
	}
	if err := waitFor(5*time.Second, 200*time.Millisecond, func() (bool, error) {
		if err := api(baseURL, http.MethodGet, "/admin/apps/"+appID+"/online-devices", nil, token, &online); err != nil {
			return false, err
		}
		return online.OnlineCount >= 1, nil
	}); err != nil {
		return fmt.Errorf("wait for online device: %w", err)
	}

	var sent struct {
		InstructionID string `json:"instruction_id"`
		Sent          bool   `json:"sent"`
	}
	if err := api(baseURL, http.MethodPost, "/admin/instructions/send", map[string]any{
		"app_id":     appID,
		"machine_id": machineID,
		"type":       "get_status",
		"payload":    `{"probe":"codex"}`,
	}, token, &sent); err != nil {
		return err
	}
	if !sent.Sent || sent.InstructionID == "" {
		return fmt.Errorf("instruction was not sent: %s", mustJSON(sent))
	}

	var instruction wsMessage
	if err := readWS(conn, &instruction); err != nil {
		return err
	}
	if instruction.Type != "instruction" || instruction.ID != sent.InstructionID {
		return fmt.Errorf("unexpected instruction message: %s", mustJSON(instruction))
	}

	resultPayload, _ := json.Marshal(map[string]any{
		"instruction_id": sent.InstructionID,
		"status":         "executed",
		"result": map[string]any{
			"ok":     true,
			"source": "go-ws-smoke",
		},
	})
	if err := conn.WriteJSON(wsMessage{Type: "instruction_result", Payload: resultPayload}); err != nil {
		return fmt.Errorf("send instruction result: %w", err)
	}

	var detail struct {
		Status        string `json:"status"`
		ResultCount   int64  `json:"result_count"`
		ExecutedCount int64  `json:"executed_count"`
	}
	if err := waitFor(5*time.Second, 200*time.Millisecond, func() (bool, error) {
		if err := api(baseURL, http.MethodGet, "/admin/instructions/"+sent.InstructionID, nil, token, &detail); err != nil {
			return false, err
		}
		return detail.Status == "executed" && detail.ResultCount >= 1 && detail.ExecutedCount >= 1, nil
	}); err != nil {
		return fmt.Errorf("wait for instruction result: %w; last detail: %s", err, mustJSON(detail))
	}

	fmt.Println(mustJSON(map[string]any{
		"admin_role":         login.User.Role,
		"ws_auth_type":       authMsg.Type,
		"online_count":       online.OnlineCount,
		"instruction_id":     sent.InstructionID,
		"instruction_status": detail.Status,
		"result_count":       detail.ResultCount,
		"executed_count":     detail.ExecutedCount,
		"temp_app_id":        appID,
		"temp_customer_id":   customerID,
	}))
	return nil
}

func api(baseURL, method, path string, body any, token string, out any) error {
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, baseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s returned HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if len(raw) == 0 || out == nil {
		return nil
	}

	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.Code != 0 {
		if envelope.Message == "" {
			envelope.Message = string(raw)
		}
		return errors.New(envelope.Message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

func readWS(conn *websocket.Conn, out any) error {
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if err := conn.ReadJSON(out); err != nil {
		return fmt.Errorf("read websocket message: %w", err)
	}
	return nil
}

func waitFor(timeout, interval time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, err := check()
		if err != nil {
			lastErr = err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("condition not met within %s", timeout)
		}
		time.Sleep(interval)
	}
}

func mustJSON(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(raw)
}
