package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
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

const maxBodyBytes = 1 << 20

var (
	validate     = validator.New()
	validateOnce sync.Once
)

func validatorInstance() *validator.Validate {
	validateOnce.Do(func() {})
	return validate
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Printf("json encode error: %v\n", err)
	}
}

func WriteError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, Response{Error: &ErrBody{Code: code, Message: msg}})
}

func RespondJSON(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, Response{Data: data})
}

func RespondCreated(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusCreated, Response{Data: data})
}

func RespondNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func RespondBadRequest(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusBadRequest, CodeBadRequest, msg)
}

func RespondNotFound(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusNotFound, CodeNotFound, msg)
}

func RespondInternal(w http.ResponseWriter, msg string) {
	WriteError(w, http.StatusInternalServerError, CodeInternal, msg)
}

func RespondInternalErr(w http.ResponseWriter, err error) {
	WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error())
}

func RespondMethodNotAllowed(w http.ResponseWriter) {
	WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed")
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalErr *json.UnmarshalTypeError
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.As(err, &syntaxErr):
			WriteError(w, http.StatusBadRequest, CodeValidation,
				fmt.Sprintf("malformed JSON at byte %d", syntaxErr.Offset))
		case errors.As(err, &unmarshalErr):
			field := unmarshalErr.Field
			if field == "" {
				field = unmarshalErr.Type.String()
			}
			WriteError(w, http.StatusBadRequest, CodeValidation,
				fmt.Sprintf("invalid type for field %q", field))
		case errors.As(err, &maxBytesErr):
			WriteError(w, http.StatusRequestEntityTooLarge, CodeBadRequest, "request body too large")
		case errors.Is(err, io.EOF):
			WriteError(w, http.StatusBadRequest, CodeValidation, "request body is empty")
		default:
			WriteError(w, http.StatusBadRequest, CodeValidation, err.Error())
		}
		return false
	}
	return true
}

func Validate(w http.ResponseWriter, v any) bool {
	if err := validatorInstance().Struct(v); err != nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, err.Error())
		return false
	}
	return true
}

func BindRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	return DecodeJSON(w, r, v) && Validate(w, v)
}

func PathValueInt(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, name+" is required")
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, name+" must be an integer")
		return 0, false
	}
	return n, true
}

func QueryParam(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

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