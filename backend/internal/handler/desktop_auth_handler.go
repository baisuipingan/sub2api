package handler

import (
	"errors"
	"mime"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type DesktopAuthHandler struct{ service *service.DesktopAuthService }

func NewDesktopAuthHandler(svc *service.DesktopAuthService) *DesktopAuthHandler {
	return &DesktopAuthHandler{service: svc}
}

func desktopAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDesktopAuthInvalid):
		response.ErrorWithDetails(c, http.StatusBadRequest, "Invalid desktop authorization request", "DESKTOP_AUTH_INVALID", nil)
	case errors.Is(err, service.ErrDesktopAuthInvalidGrant):
		response.ErrorWithDetails(c, http.StatusBadRequest, "Authorization expired or invalid. Start browser login again.", "DESKTOP_AUTH_INVALID_GRANT", nil)
	default:
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Desktop login is temporarily unavailable", "DESKTOP_AUTH_UNAVAILABLE", nil)
	}
}

func readDesktopAuthBody(c *gin.Context) ([]byte, error) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, service.ErrDesktopAuthInvalid
	}
	data, err := readDesktopBody(c, 4<<10)
	if err != nil {
		return nil, service.ErrDesktopAuthInvalid
	}
	return data, nil
}

func (h *DesktopAuthHandler) Authorize(c *gin.Context) {
	raw, err := readDesktopAuthBody(c)
	if err != nil {
		desktopAuthError(c, err)
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.ErrorWithDetails(c, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED", nil)
		return
	}
	input, err := service.ParseDesktopAuthAuthorize(raw)
	if err != nil {
		desktopAuthError(c, err)
		return
	}
	redirect, err := h.service.Authorize(c.Request.Context(), subject.UserID, input)
	if err != nil {
		desktopAuthError(c, err)
		return
	}
	response.Success(c, gin.H{"redirect_url": redirect})
}

func (h *DesktopAuthHandler) Token(c *gin.Context) {
	raw, err := readDesktopAuthBody(c)
	if err != nil {
		desktopAuthError(c, err)
		return
	}
	input, err := service.ParseDesktopAuthToken(raw)
	if err != nil {
		desktopAuthError(c, err)
		return
	}
	pair, user, err := h.service.Exchange(c.Request.Context(), input)
	if err != nil {
		desktopAuthError(c, err)
		return
	}
	response.Success(c, AuthResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken, ExpiresIn: pair.ExpiresIn, TokenType: "Bearer", User: dto.UserFromService(user)})
}
