package redaction

import (
	"regexp"
	"strings"
)

// Policy v1 covers secrets and credentials only; personal data such as e-mail
// addresses or phone numbers is out of scope (Martin, 2026-11-10).
const (
	KindPrivateKey          Kind = "private_key"
	KindAWSAccessKeyID      Kind = "aws_access_key_id"
	KindGitHubToken         Kind = "github_token"
	KindAnthropicAPIKey     Kind = "anthropic_api_key"
	KindOpenAIAPIKey        Kind = "openai_api_key"
	KindSlackToken          Kind = "slack_token"
	KindJWT                 Kind = "jwt"
	KindAuthorizationHeader Kind = "authorization_header"
	KindURLPassword         Kind = "url_password"
	KindSecretAssignment    Kind = "secret_assignment"
)

// placeholderPrefix starts every replacement. No rule may match inside a
// placeholder, and a value that is exactly one known placeholder is left as it
// is, which keeps redaction idempotent.
const placeholderPrefix = "[REDACTED:"

func placeholder(kind Kind) string {
	return placeholderPrefix + string(kind) + "]"
}

// isPlaceholder reports whether value is exactly one complete placeholder of a
// known kind. A forged kind, a missing bracket or any extra text is a value.
func isPlaceholder(value string) bool {
	inner, ok := strings.CutPrefix(value, placeholderPrefix)
	if !ok {
		return false
	}
	kind, ok := strings.CutSuffix(inner, "]")
	return ok && knownKind(Kind(kind))
}

// span is the byte range of one detected value.
type span struct{ start, end int }

// rule replaces the values it finds. values returns non-overlapping spans in
// text order; keys, schemes and other surrounding text stay readable.
type rule struct {
	kind   Kind
	values func(text string) []span
}

// rules run in this order: whole blocks, URL passwords, vendor token shapes,
// then the remaining structural rules, so a value is counted under its most
// specific kind and a later rule sees only the placeholder. URL passwords come
// first because a vendor token in the user name would otherwise hide them.
var rules = []rule{
	// An unterminated block is redacted to the end of the text rather than leaked.
	{KindPrivateKey, wholeMatches(`-----BEGIN[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----(?:[\s\S]*?-----END[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|[\s\S]*$)`)},
	// The scheme, user and host stay; only the password is replaced. It runs
	// before the vendor rules, so a token in the user name cannot change what
	// this rule sees.
	{KindURLPassword, urlPasswords},
	// Shapes that end at a word boundary may also end in a glued secret key
	// name (gluedKeySuffix), which tokenMatches then leaves for the assignment
	// rule; otherwise "_password" would stop the match and the token would
	// become part of the key.
	{KindAWSAccessKeyID, tokenMatches(`\b(?:AKIA|ASIA)[A-Z0-9]{16}` + gluedKeySuffix + `\b`)},
	{KindGitHubToken, tokenMatches(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{36,})` + gluedKeySuffix + `\b`)},
	// Anthropic runs before OpenAI because both start with "sk-".
	{KindAnthropicAPIKey, tokenMatches(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	// Legacy keys exclude hyphens so slugs such as "sk-learn-..." stay intact.
	{KindOpenAIAPIKey, tokenMatches(`\bsk-(?:(?:proj|svcacct|admin)-[A-Za-z0-9_-]{20,}|[A-Za-z0-9]{32,}` + gluedKeySuffix + `\b)`)},
	{KindSlackToken, tokenMatches(`\bxox[baprs]-[A-Za-z0-9-]{10,}`)},
	// Only the header must be JSON ("eyJ"); an unsigned token has an empty signature.
	{KindJWT, tokenMatches(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)},
	// Only the credential after the scheme word is replaced: the whole run up
	// to a space, quote, comma or semicolon, so placeholders that earlier rules
	// left inside it, repeated or unterminated, cannot hide a remaining tail.
	{KindAuthorizationHeader, groupMatches(`(?i)\b(?:proxy-)?authorization["']?[ \t]*[:=][ \t]*["']?(?:bearer|basic|token)[ \t]+([^\s"'` + "`" + `,;]*)`)},
	{KindSecretAssignment, assignmentValues},
}

func wholeMatches(pattern string) func(string) []span {
	re := regexp.MustCompile(pattern)
	return func(text string) []span {
		var spans []span
		for _, m := range re.FindAllStringIndex(text, -1) {
			spans = append(spans, span{m[0], m[1]})
		}
		return spans
	}
}

// gluedKeySuffix optionally extends a token shape over a secret key name glued
// to it.
const gluedKeySuffix = `(?:(?i)[_.-]?` + secretKeyNames + `)?`

// gluedKey is a secret key name at the end of a vendor token match, and
// operatorNext an assignment operator right after it.
var (
	gluedKey     = regexp.MustCompile(`(?i)[_.-]?` + secretKeyNames + `$`)
	operatorNext = regexp.MustCompile(`^["']?\s*[:=]`)
)

// tokenMatches returns each vendor token match. A match that swallowed a key
// glued to the token, as in xoxb-...-password=value, stops before the key, so
// the assignment rule still sees the key and masks its value.
func tokenMatches(pattern string) func(string) []span {
	re := regexp.MustCompile(pattern)
	return func(text string) []span {
		var spans []span
		for _, m := range re.FindAllStringIndex(text, -1) {
			end := m[1]
			if key := gluedKey.FindStringIndex(text[m[0]:end]); key != nil && key[0] > 0 && operatorNext.MatchString(text[end:]) {
				end = m[0] + key[0]
			}
			spans = append(spans, span{m[0], end})
		}
		return spans
	}
}

// groupMatches returns the first capture group of each match.
func groupMatches(pattern string) func(string) []span {
	re := regexp.MustCompile(pattern)
	return func(text string) []span {
		var spans []span
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			spans = append(spans, span{m[2], m[3]})
		}
		return spans
	}
}

// urlUserinfo finds the userinfo of a URL with a scheme. The user name may be
// empty and may hold placeholders, whose own colon is not the separator.
var urlUserinfo = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]*://([^\s/?#@]*)@`)

