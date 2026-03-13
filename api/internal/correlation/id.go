package correlation

import (
	"github.com/google/uuid"
)

// GenerateID returns a new UUID v4 correlation ID.
func GenerateID() string {
	return uuid.New().String()
}
