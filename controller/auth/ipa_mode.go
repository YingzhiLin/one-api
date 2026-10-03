package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/config"
)

func rejectOutsideIPA(c *gin.Context) bool {
	if !config.IPAOnly {
		return false
	}
	c.JSON(http.StatusOK, gin.H{"success": false, "message": "系统仅允许使用 FreeIPA 登录"})
	return true
}
