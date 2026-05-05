// Package license 脚本管理和版本下载功能
package license

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	Filename      string `json:"filename"`
	DownloadURL   string `json:"download_url"`
	FileSize      int64  `json:"file_size"`
	FileHash      string `json:"file_hash"`
	FileSignature string `json:"file_signature"`
	SignatureAlg  string `json:"signature_alg"`
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
	data, err := m.client.requestWithClientSession("GET", "/scripts/version", nil)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化响应失败: %w", err)
	}

	var versions ScriptVersionResponse
	if err := json.Unmarshal(dataBytes, &versions); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &versions, nil
}

// DownloadScript 下载指定脚本文件
// filename: 脚本文件名
// savePath: 保存路径（如果为空，返回内容而不保存）
func (m *ScriptManager) DownloadScript(filename string, savePath string) ([]byte, error) {
	downloadURL := fmt.Sprintf("%s/api/client/scripts/%s", m.client.serverURL, url.PathEscape(filename))

	resp, err := m.client.requestRawWithClientSession(http.MethodGet, downloadURL, nil)
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
	downloadURL := fmt.Sprintf("%s/api/client/releases/download/%s", m.client.serverURL, url.PathEscape(filename))

	resp, err := m.client.requestRawWithClientSession(http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	return downloadResponseToFile(resp, savePath, progressCallback)
}

func downloadResponseToFile(resp *http.Response, savePath string, progressCallback func(downloaded, total int64)) error {
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

	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	tmpPath := savePath + ".part"
	_ = os.Remove(tmpPath)
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}

	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	totalSize := resp.ContentLength
	if progressCallback != nil && totalSize > 0 {
		var downloaded int64
		buf := make([]byte, 32*1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := file.Write(buf[:n]); writeErr != nil {
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
		if err := file.Close(); err != nil {
			return fmt.Errorf("关闭文件失败: %w", err)
		}
		if err := replaceFile(tmpPath, savePath); err != nil {
			return fmt.Errorf("保存文件失败: %w", err)
		}
		cleanup = false
		return nil
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("保存文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭文件失败: %w", err)
	}
	if err := replaceFile(tmpPath, savePath); err != nil {
		return fmt.Errorf("保存文件失败: %w", err)
	}
	cleanup = false
	return nil
}

func replaceFile(tmpPath, finalPath string) error {
	if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

func downloadURLToFile(httpClient *http.Client, downloadURL string, savePath string, progressCallback func(downloaded, total int64)) error {
	resp, err := httpClient.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	return downloadResponseToFile(resp, savePath, progressCallback)
}

func isDownloadAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 401") || strings.Contains(msg, "HTTP 403") || strings.Contains(msg, "下载令牌")
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

	downloadURL := updateInfo.DownloadURL
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		downloadURL = m.client.serverURL + updateInfo.DownloadURL
	}
	if err := downloadURLToFile(m.client.GetHTTPClient(), downloadURL, savePath, progressCallback); err != nil {
		if !isDownloadAuthError(err) {
			return nil, err
		}
		refreshed, refreshErr := m.client.CheckUpdate()
		if refreshErr != nil {
			return nil, fmt.Errorf("下载链接已过期，重新获取版本信息失败: %w", refreshErr)
		}
		if refreshed == nil || refreshed.DownloadURL == "" {
			return nil, fmt.Errorf("下载链接已过期，未找到可用的新链接")
		}
		updateInfo = refreshed
		downloadURL = updateInfo.DownloadURL
		if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
			downloadURL = m.client.serverURL + updateInfo.DownloadURL
		}
		if err := downloadURLToFile(m.client.GetHTTPClient(), downloadURL, savePath, progressCallback); err != nil {
			return nil, err
		}
	}

	actualHash, actualSize, err := hashFileSHA256(savePath)
	if err != nil {
		return nil, fmt.Errorf("计算文件哈希失败: %w", err)
	}
	if updateInfo.FileHash != "" && !strings.EqualFold(updateInfo.FileHash, actualHash) {
		return nil, fmt.Errorf("文件校验失败: 期望 %s, 实际 %s", updateInfo.FileHash, actualHash)
	}
	if err := m.client.verifyDownloadedFileSignature(actualHash, actualSize, updateInfo.FileSignature, updateInfo.SignatureAlg); err != nil {
		return nil, err
	}

	return updateInfo, nil
}
