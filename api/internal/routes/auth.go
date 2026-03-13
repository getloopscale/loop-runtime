package routes

import (
	"encoding/json"
	"net/http"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// AuthHandler provides HTTP handlers for authentication-related routes.
type AuthHandler struct{}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

// Whoami returns the authenticated user's information.
// GET /api/v1/auth/whoami
func (h *AuthHandler) Whoami(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"user_id": uc.UserID,
		"email":   uc.Email,
		"team":    uc.TenantID,
		"role":    uc.Role,
	})
}
