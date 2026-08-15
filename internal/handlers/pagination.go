package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

func parsePagination(c *gin.Context) (int, int) {
	page, pageSize := 1, 10
	if n, err := strconv.Atoi(c.Query("page")); err == nil && n > 0 {
		page = n
	}
	if n, err := strconv.Atoi(c.Query("page_size")); err == nil && n > 0 && n <= 100 {
		pageSize = n
	}
	return page, pageSize
}
