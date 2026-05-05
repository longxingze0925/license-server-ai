package handler

import (
	"encoding/csv"
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"time"

	"github.com/gin-gonic/gin"
)

type ExportHandler struct{}

func NewExportHandler() *ExportHandler {
	return &ExportHandler{}
}

// ExportLicenses 导出授权数据
func (h *ExportHandler) ExportLicenses(c *gin.Context) {
	appID := c.Query("app_id")
	status := c.Query("status")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := scopedLicenseQuery(c).Preload("Application")

	if appID != "" {
		query = query.Where("licenses.app_id = ?", appID)
	}
	if status != "" {
		query = query.Where("licenses.status = ?", status)
	}
	var ok bool
	query, ok = applyCreatedAtDateRangeForColumn(c, query, "licenses.created_at", startDate, endDate)
	if !ok {
		return
	}

	var licenses []model.License
	if err := query.Order("licenses.created_at DESC").Find(&licenses).Error; err != nil {
		response.ServerError(c, "查询授权数据失败")
		return
	}

	// 设置响应头
	filename := fmt.Sprintf("licenses_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Transfer-Encoding", "binary")

	// 写入 BOM 以支持 Excel 中文显示
	if !writeCSVBom(c) {
		return
	}

	writer := csv.NewWriter(c.Writer)

	// 写入表头
	if !writeCSVRow(c, writer, []string{
		"授权码", "应用名称", "类型", "状态", "有效期(天)", "最大设备数",
		"激活时间", "到期时间", "创建时间",
	}) {
		return
	}

	// 写入数据
	for _, license := range licenses {
		appName := ""
		if license.Application != nil {
			appName = license.Application.Name
		}
		activatedAt := ""
		if license.ActivatedAt != nil {
			activatedAt = license.ActivatedAt.Format("2006-01-02 15:04:05")
		}
		expireAt := ""
		if license.ExpireAt != nil {
			expireAt = license.ExpireAt.Format("2006-01-02 15:04:05")
		}

		if !writeCSVRow(c, writer, []string{
			license.LicenseKey,
			appName,
			string(license.Type),
			string(license.Status),
			fmt.Sprintf("%d", license.DurationDays),
			fmt.Sprintf("%d", license.MaxDevices),
			activatedAt,
			expireAt,
			license.CreatedAt.Format("2006-01-02 15:04:05"),
		}) {
			return
		}
	}
	flushCSV(c, writer)
}

// ExportDevices 导出设备数据
func (h *ExportHandler) ExportDevices(c *gin.Context) {
	status := c.Query("status")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := scopedDeviceQuery(c)

	if status != "" {
		query = query.Where("devices.status = ?", status)
	}
	var ok bool
	query, ok = applyCreatedAtDateRangeForColumn(c, query, "devices.created_at", startDate, endDate)
	if !ok {
		return
	}

	var devices []model.Device
	if err := query.Order("devices.created_at DESC").Find(&devices).Error; err != nil {
		response.ServerError(c, "查询设备数据失败")
		return
	}

	// 设置响应头
	filename := fmt.Sprintf("devices_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Transfer-Encoding", "binary")

	if !writeCSVBom(c) {
		return
	}

	writer := csv.NewWriter(c.Writer)

	// 写入表头
	if !writeCSVRow(c, writer, []string{
		"设备名称", "机器码", "主机名", "操作系统", "系统版本",
		"IP地址", "IP归属地", "状态", "最后心跳", "绑定时间",
	}) {
		return
	}

	// 写入数据
	for _, device := range devices {
		lastHeartbeat := ""
		if device.LastHeartbeatAt != nil {
			lastHeartbeat = device.LastHeartbeatAt.Format("2006-01-02 15:04:05")
		}
		location := device.IPCountry + " " + device.IPCity

		if !writeCSVRow(c, writer, []string{
			device.DeviceName,
			device.MachineID,
			device.Hostname,
			device.OSType,
			device.OSVersion,
			device.IPAddress,
			location,
			string(device.Status),
			lastHeartbeat,
			device.CreatedAt.Format("2006-01-02 15:04:05"),
		}) {
			return
		}
	}
	flushCSV(c, writer)
}

