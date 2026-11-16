package live

import "strings"

// shellQuote quotes value as one POSIX shell word: it is wrapped in single
// quotes, and each embedded single quote closes the quote, adds an escaped
// quote and reopens it. Nothing inside single quotes is expanded, so spaces,
// $(), backticks and globs stay literal.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
