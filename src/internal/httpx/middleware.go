package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func InstallErrorMiddleware(r *gin.Engine) {
	r.NoRoute(func(c *gin.Context) {
		Fail(c, http.StatusNotFound, CodeNotFound, "route not found")
	})
	r.NoMethod(func(c *gin.Context) {
		MethodNotAllowed(c)
	})
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		Fail(c, http.StatusInternalServerError, CodeInternal, "internal server error")
	}))
}