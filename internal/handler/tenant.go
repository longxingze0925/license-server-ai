package handler

import (
	"license-server/internal/middleware"
	"license-server/internal/model"
	"license-server/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TenantHandler struct{}

func NewTenantHandler() *TenantHandler {
	return &TenantHandler{}
}

// Get 获取当前租户信息
func (h *TenantHandler) Get(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var tenant model.Tenant
	if err := model.DB.First(&tenant, "id = ?", tenantID).Error; err != nil {
		response.NotFound(c, "租户不存在")
		return
	}

	// 获取统计信息
	var appCount, memberCount, customerCount, licenseCount int64
	model.DB.Model(&model.Application{}).Where("tenant_id = ?", tenantID).Count(&appCount)
	model.DB.Model(&model.TeamMember{}).Where("tenant_id = ?", tenantID).Count(&memberCount)
	model.DB.Model(&model.Customer{}).Where("tenant_id = ?", tenantID).Count(&customerCount)
	model.DB.Model(&model.License{}).Where("tenant_id = ?", tenantID).Count(&licenseCount)

	// 获取套餐限制
	limits := tenant.GetPlanLimits()

	response.Success(c, gin.H{
		"id":         tenant.ID,
		"name":       tenant.Name,
		"slug":       tenant.Slug,
		"logo":       tenant.Logo,
		"email":      tenant.Email,
		"phone":      tenant.Phone,
		"website":    tenant.Website,
		"address":    tenant.Address,
		"status":     tenant.Status,
		"plan":       tenant.Plan,
		"created_at": tenant.CreatedAt,
		"usage": gin.H{
			"applications": appCount,
			"team_members": memberCount,
			"customers":    customerCount,
			"licenses":     licenseCount,
		},
		"limits": limits,
	})
}

// UpdateTenantRequest 更新租户请求
type UpdateTenantRequest struct {
	Name    string `json:"name"`
	Logo    string `json:"logo"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Website string `json:"website"`
	Address string `json:"address"`
}

// Update 更新租户信息（需要 Owner 或 Admin 权限）
func (h *TenantHandler) Update(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	var tenant model.Tenant
	if err := model.DB.First(&tenant, "id = ?", tenantID).Error; err != nil {
		response.NotFound(c, "租户不存在")
		return
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Logo != "" {
		updates["logo"] = req.Logo
	}
	if req.Email != "" {
		updates["email"] = req.Email
	}
	if req.Phone != "" {
		updates["phone"] = req.Phone
	}
	if req.Website != "" {
		updates["website"] = req.Website
	}
	if req.Address != "" {
		updates["address"] = req.Address
	}

	if len(updates) > 0 {
		if err := model.DB.Model(&tenant).Updates(updates).Error; err != nil {
			response.ServerError(c, "更新租户失败")
			return
		}
	}

	response.SuccessWithMessage(c, "更新成功", nil)
}

// Delete 删除租户（仅 Owner 可操作）
func (h *TenantHandler) Delete(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	role := middleware.GetUserRole(c)

	// 只有 Owner 可以删除租户
	if role != string(model.RoleOwner) {
		response.Forbidden(c, "只有所有者可以删除租户")
		return
	}

	var tenant model.Tenant
	if err := model.DB.First(&tenant, "id = ?", tenantID).Error; err != nil {
		response.NotFound(c, "租户不存在")
		return
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := cleanupTenantData(tx, tenantID); err != nil {
			return err
		}
		return tx.Delete(&tenant).Error
	}); err != nil {
		response.ServerError(c, "删除租户失败: "+err.Error())
		return
	}

	response.SuccessWithMessage(c, "租户已删除", nil)
}

func cleanupTenantData(tx *gorm.DB, tenantID string) error {
	var appIDs, teamMemberIDs, customerIDs, licenseIDs, subscriptionIDs, deviceIDs, secureScriptIDs, hotUpdateIDs, webhookIDs, generationTaskIDs []string

	if err := tx.Model(&model.Application{}).Where("tenant_id = ?", tenantID).Pluck("id", &appIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.TeamMember{}).Where("tenant_id = ?", tenantID).Pluck("id", &teamMemberIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Customer{}).Where("tenant_id = ?", tenantID).Pluck("id", &customerIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.License{}).Where("tenant_id = ?", tenantID).Pluck("id", &licenseIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Subscription{}).Where("tenant_id = ?", tenantID).Pluck("id", &subscriptionIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Device{}).Where("tenant_id = ?", tenantID).Pluck("id", &deviceIDs).Error; err != nil {
		return err
	}
	if err := pluckIDsByAppIDs(tx, &model.SecureScript{}, appIDs, &secureScriptIDs); err != nil {
		return err
	}
	if err := pluckIDsByAppIDs(tx, &model.HotUpdate{}, appIDs, &hotUpdateIDs); err != nil {
		return err
	}
	if err := tx.Model(&model.GenerationTask{}).Where("tenant_id = ?", tenantID).Pluck("id", &generationTaskIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Webhook{}).Where("org_id = ?", tenantID).Pluck("id", &webhookIDs).Error; err != nil {
		return err
	}

	teamOrCustomerIDs := append(append([]string{}, teamMemberIDs...), customerIDs...)

	if err := deleteWhereIn(tx, &model.GenerationFile{}, "task_id", generationTaskIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.CreditTransaction{}, "user_id", teamMemberIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.UserCredit{}, "user_id", teamMemberIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.Notification{}, "user_id", teamMemberIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.GenerationTask{}, "id", generationTaskIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.ScriptDelivery{}, "script_id", secureScriptIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.HotUpdateLog{}, "hot_update_id", hotUpdateIDs); err != nil {
		return err
	}
	if err := deleteWhereIn(tx, &model.LicenseEvent{}, "license_id", licenseIDs); err != nil {
		return err
	}

	for _, cleanup := range []struct {
		model interface{}
		query string
		args  []interface{}
	}{
		{&model.RealtimeInstructionResult{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.RealtimeInstruction{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.DeviceConnection{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserConfig{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserWorkflow{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserBatchTask{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserMaterial{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserPost{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserComment{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserCommentScript{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserFile{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.SyncCheckpoint{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.SyncConflict{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.SyncLog{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserVoiceConfig{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.UserTableData{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.ClientSyncData{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.ClientSession{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.Heartbeat{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.DeviceBlacklist{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.PublishTask{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.ProviderCredential{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.PricingRule{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.AuditLog{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.SecureScript{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.HotUpdate{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.Script{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.AppRelease{}, "app_id IN ?", []interface{}{appIDs}},
		{&model.Device{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.License{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.Subscription{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.Customer{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.Application{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.TeamInvitation{}, "tenant_id = ?", []interface{}{tenantID}},
		{&model.TeamMember{}, "tenant_id = ?", []interface{}{tenantID}},
	} {
		if len(cleanup.args) == 1 {
			if ids, ok := cleanup.args[0].([]string); ok && len(ids) == 0 {
				continue
			}
		}
		if err := tx.Where(cleanup.query, cleanup.args...).Delete(cleanup.model).Error; err != nil {
			return err
		}
	}

	if err := deleteWhereIn(tx, &model.WebhookLog{}, "webhook_id", webhookIDs); err != nil {
		return err
	}
	if err := tx.Where("org_id = ?", tenantID).Delete(&model.Webhook{}).Error; err != nil {
		return err
	}
	if err := tx.Where("key LIKE ?", "tenant:"+tenantID+":%").Delete(&model.Setting{}).Error; err != nil {
		return err
	}
	if len(teamOrCustomerIDs) > 0 {
		if err := tx.Where("user_id IN ?", teamOrCustomerIDs).Delete(&model.GenerationFile{}).Error; err != nil {
			return err
		}
	}

	return nil
}

func pluckIDsByAppIDs(tx *gorm.DB, modelValue interface{}, appIDs []string, out *[]string) error {
	if len(appIDs) == 0 {
		return nil
	}
	return tx.Model(modelValue).Where("app_id IN ?", appIDs).Pluck("id", out).Error
}

func deleteWhereIn(tx *gorm.DB, modelValue interface{}, column string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return tx.Where(column+" IN ?", ids).Delete(modelValue).Error
}
