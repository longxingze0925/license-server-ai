package handler

import (
	"fmt"
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"
	"time"

	"github.com/gin-gonic/gin"
)

type StatisticsHandler struct{}

func NewStatisticsHandler() *StatisticsHandler {
	return &StatisticsHandler{}
}

func localDayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func parseStatsDateRange(c *gin.Context, defaultStart time.Time) (time.Time, time.Time, error) {
	start := defaultStart
	end := time.Now()

	if raw := c.Query("start_date"); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("start_date 格式错误，应为 YYYY-MM-DD")
		}
		start = parsed
	}
	if raw := c.Query("end_date"); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("end_date 格式错误，应为 YYYY-MM-DD")
		}
		end = parsed.AddDate(0, 0, 1)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end_date 不能早于 start_date")
	}
	return start, end, nil
}

// Dashboard 仪表盘数据
func (h *StatisticsHandler) Dashboard(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	role, _ := c.Get("role")

	// 只读用户只显示部分统计卡片，但数据不按用户过滤
	isViewer := role == "viewer"

	// 客户统计
	var totalCustomers int64
	scopedCustomerQuery(c).Count(&totalCustomers)

	// 应用统计
	var totalApps int64
	model.DB.Model(&model.Application{}).Where("tenant_id = ?", tenantID).Count(&totalApps)

	// 授权统计
	var totalLicenses int64
	scopedLicenseQuery(c).Count(&totalLicenses)

	var activeLicenses int64
	scopedLicenseQuery(c).Where("licenses.status = ?", model.LicenseStatusActive).Count(&activeLicenses)

	var pendingLicenses int64
	scopedLicenseQuery(c).Where("licenses.status = ?", model.LicenseStatusPending).Count(&pendingLicenses)

	var expiredLicenses int64
	scopedLicenseQuery(c).Where("licenses.status = ?", model.LicenseStatusExpired).Count(&expiredLicenses)

	// 订阅统计
	var totalSubscriptions int64
	scopedSubscriptionQuery(c).Count(&totalSubscriptions)

	var activeSubscriptions int64
	scopedSubscriptionQuery(c).Where("subscriptions.status = ?", model.SubscriptionStatusActive).Count(&activeSubscriptions)

	// 设备统计
	var totalDevices int64
	scopedDeviceQuery(c).Count(&totalDevices)

	var activeDevices int64
	oneDayAgo := time.Now().Add(-24 * time.Hour)
	scopedDeviceQuery(c).Where("devices.last_active_at > ?", oneDayAgo).Count(&activeDevices)

	// 今日新增
	today := localDayStart(time.Now())

	var todayCustomers int64
	scopedCustomerQuery(c).Where("customers.created_at >= ?", today).Count(&todayCustomers)

	var todayLicenses int64
	scopedLicenseQuery(c).Where("licenses.created_at >= ?", today).Count(&todayLicenses)

	result := gin.H{
		"licenses": gin.H{
			"total":     totalLicenses,
			"active":    activeLicenses,
			"pending":   pendingLicenses,
			"expired":   expiredLicenses,
			"today_new": todayLicenses,
		},
		"subscriptions": gin.H{
			"total":  totalSubscriptions,
			"active": activeSubscriptions,
		},
		"devices": gin.H{
			"total":  totalDevices,
			"active": activeDevices,
		},
	}

	// 非只读用户显示更多统计
	if !isViewer {
		result["customers"] = gin.H{
			"total":     totalCustomers,
			"today_new": todayCustomers,
		}
		result["applications"] = gin.H{
			"total": totalApps,
		}
	}

	response.Success(c, result)
}

