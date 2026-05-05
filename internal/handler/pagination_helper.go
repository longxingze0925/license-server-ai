package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

func parsePageParams(c *gin.Context, defaultPageSize, maxPageSize int) (int, int) {
	if defaultPageSize <= 0 {
		defaultPageSize = 20
	}
	if maxPageSize <= 0 {
		maxPageSize = defaultPageSize
	}

	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaultPageSize)))
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}

	return page, pageSize
}
