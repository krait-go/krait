package usecase

import "example.com/cleanarch/domain"

// UserService handles user business logic.
type UserService struct{}

// GetUser retrieves a user by ID.
func (s *UserService) GetUser(id string) *domain.User {
	return domain.NewUser(id, "Test User", "test@example.com")
}
