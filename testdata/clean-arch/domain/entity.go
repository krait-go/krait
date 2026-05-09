package domain

// User is a domain entity.
type User struct {
	ID    string
	Name  string
	Email string
}

// NewUser creates a new User.
func NewUser(id, name, email string) *User {
	return &User{ID: id, Name: name, Email: email}
}
