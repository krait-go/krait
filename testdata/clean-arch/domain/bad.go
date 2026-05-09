package domain

import "example.com/cleanarch/handler"

// BadDependency violates clean architecture — domain should NOT import handler.
func BadDependency() string {
	h := handler.NewHTTPHandler(nil)
	return h.HandleGetUser("123")
}
