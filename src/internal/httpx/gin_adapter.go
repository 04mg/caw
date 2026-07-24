package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterAPIGin(mux *http.ServeMux, register func(rg *gin.RouterGroup)) {
	r := gin.New()
	rg := r.Group("")
	register(rg)
	mux.Handle("/", r)
}