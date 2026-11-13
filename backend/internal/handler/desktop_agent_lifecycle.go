package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const desktopCredentialStepUpContextKey = "desktop_credential_step_up_authorized"

// DesktopAgentCredentialStepUpPassed must run after the site's step-up
// middleware. This marks a server-verified authorization, never a body field.
func DesktopAgentCredentialStepUpPassed(c *gin.Context) {
	c.Set(desktopCredentialStepUpContextKey, true)
}

func (h *DesktopAgentHandler) GetProfiles(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	profiles, err := h.service.GetProfiles(c.Request.Context())
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, gin.H{"profiles": profiles})
}

func (h *DesktopAgentHandler) RevokeCredential(c *gin.Context) { h.manageCredential(c, false, false) }
func (h *DesktopAgentHandler) ResetCredential(c *gin.Context)  { h.manageCredential(c, true, false) }
func (h *DesktopAgentHandler) AdminRevokeCredential(c *gin.Context) {
	h.manageCredential(c, false, true)
}
func (h *DesktopAgentHandler) AdminResetCredential(c *gin.Context) { h.manageCredential(c, true, true) }

func (h *DesktopAgentHandler) manageCredential(c *gin.Context, reset, admin bool) {
	c.Header("Cache-Control", "no-store")
	var userID int64
	subject, authenticated := middleware.GetAuthSubjectFromContext(c)
	if !authenticated || subject.UserID <= 0 {
		response.ErrorWithDetails(c, http.StatusUnauthorized, "authentication required", "UNAUTHORIZED", nil)
		return
	}
	caller := service.DesktopAgentCredentialCaller{UserID: subject.UserID}
	if admin {
		if !allowDesktopAgentAdmin(c) {
			return
		}
		role, ok := middleware.GetUserRoleFromContext(c)
		if !ok || role != service.RoleAdmin || (reset && !c.GetBool(desktopCredentialStepUpContextKey)) {
			response.ErrorWithDetails(c, http.StatusForbidden, "full administrator authorization required", "FORBIDDEN", nil)
			return
		}
		caller.Administrator = true
		caller.StepUpAuthorized = c.GetBool(desktopCredentialStepUpContextKey)
		var err error
		userID, err = strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			desktopAgentError(c, service.ErrDesktopAgentInvalid)
			return
		}
	} else {
		userID = subject.UserID
	}
	raw, err := readDesktopBody(c, 128)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	keyID, err := service.ParseDesktopAgentCredentialAction(raw)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	result, err := h.service.ManageCredential(c.Request.Context(), caller, userID, keyID, reset)
	if errors.Is(err, service.ErrAPIKeyNotFound) {
		response.ErrorWithDetails(c, http.StatusNotFound, "managed credential not found", "DESKTOP_CREDENTIAL_NOT_FOUND", nil)
		return
	}
	if errors.Is(err, service.ErrInsufficientPerms) {
		response.ErrorWithDetails(c, http.StatusForbidden, "credential reset is not allowed", "FORBIDDEN", nil)
		return
	}
	if errors.Is(err, service.ErrDesktopAgentRestoreAdminRequired) {
		response.ErrorWithDetails(c, http.StatusForbidden, "disabled Agent credentials can only be restored by an administrator", "DESKTOP_CREDENTIAL_RESTORE_ADMIN_REQUIRED", nil)
		return
	}
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, result)
}
