package mapping

import (
	"strings"
)

func Map(message string) string {
	if strings.HasPrefix(message, "Hello") {
		return message + " 😃"
	}
	return message + " 😞"
}
