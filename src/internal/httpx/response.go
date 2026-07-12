package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ErrBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Data  any      `json:"data,omitempty"`
	Error *ErrBody `json:"error,omitempty"`
}

const (
	CodeBadRequest       = "bad_request"
	CodeNotFound         = "not_found"
	CodeInternal         = "internal"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeValidation       = "validation"
	CodeConflict         = "conflict"
)

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Data: data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Response{Data: data})
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func Fail(c *gin.Context, status int, code, msg string) {
	c.JSON(status, Response{Error: &ErrBody{Code: code, Message: msg}})
}

func FailErr(c *gin.Context, status int, err error) {
	Fail(c, status, CodeInternal, err.Error())
}

func BadRequest(c *gin.Context, msg string) {
	Fail(c, http.StatusBadRequest, CodeBadRequest, msg)
}

func NotFound(c *gin.Context, msg string) {
	Fail(c, http.StatusNotFound, CodeNotFound, msg)
}

func Internal(c *gin.Context, msg string) {
	Fail(c, http.StatusInternalServerError, CodeInternal, msg)
}

func InternalErr(c *gin.Context, err error) {
	Fail(c, http.StatusInternalServerError, CodeInternal, err.Error())
}

func MethodNotAllowed(c *gin.Context) {
	Fail(c, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed")
}

func Bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		Fail(c, http.StatusBadRequest, CodeValidation, err.Error())
		return false
	}
	return true
}