// ExportCustomers 导出客户数据
func (h *ExportHandler) ExportCustomers(c *gin.Context) {
	status := c.Query("status")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := scopedCustomerQuery(c)

	if status != "" {
		query = query.Where("status = ?", status)
	}
	var ok bool
	query, ok = applyCreatedAtDateRangeForColumn(c, query, "customers.created_at", startDate, endDate)
	if !ok {
		return
	}

	var customers []model.Customer
	if err := query.Order("customers.created_at DESC").Find(&customers).Error; err != nil {
		response.ServerError(c, "查询客户数据失败")
		return
	}

	// 设置响应头
	filename := fmt.Sprintf("customers_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Transfer-Encoding", "binary")

	if !writeCSVBom(c) {
		return
	}

	writer := csv.NewWriter(c.Writer)

	// 写入表头
	if !writeCSVRow(c, writer, []string{
		"姓名", "邮箱", "电话", "公司", "状态", "最后登录", "注册时间",
	}) {
		return
	}

	// 写入数据
	for _, customer := range customers {
		lastLogin := ""
		if customer.LastLoginAt != nil {
			lastLogin = customer.LastLoginAt.Format("2006-01-02 15:04:05")
		}

		if !writeCSVRow(c, writer, []string{
			customer.Name,
			customer.Email,
			customer.Phone,
			customer.Company,
			string(customer.Status),
			lastLogin,
			customer.CreatedAt.Format("2006-01-02 15:04:05"),
		}) {
			return
		}
	}
	flushCSV(c, writer)
}

// ExportAuditLogs 导出审计日志
func (h *ExportHandler) ExportAuditLogs(c *gin.Context) {
	if current, ok := c.Get("team_member"); ok {
		if member, ok := current.(model.TeamMember); !ok || !member.HasPermission("audit:read") {
			response.Forbidden(c, "没有导出审计日志权限")
			return
		}
	} else {
		response.Forbidden(c, "没有导出审计日志权限")
		return
	}

	tenantID := middleware.GetTenantID(c)
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := model.DB.Model(&model.AuditLog{}).Where("tenant_id = ?", tenantID)

	var ok bool
	query, ok = applyCreatedAtDateRangeForColumn(c, query, "created_at", startDate, endDate)
	if !ok {
		return
	}

	var logs []model.AuditLog
	if err := query.Order("created_at DESC").Limit(10000).Find(&logs).Error; err != nil {
		response.ServerError(c, "查询审计日志失败")
		return
	}

	// 设置响应头
	filename := fmt.Sprintf("audit_logs_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Transfer-Encoding", "binary")

	if !writeCSVBom(c) {
		return
	}

	writer := csv.NewWriter(c.Writer)

	// 写入表头
	if !writeCSVRow(c, writer, []string{
		"时间", "用户邮箱", "操作", "资源", "描述", "IP地址", "状态码", "耗时(ms)",
	}) {
		return
	}

	// 写入数据
	for _, log := range logs {
		if !writeCSVRow(c, writer, []string{
			log.CreatedAt.Format("2006-01-02 15:04:05"),
			log.UserEmail,
			log.Action,
			log.Resource,
			log.Description,
			log.IPAddress,
			fmt.Sprintf("%d", log.ResponseCode),
			fmt.Sprintf("%d", log.Duration),
		}) {
			return
		}
	}
	flushCSV(c, writer)
}

// GetExportFormats 获取支持的导出格式
func (h *ExportHandler) GetExportFormats(c *gin.Context) {
	response.Success(c, gin.H{
		"formats": []gin.H{
			{"key": "csv", "name": "CSV", "description": "逗号分隔值文件，可用Excel打开"},
		},
		"resources": []gin.H{
			{"key": "licenses", "name": "授权数据"},
			{"key": "devices", "name": "设备数据"},
			{"key": "customers", "name": "客户数据"},
			{"key": "audit_logs", "name": "审计日志"},
		},
	})
}

func writeCSVBom(c *gin.Context) bool {
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		_ = c.Error(err)
		return false
	}
	return true
}

func writeCSVRow(c *gin.Context, writer *csv.Writer, row []string) bool {
	if err := writer.Write(row); err != nil {
		_ = c.Error(err)
		return false
	}
	return true
}

func flushCSV(c *gin.Context, writer *csv.Writer) {
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = c.Error(err)
	}
}