// urlPasswords returns the password of every URL userinfo: everything after
// the colon that ends the user name.
func urlPasswords(text string) []span {
	var spans []span
	for _, m := range urlUserinfo.FindAllStringSubmatchIndex(text, -1) {
		if colon := userinfoColon(text[m[2]:m[3]]); colon >= 0 {
			spans = append(spans, span{m[2] + colon + 1, m[3]})
		}
	}
	return spans
}

// userinfoColon returns the index of the colon ending the user name, or -1.
// A complete placeholder in the user name is skipped as one unit.
func userinfoColon(userinfo string) int {
	for i := 0; i < len(userinfo); i++ {
		if n := placeholderLength(userinfo[i:]); n > 0 {
			i += n - 1
		} else if userinfo[i] == ':' {
			return i
		}
	}
	return -1
}

// placeholderLength returns the length of the placeholder starting text, or 0.
func placeholderLength(text string) int {
	rest, ok := strings.CutPrefix(text, placeholderPrefix)
	if !ok {
		return 0
	}
	kind := len(rest) - len(strings.TrimLeft(rest, "abcdefghijklmnopqrstuvwxyz_"))
	if kind == 0 || kind == len(rest) || rest[kind] != ']' {
		return 0
	}
	return len(placeholderPrefix) + kind + 1
}

// Policy is redaction policy v1. The zero value is ready to use.
type Policy struct{}

var _ Redactor = Policy{}

// Redact replaces every detected value with "[REDACTED:<kind>]" and counts the
// replacements per kind. Redacting an already redacted text changes nothing.
func (Policy) Redact(text string) Result {
	var findings []Finding
	for _, r := range rules {
		var count int
		text, count = r.apply(text)
		if count > 0 {
			findings = append(findings, Finding{Kind: r.kind, Count: count})
		}
	}
	sortFindings(findings)
	return Result{Text: text, Findings: findings, PolicyVersion: PolicyVersion}
}

// apply rewrites text in one pass and returns how many values it replaced.
// Empty values and values that are exactly one known placeholder stay as they
// are; anything else, including a forged or suffixed placeholder, is replaced.
func (r rule) apply(text string) (string, int) {
	var out strings.Builder
	last, count := 0, 0
	for _, s := range r.values(text) {
		value := text[s.start:s.end]
		if value == "" || isPlaceholder(value) {
			continue
		}
		out.WriteString(text[last:s.start])
		out.WriteString(placeholder(r.kind))
		last = s.end
		count++
	}
	if count == 0 {
		return text, 0
	}
	out.WriteString(text[last:])
	return out.String(), count
}
