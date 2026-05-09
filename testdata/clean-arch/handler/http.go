package handler

import (
	"fmt"

	"example.com/cleanarch/domain"
	"example.com/cleanarch/usecase"
)

// HTTPHandler handles HTTP requests.
type HTTPHandler struct {
	service *usecase.UserService
}

// NewHTTPHandler creates a new handler.
func NewHTTPHandler(s *usecase.UserService) *HTTPHandler {
	return &HTTPHandler{service: s}
}

// HandleGetUser handles GET /users/:id.
func (h *HTTPHandler) HandleGetUser(id string) string {
	user := h.service.GetUser(id)
	return formatUser(user)
}

func formatUser(u *domain.User) string {
	return fmt.Sprintf("User: %s (%s)", u.Name, u.Email)
}
