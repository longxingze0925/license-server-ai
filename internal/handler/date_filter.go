package handler

import (
	"fmt"
	"time"

	"license-server/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func applyCreatedAtDateRange(c *gin.Context, query *gorm.DB, startDate, endDate string) (*gorm.DB, bool) {
	return applyDateRange(c, query, "created_at", startDate, endDate)
}

func applyCreatedAtDateRangeForColumn(c *gin.Context, query *gorm.DB, column, startDate, endDate string) (*gorm.DB, bool) {
	if column == "" {
		column = "created_at"
	}
	return applyDateRange(c, query, column, startDate, endDate)
}

func applyDateRange(c *gin.Context, query *gorm.DB, column, startDate, endDate string) (*gorm.DB, bool) {
	start, end, err := parseDateRange(startDate, endDate)
	if err != nil {
		response.BadRequest(c, err.Error())
		return nil, false
	}
	if start != nil {
		query = query.Where(column+" >= ?", *start)
	}
	if end != nil {
		query = query.Where(column+" < ?", *end)
	}
	return query, true
}

func parseDateRange(startDate, endDate string) (*time.Time, *time.Time, error) {
	var start *time.Time
	var end *time.Time

	if startDate != "" {
		parsed, err := parseDateOnly("start_date", startDate)
		if err != nil {
			return nil, nil, err
		}
		start = &parsed
	}
	if endDate != "" {
		parsed, err := parseDateOnly("end_date", endDate)
		if err != nil {
			return nil, nil, err
		}
		nextDay := parsed.AddDate(0, 0, 1)
		end = &nextDay
	}
	if start != nil && end != nil && !end.After(*start) {
		return nil, nil, fmt.Errorf("end_date 不能早于 start_date")
	}
	return start, end, nil
}

func parseDateOnly(name, raw string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s 必须是 YYYY-MM-DD 格式", name)
	}
	return parsed, nil
}
