// Package license 脚本管理和版本下载功能
package license

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

// ScriptInfo 脚本信息
type ScriptInfo struct {
	Filename    string `json:"filename"`
	Version     string `json:"version"`
	VersionCode int    `json:"version_code"`
	FileSize    int64  `json:"file_size"`
	FileHash    string `json:"file_hash"`
	UpdatedAt   string `json:"updated_at"`
}

// ScriptVersionResponse 脚本版本响应
type ScriptVersionResponse struct {
	Scripts     []ScriptInfo `json:"scripts"`
	TotalCount  int          `json:"total_count"`
	LastUpdated string       `json:"last_updated"`
}

// ReleaseDownloadInfo 版本下载信息
type ReleaseDownloadInfo struct {
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	FileSize    int64  `json:"file_size"`
	FileHash    string `json:"file_hash"`
}

// ScriptManager 脚本管理器
type ScriptManager struct {
	client *Client
}

// NewScriptManager 创建脚本管理器
func (c *Client) NewScriptManager() *ScriptManager {
	return &ScriptManager{
		client: c,
	}
}

// GetScriptVersions 获取脚本版本信息
// 返回所有可用脚本的版本信息列表
func (m *ScriptManager) GetScriptVersions() (*ScriptVersionResponse, error) {
	params := url.Values{}
	params.Set("app_key", m.client.appKey)

	resp, err := m.client.httpClient.Get(m.client.serverURL + "/api/client/scripts/version?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		Code    int                   `json:"code"`
		Message string                `json:"message"`
		Data    ScriptVersionResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API错误: %s", result.Message)
	}

	return &result.Data, nil
}

// DownloadScript 下载指定脚本文件
// filename: 脚本文件名
// savePath: 保存路径（如果为空，返回内容而不保存）
func (m *ScriptManager) DownloadScript(filename string, savePath string) ([]byte, error) {
	params := url.Values{}
	params.Set("app_key", m.client.appKey)
	params.Set("machine_id", m.client.machineID)

	downloadURL := fmt.Sprintf("%s/api/client/scripts/%s?%s",
		m.client.serverURL, url.PathEscape(filename), params.Encode())

	resp, err := m.client.httpClient.Get(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		var errResult struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &errResult) == nil && errResult.Message != "" {
			return nil, fmt.Errorf("下载失败: %s", errResult.Message)
		}
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取内容失败: %w", err)
	}

	// 如果指定了保存路径，保存到文件
	if savePath != "" {
		// 确保目录存在
		dir := filepath.Dir(savePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建目录失败: %w", err)
		}

		if err := os.WriteFile(savePath, content, 0644); err != nil {
			return nil, fmt.Errorf("保存文件失败: %w", err)
		}
	}

	return content, nil
}

// CheckScriptUpdate 检查脚本是否有更新
// filename: 脚本文件名
// currentVersion: 当前版本号
// 返回: 是否有更新, 最新版本信息, 错误
func (m *ScriptManager) CheckScriptUpdate(filename string, currentVersionCode int) (bool, *ScriptInfo, error) {
	versions, err := m.GetScriptVersions()
	if err != nil {
		return false, nil, err
	}

	for _, script := range versions.Scripts {
		if script.Filename == filename {
			if script.VersionCode > currentVersionCode {
				return true, &script, nil
			}
			return false, &script, nil
		}
	}

	return false, nil, fmt.Errorf("脚本 %s 不存在", filename)
}

// ReleaseManager 版本发布管理器
type ReleaseManager struct {
	client *Client
}

// NewReleaseManager 创建版本发布管理器
func (c *Client) NewReleaseManager() *ReleaseManager {
	return &ReleaseManager{
		client: c,
	}
}

// DownloadRelease 下载版本文件
// filename: 文件名
// savePath: 保存路径
// progressCallback: 下载进度回调 (已下载字节数, 总字节数)
func (m *ReleaseManager) DownloadRelease(filename string, savePath string, progressCallback func(downloaded, total int64)) error {
	params := url.Values{}
	params.Set("app_key", m.client.appKey)
	params.Set("machine_id", m.client.machineID)

	downloadURL := fmt.Sprintf("%s/api/client/releases/download/%s?%s",
		m.client.serverURL, url.PathEscape(filename), params.Encode())

	resp, err := m.client.httpClient.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		var errResult struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &errResult) == nil && errResult.Message != "" {
			return fmt.Errorf("下载失败: %s", errResult.Message)
		}
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	// 确保目录存在
	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 创建文件
	file, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	// 获取文件大小
	totalSize := resp.ContentLength

	// 如果有进度回调，使用带进度的复制
	if progressCallback != nil && totalSize > 0 {
		var downloaded int64
		buf := make([]byte, 32*1024) // 32KB buffer
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				_, writeErr := file.Write(buf[:n])
				if writeErr != nil {
					return fmt.Errorf("写入文件失败: %w", writeErr)
				}
				downloaded += int64(n)
				progressCallback(downloaded, totalSize)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("读取数据失败: %w", err)
			}
		}
	} else {
		// 直接复制
		if _, err := io.Copy(file, resp.Body); err != nil {
			return fmt.Errorf("保存文件失败: %w", err)
		}
	}

	return nil
}

// GetLatestReleaseAndDownload 获取最新版本并下载
// savePath: 保存路径
// progressCallback: 下载进度回调
func (m *ReleaseManager) GetLatestReleaseAndDownload(savePath string, progressCallback func(downloaded, total int64)) (*UpdateInfo, error) {
	// 获取最新版本信息
	updateInfo, err := m.client.CheckUpdate()
	if err != nil {
		return nil, fmt.Errorf("获取版本信息失败: %w", err)
	}

	if updateInfo == nil || updateInfo.DownloadURL == "" {
		return nil, fmt.Errorf("没有可用的更新")
	}

	// 从 DownloadURL 提取文件名（忽略 query 参数）
	downloadURLParsed, err := url.Parse(updateInfo.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("无效的下载URL: %w", err)
	}
	filename := filepath.Base(downloadURLParsed.Path)
	if filename == "" || filename == "." {
		return nil, fmt.Errorf("无效的下载URL")
	}

	// 下载文件
	if err := m.DownloadRelease(filename, savePath, progressCallback); err != nil {
		return nil, err
	}

	return updateInfo, nil
}
