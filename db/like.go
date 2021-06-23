package db

import "strings"

func EscapeLike(s string) string {
	return strings.NewReplacer(
		"_", `\_`,
		"%", `\%`,
	).Replace(s)
}