// AppStatistics 应用统计
func (h *StatisticsHandler) AppStatistics(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	appID := c.Param("app_id")

	var app model.Application
	if err := model.DB.First(&app, "id = ? AND tenant_id = ?", appID, tenantID).Error; err != nil {
		response.NotFound(c, "应用不存在")
		return
	}

	// 授权统计
	var totalLicenses int64
	scopedLicenseQuery(c).Where("licenses.app_id = ?", appID).Count(&totalLicenses)

	var activeLicenses int64
	scopedLicenseQuery(c).Where("licenses.app_id = ? AND licenses.status = ?", appID, model.LicenseStatusActive).Count(&activeLicenses)

	var pendingLicenses int64
	scopedLicenseQuery(c).Where("licenses.app_id = ? AND licenses.status = ?", appID, model.LicenseStatusPending).Count(&pendingLicenses)

	var expiredLicenses int64
	scopedLicenseQuery(c).Where("licenses.app_id = ? AND licenses.status = ?", appID, model.LicenseStatusExpired).Count(&expiredLicenses)

	// 客户统计：一个客户只要在该应用下有授权或订阅，就算作该应用客户。
	customerIDs := make(map[string]struct{})
	var licenseCustomerIDs []string
	scopedLicenseQuery(c).
		Where("licenses.app_id = ? AND licenses.customer_id IS NOT NULL AND licenses.customer_id != ''", appID).
		Pluck("customer_id", &licenseCustomerIDs)
	for _, id := range licenseCustomerIDs {
		customerIDs[id] = struct{}{}
	}
	var subscriptionCustomerIDs []string
	scopedSubscriptionQuery(c).
		Where("subscriptions.app_id = ?", appID).
		Pluck("customer_id", &subscriptionCustomerIDs)
	for _, id := range subscriptionCustomerIDs {
		customerIDs[id] = struct{}{}
	}

	customerIDList := make([]string, 0, len(customerIDs))
	for id := range customerIDs {
		customerIDList = append(customerIDList, id)
	}

	var todayCustomers int64
	if len(customerIDList) > 0 {
		model.DB.Model(&model.Customer{}).
			Where("tenant_id = ? AND id IN ? AND created_at >= ?", tenantID, customerIDList, localDayStart(time.Now())).
			Count(&todayCustomers)
	}

	// 设备统计
	var totalDevices int64
	scopedDeviceQuery(c).
		Joins("LEFT JOIN licenses ON devices.license_id = licenses.id").
		Joins("LEFT JOIN subscriptions ON devices.subscription_id = subscriptions.id").
		Where("licenses.app_id = ? OR subscriptions.app_id = ?", appID, appID).
		Count(&totalDevices)

	var activeDevices int64
	oneDayAgo := time.Now().Add(-24 * time.Hour)
	scopedDeviceQuery(c).
		Joins("LEFT JOIN licenses ON devices.license_id = licenses.id").
		Joins("LEFT JOIN subscriptions ON devices.subscription_id = subscriptions.id").
		Where("(licenses.app_id = ? OR subscriptions.app_id = ?) AND devices.last_active_at > ?", appID, appID, oneDayAgo).
		Count(&activeDevices)

	// 脚本统计
	var totalScripts int64
	model.DB.Model(&model.Script{}).Where("app_id = ?", appID).Count(&totalScripts)

	// 版本统计
	var totalReleases int64
	model.DB.Model(&model.AppRelease{}).Where("app_id = ?", appID).Count(&totalReleases)

	var latestRelease model.AppRelease
	model.DB.Where("app_id = ? AND status = ?", appID, model.ReleaseStatusPublished).
		Order("version_code DESC").First(&latestRelease)

	response.Success(c, gin.H{
		"app": gin.H{
			"id":   app.ID,
			"name": app.Name,
		},
		"licenses": gin.H{
			"total":   totalLicenses,
			"active":  activeLicenses,
			"pending": pendingLicenses,
			"expired": expiredLicenses,
		},
		"devices": gin.H{
			"total":  totalDevices,
			"active": activeDevices,
		},
		"customers": gin.H{
			"total":     len(customerIDs),
			"today_new": todayCustomers,
		},
		"applications": gin.H{
			"total": 1,
		},
		"scripts": gin.H{
			"total": totalScripts,
		},
		"releases": gin.H{
			"total":          totalReleases,
			"latest_version": latestRelease.Version,
		},
	})
}

