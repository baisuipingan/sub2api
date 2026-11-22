package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *WorkbenchHandler) GetSearchConfig(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	cfg, err := h.service.GetSearchConfig(c.Request.Context())
	if err != nil {
		workbenchSearchAdminError(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *WorkbenchHandler) UpdateSearchConfig(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	var input struct {
		Provider *string  `json:"provider"`
		APIKey   string   `json:"api_key,omitempty"`
		Price    *float64 `json:"price_per_1000_usd,omitempty"`
	}
	if !decodeWorkbenchSearchBody(c, &input) || input.Provider == nil {
		workbenchAdminError(c, service.ErrWorkbenchInvalid)
		return
	}
	cfg, err := h.service.UpdateSearchConfig(c.Request.Context(), *input.Provider, input.APIKey, input.Price)
	if err != nil {
		workbenchSearchAdminError(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *WorkbenchHandler) TestSearch(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	var input struct {
		Query string `json:"query"`
	}
	if !decodeWorkbenchSearchBody(c, &input) {
		workbenchAdminError(c, service.ErrWorkbenchInvalid)
		return
	}
	result, err := h.service.TestSearch(c.Request.Context(), input.Query)
	if err != nil {
		workbenchSearchAdminError(c, err)
		return
	}
	response.Success(c, result)
}

func decodeWorkbenchSearchBody(c *gin.Context, output any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(output) == nil && decoder.Decode(new(any)) == io.EOF
}

func workbenchSearchAdminError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrWorkbenchInvalid) {
		workbenchAdminError(c, err)
		return
	}
	response.ErrorFrom(c, err)
}