// LicenseTrend 授权趋势（最近30天）
func (h *StatisticsHandler) LicenseTrend(c *gin.Context) {
	appID := c.Query("app_id")
	start, end, err := parseStatsDateRange(c, time.Now().AddDate(0, 0, -30))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	type DayCount struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}

	var results []DayCount

	query := scopedLicenseQuery(c).
		Select("DATE(licenses.created_at) as date, COUNT(*) as count").
		Where("licenses.created_at >= ? AND licenses.created_at < ?", start, end).
		Group("DATE(licenses.created_at)").
		Order("date ASC")

	if appID != "" {
		query = query.Where("licenses.app_id = ?", appID)
	}

	query.Scan(&results)

	response.Success(c, results)
}

// DeviceTrend 设备趋势（最近30天）
func (h *StatisticsHandler) DeviceTrend(c *gin.Context) {
	appID := c.Query("app_id")
	start, end, err := parseStatsDateRange(c, time.Now().AddDate(0, 0, -30))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	type DayCount struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}

	var results []DayCount

	query := scopedDeviceQuery(c).
		Select("DATE(devices.created_at) as date, COUNT(*) as count").
		Where("devices.created_at >= ? AND devices.created_at < ?", start, end).
		Group("DATE(devices.created_at)").
		Order("date ASC")

	if appID != "" {
		query = query.
			Joins("LEFT JOIN licenses ON devices.license_id = licenses.id").
			Joins("LEFT JOIN subscriptions ON devices.subscription_id = subscriptions.id").
			Where("licenses.app_id = ? OR subscriptions.app_id = ?", appID, appID)
	}

	query.Scan(&results)

	response.Success(c, results)
}

// HeartbeatTrend 心跳趋势（最近24小时）
func (h *StatisticsHandler) HeartbeatTrend(c *gin.Context) {
	appID := c.Query("app_id")

	type HourCount struct {
		Hour  string `json:"hour"`
		Count int64  `json:"count"`
	}

	var results []HourCount

	query := scopedHeartbeatQuery(c).
		Select("DATE_FORMAT(heartbeats.created_at, '%Y-%m-%d %H:00') as hour, COUNT(*) as count").
		Where("heartbeats.created_at >= ?", time.Now().Add(-24*time.Hour)).
		Group("hour").
		Order("hour ASC")

	if appID != "" {
		query = query.
			Joins("LEFT JOIN licenses ON heartbeats.license_id = licenses.id").
			Joins("LEFT JOIN subscriptions ON heartbeats.subscription_id = subscriptions.id").
			Where("licenses.app_id = ? OR subscriptions.app_id = ?", appID, appID)
	}

	query.Scan(&results)

	response.Success(c, results)
}

// LicenseTypeDistribution 授权类型分布
func (h *StatisticsHandler) LicenseTypeDistribution(c *gin.Context) {
	appID := c.Query("app_id")
	start, end, err := parseStatsDateRange(c, time.Time{})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	type TypeCount struct {
		Type  string `json:"type"`
		Count int64  `json:"count"`
	}

	var results []TypeCount

	query := scopedLicenseQuery(c).
		Select("licenses.type, COUNT(*) as count").
		Group("licenses.type")

	if !start.IsZero() {
		query = query.Where("licenses.created_at >= ? AND licenses.created_at < ?", start, end)
	}
	if appID != "" {
		query = query.Where("licenses.app_id = ?", appID)
	}

	query.Scan(&results)

	response.Success(c, results)
}

// DeviceOSDistribution 设备系统分布
func (h *StatisticsHandler) DeviceOSDistribution(c *gin.Context) {
	appID := c.Query("app_id")
	start, end, err := parseStatsDateRange(c, time.Time{})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	type OSCount struct {
		OSType string `json:"os_type"`
		Count  int64  `json:"count"`
	}

	var results []OSCount

	query := scopedDeviceQuery(c).
		Select("devices.os_type, COUNT(*) as count").
		Where("devices.os_type IS NOT NULL AND devices.os_type != ''").
		Group("devices.os_type")

	if !start.IsZero() {
		query = query.Where("devices.created_at >= ? AND devices.created_at < ?", start, end)
	}
	if appID != "" {
		query = query.
			Joins("LEFT JOIN licenses ON devices.license_id = licenses.id").
			Joins("LEFT JOIN subscriptions ON devices.subscription_id = subscriptions.id").
			Where("licenses.app_id = ? OR subscriptions.app_id = ?", appID, appID)
	}

	query.Scan(&results)

	response.Success(c, results)
}
