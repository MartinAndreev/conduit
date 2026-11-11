package redaction

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Fixtures are obviously fake and assembled from pieces so that secret
// scanners do not flag the source file.
var (
	fakePEMBody    = "FAKEKEYBODYexampleONLY"
	fakePEM        = "-----BEGIN " + "RSA PRIVATE KEY-----\n" + fakePEMBody + "\n-----END RSA PRIVATE KEY-----"
	fakeOpenSSH    = "-----BEGIN " + "OPENSSH PRIVATE KEY-----\n" + fakePEMBody + "\n-----END OPENSSH PRIVATE KEY-----"
	fakePGP        = "-----BEGIN " + "PGP PRIVATE KEY BLOCK-----\n" + fakePEMBody + "\n-----END PGP PRIVATE KEY BLOCK-----"
	fakeAWS        = "AKIA" + "EXAMPLEEXAMPLE12"
	fakeAWSSession = "ASIA" + "EXAMPLEEXAMPLE34"
	fakeGitHub     = "ghp_" + strings.Repeat("Ex4mple", 6)
	fakeGitHubApp  = "ghs_" + strings.Repeat("Ex4mple", 6)
	fakeGitHubPAT  = "github_pat_" + strings.Repeat("Ex4mple_", 6)
	fakeAnthropic  = "sk-ant-" + "api03-" + "fakeEXAMPLEfakeEXAMPLE-fake"
	fakeOpenAI     = "sk-" + "proj-" + "fakeEXAMPLEfakeEXAMPLE_fake"
	fakeOpenAIOld  = "sk-" + strings.Repeat("Ex4mple", 6)
	fakeSlack      = "xox" + "b-" + "000000000000-fakeEXAMPLE"
	fakeJWT        = "eyJ" + "hbGciOiJub25lIn0" + "." + "eyJzdWIiOiJmYWtlIn0" + "." + "ZmFrZXNpZ25hdHVyZQ"
	fakeBearer     = "fakeOpaqueBearer" + "Value"
	fakeBasic      = "ZmFrZTpm" + "YWtl"
	fakeURLPass    = "fakePassw0rd" + "Example"
	fakeSecret     = "hunter2" + "Example"
)

func findings(pairs ...any) []Finding {
	var out []Finding
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Finding{Kind: pairs[i].(Kind), Count: pairs[i+1].(int)})
	}
	return out
}

func TestPolicyRedact(t *testing.T) {
	tests := []redactCase{
		// Positive: one case per pattern.
		{"pem private key block", "key:\n" + fakePEM + "\nend", "key:\n[REDACTED:private_key]\nend", findings(KindPrivateKey, 1), []string{fakePEMBody}},
		{"openssh private key", fakeOpenSSH, "[REDACTED:private_key]", findings(KindPrivateKey, 1), []string{fakePEMBody}},
		{"pgp private key block", fakePGP, "[REDACTED:private_key]", findings(KindPrivateKey, 1), []string{fakePEMBody}},
		{"unterminated private key to end of text", "x " + "-----BEGIN " + "PRIVATE KEY-----\n" + fakePEMBody + "\nmore", "x [REDACTED:private_key]", findings(KindPrivateKey, 1), []string{fakePEMBody}},
		{"aws access key id", "id " + fakeAWS + " ok", "id [REDACTED:aws_access_key_id] ok", findings(KindAWSAccessKeyID, 1), []string{fakeAWS}},
		{"aws session key id", fakeAWSSession, "[REDACTED:aws_access_key_id]", findings(KindAWSAccessKeyID, 1), []string{fakeAWSSession}},
		{"github classic token", "use " + fakeGitHub, "use [REDACTED:github_token]", findings(KindGitHubToken, 1), []string{fakeGitHub}},
		{"github app token", fakeGitHubApp, "[REDACTED:github_token]", findings(KindGitHubToken, 1), []string{fakeGitHubApp}},
		{"github fine-grained token", fakeGitHubPAT, "[REDACTED:github_token]", findings(KindGitHubToken, 1), []string{fakeGitHubPAT}},
		{"anthropic key", "k=" + fakeAnthropic, "k=[REDACTED:anthropic_api_key]", findings(KindAnthropicAPIKey, 1), []string{fakeAnthropic}},
		{"openai project key", fakeOpenAI, "[REDACTED:openai_api_key]", findings(KindOpenAIAPIKey, 1), []string{fakeOpenAI}},
		{"openai legacy key", "(" + fakeOpenAIOld + ")", "([REDACTED:openai_api_key])", findings(KindOpenAIAPIKey, 1), []string{fakeOpenAIOld}},
		{"slack token", fakeSlack, "[REDACTED:slack_token]", findings(KindSlackToken, 1), []string{fakeSlack}},
		{"jwt", "jwt " + fakeJWT + ".", "jwt [REDACTED:jwt].", findings(KindJWT, 1), []string{fakeJWT}},
		{"bearer header", "Authorization: Bearer " + fakeBearer, "Authorization: Bearer [REDACTED:authorization_header]", findings(KindAuthorizationHeader, 1), []string{fakeBearer}},
		{"basic header in curl", `curl -H "authorization: Basic ` + fakeBasic + `"`, `curl -H "authorization: Basic [REDACTED:authorization_header]"`, findings(KindAuthorizationHeader, 1), []string{fakeBasic}},
		{"proxy authorization json", `{"Proxy-Authorization": "Bearer ` + fakeBearer + `"}`, `{"Proxy-Authorization": "Bearer [REDACTED:authorization_header]"}`, findings(KindAuthorizationHeader, 1), []string{fakeBearer}},
		{"url password keeps scheme user host", "postgres://app:" + fakeURLPass + "@db.example.test:5432/app", "postgres://app:[REDACTED:url_password]@db.example.test:5432/app", findings(KindURLPassword, 1), []string{fakeURLPass}},
		{"env assignment", "DB_PASSWORD=" + fakeSecret, "DB_PASSWORD=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"yaml value masked to end of line", "client_secret: " + fakeSecret + " # rotate", "client_secret: [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"json assignment", `{"api_key": "` + fakeSecret + `", "n": 1}`, `{"api_key": "[REDACTED:secret_assignment]", "n": 1}`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"single quoted with escape", `passwd = 'it\'s` + fakeSecret + `'`, `passwd = '[REDACTED:secret_assignment]'`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"double quoted with escape", `secret: "a\"` + fakeSecret + `"`, `secret: "[REDACTED:secret_assignment]"`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"go short assignment", `accessKey := "` + fakeSecret + `"`, `accessKey := "[REDACTED:secret_assignment]"`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"query string token", "https://api.example.test/x?token=" + fakeSecret + "&page=2", "https://api.example.test/x?token=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"dot before digits", "password=" + fakeSecret + ".2024", "password=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"bare identifier counts as a value", "Password: " + fakeSecret + ",", "Password: [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"literal prefix is not a literal", "token: nullable" + fakeSecret, "token: [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"cli flag", "--password=" + fakeSecret, "--password=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"other key names", "APIKEY=" + fakeSecret + "\nsecret-key=" + fakeSecret + "\nprivate_key: " + fakeSecret + "\naws_secret_access_key=" + fakeSecret,
			"APIKEY=[REDACTED:secret_assignment]\nsecret-key=[REDACTED:secret_assignment]\nprivate_key: [REDACTED:secret_assignment]\naws_secret_access_key=[REDACTED:secret_assignment]",
			findings(KindSecretAssignment, 4), []string{fakeSecret}},

		// Negative: no value, or a value that is not a secret.
		{"empty text", "", "", nil, nil},
		{"prose with token and password", "Rotate the token before release; the password reset flow is unchanged.", "Rotate the token before release; the password reset flow is unchanged.", nil, nil},
		{"git sha", "commit 3f786850e387550fdab836ed7e6dc881de23001b", "commit 3f786850e387550fdab836ed7e6dc881de23001b", nil, nil},
		{"uuid", "id 123e4567-e89b-12d3-a456-426614174000", "id 123e4567-e89b-12d3-a456-426614174000", nil, nil},
		{"content hash", "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", nil, nil},
		{"token-like key names", "max_tokens: 1024\ntoken_count=5\npassword_hash: abc", "max_tokens: 1024\ntoken_count=5\npassword_hash: abc", nil, nil},
		{"key followed by deeper lines masks them", "password:\n  nested: x\n    deeper\nnext: y", "password:\n  [REDACTED:secret_assignment]\nnext: y", findings(KindSecretAssignment, 1), []string{"nested", "deeper"}},
		{"key followed by sibling has no value", "password:\nnext: y", "password:\nnext: y", nil, nil},
		{"empty quoted value", `token: ""`, `token: ""`, nil, nil},
		{"key at end of text", "password=", "password=", nil, nil},
		{"variable references and placeholders are masked", "password: ${DB_PASSWORD}\ntoken=$TOKEN\nsecret: <your-secret>\npassword: $hunter2\npassword: (hunter2)",
			"password: [REDACTED:secret_assignment]\ntoken=[REDACTED:secret_assignment]\nsecret: [REDACTED:secret_assignment]\npassword: [REDACTED:secret_assignment]\npassword: [REDACTED:secret_assignment]",
			findings(KindSecretAssignment, 5), []string{"hunter2", "DB_PASSWORD", "TOKEN", "your-secret"}},
		{"comparison", "if password == other {", "if password == other {", nil, nil},
		{"url without password", "https://user@example.test/path and http://example.test:8080/x", "https://user@example.test/path and http://example.test:8080/x", nil, nil},
		{"sk slug and short sk", "sk-learn-preprocessing-pipeline-v2 and sk-short", "sk-learn-preprocessing-pipeline-v2 and sk-short", nil, nil},
		{"bearer in prose", "a bearer token is required", "a bearer token is required", nil, nil},
		{"aws-like inside word", "XAKIA" + "EXAMPLEEXAMPLE12", "XAKIA" + "EXAMPLEEXAMPLE12", nil, nil},
		{"public key block", "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----", "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----", nil, nil},

		// Mixed text and multiple occurrences.
		{"multiple of one kind", fakeAWS + " and " + fakeAWSSession + " and " + fakeAWS, "[REDACTED:aws_access_key_id] and [REDACTED:aws_access_key_id] and [REDACTED:aws_access_key_id]", findings(KindAWSAccessKeyID, 3), []string{fakeAWS, fakeAWSSession}},
		{"specific kind wins inside assignment", `api_key = "` + fakeAnthropic + `"` + "\ntoken=" + fakeGitHub,
			`api_key = "[REDACTED:anthropic_api_key]"` + "\ntoken=[REDACTED:github_token]",
			findings(KindAnthropicAPIKey, 1, KindGitHubToken, 1), []string{fakeAnthropic, fakeGitHub}},
		{"jwt wins inside bearer header", "Authorization: Bearer " + fakeJWT, "Authorization: Bearer [REDACTED:jwt]", findings(KindJWT, 1), []string{fakeJWT}},
		{"pem inside json value", `{"private_key": "` + fakePEM + `"}`, `{"private_key": "[REDACTED:private_key]"}`, findings(KindPrivateKey, 1), []string{fakePEMBody}},
		{"mixed text sorted by kind", "slack " + fakeSlack + "\nurl https://bot:" + fakeURLPass + "@ci.example.test\npassword=" + fakeSecret + "\naws " + fakeAWS + "\nbody " + fakeOpenAI,
			"slack [REDACTED:slack_token]\nurl https://bot:[REDACTED:url_password]@ci.example.test\npassword=[REDACTED:secret_assignment]\naws [REDACTED:aws_access_key_id]\nbody [REDACTED:openai_api_key]",
			findings(KindAWSAccessKeyID, 1, KindOpenAIAPIKey, 1, KindSecretAssignment, 1, KindSlackToken, 1, KindURLPassword, 1),
			[]string{fakeSlack, fakeURLPass, fakeSecret, fakeAWS, fakeOpenAI}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}

type redactCase struct {
	name    string
	input   string
	want    string
	finds   []Finding
	secrets []string
}

// checkRedact asserts the exact output and findings, that no secret survives,
// and that a second pass changes nothing and finds nothing.
func checkRedact(t *testing.T, tt redactCase) {
	t.Helper()
	got := Policy{}.Redact(tt.input)
	if got.Text != tt.want {
		t.Fatalf("text = %q, want %q", got.Text, tt.want)
	}
	if !slices.Equal(got.Findings, tt.finds) {
		t.Fatalf("findings = %v, want %v", got.Findings, tt.finds)
	}
	if got.PolicyVersion != PolicyVersion {
		t.Fatalf("policy version = %q, want %q", got.PolicyVersion, PolicyVersion)
	}
	for _, secret := range tt.secrets {
		if strings.Contains(got.Text, secret) || strings.Contains(fmt.Sprint(got.Findings), secret) {
			t.Fatalf("secret %q leaked into result", secret)
		}
	}
	again := Policy{}.Redact(got.Text)
	if again.Text != got.Text || len(again.Findings) != 0 {
		t.Fatalf("not idempotent: text %q, findings %v", again.Text, again.Findings)
	}
}

// Every placeholder must survive every rule, including as an assignment value.
func TestPlaceholdersAreInert(t *testing.T) {
	for _, r := range rules {
		p := placeholder(r.kind)
		for _, text := range []string{p, "password=" + p, `token: "` + p + `"`, "Authorization: Bearer " + p, "https://u:" + p + "@h.example.test"} {
			if got := (Policy{}).Redact(text); got.Text != text || len(got.Findings) != 0 {
				t.Errorf("placeholder text %q changed to %q with %v", text, got.Text, got.Findings)
			}
		}
	}
}

// Only a value that is exactly one known placeholder is exempt; anything that
// merely starts like one is a value.
func TestForgedAndPartialPlaceholders(t *testing.T) {
	tests := []redactCase{
		{"quoted forged kind with tail", `password="[REDACTED:fake]` + fakeSecret + `"`, `password="[REDACTED:secret_assignment]"`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"quoted known placeholder with tail", `password="[REDACTED:secret_assignment]` + fakeSecret + `"`, `password="[REDACTED:secret_assignment]"`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"unquoted forged kind with tail", "password=[REDACTED:fake]" + fakeSecret, "password=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"unquoted known placeholder with tail", "token: [REDACTED:jwt]" + fakeSecret + " # x", "token: [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"forged exact placeholder", "password=[REDACTED:fake]", "password=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), nil},
		{"malformed placeholder without bracket", "password=[REDACTED:secret_assignment" + fakeSecret, "password=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"secret starting with bracket", "password=[" + fakeSecret, "password=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"url password with forged prefix", "https://u:[REDACTED:fake]" + fakeURLPass + "@host.example.test", "https://u:[REDACTED:url_password]@host.example.test", findings(KindURLPassword, 1), []string{fakeURLPass}},
		{"url password with known prefix", "https://u:[REDACTED:url_password]" + fakeURLPass + "@host.example.test", "https://u:[REDACTED:url_password]@host.example.test", findings(KindURLPassword, 1), []string{fakeURLPass}},
		{"bearer with placeholder prefix", "Authorization: Bearer [REDACTED:jwt]" + fakeBearer, "Authorization: Bearer [REDACTED:authorization_header]", findings(KindAuthorizationHeader, 1), []string{fakeBearer}},
		{"overlap leaves quoted remainder", `token="` + fakeGitHub + ` ` + fakeSecret + `"`, `token="[REDACTED:secret_assignment]"`, findings(KindGitHubToken, 1, KindSecretAssignment, 1), []string{fakeGitHub, fakeSecret}},
		{"overlap leaves unquoted suffix", "token=" + fakeGitHub + "." + fakeSecret, "token=[REDACTED:secret_assignment]", findings(KindGitHubToken, 1, KindSecretAssignment, 1), []string{fakeGitHub, fakeSecret}},
		{"overlap inside url password", "https://u:" + fakeGitHub + "x." + fakeURLPass + "@h.example.test", "https://u:[REDACTED:url_password]@h.example.test", findings(KindURLPassword, 1), []string{fakeGitHub, fakeURLPass}},
		{"flow structure is masked", "secret: []", "secret: [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), nil},
		{"vendor token as url user", "https://" + fakeGitHub + ":" + fakeURLPass + "@host.example.test", "https://[REDACTED:github_token]:[REDACTED:url_password]@host.example.test", findings(KindGitHubToken, 1, KindURLPassword, 1), []string{fakeGitHub, fakeURLPass}},
		{"vendor token with suffix as url user", "https://" + fakeGitHub + ".x:" + fakeURLPass + "@host.example.test", "https://[REDACTED:github_token].x:[REDACTED:url_password]@host.example.test", findings(KindGitHubToken, 1, KindURLPassword, 1), []string{fakeGitHub, fakeURLPass}},
		{"yaml doubled quote after placeholder", "password: '[REDACTED:secret_assignment]''" + fakeSecret + "'", "password: '[REDACTED:secret_assignment]'", findings(KindSecretAssignment, 1), []string{fakeSecret}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}

func TestMultilineValues(t *testing.T) {
	tests := []redactCase{
		{"double quoted across LF", "password: \"line1\n" + fakeSecret + "\"\nnext: 1", "password: \"[REDACTED:secret_assignment]\"\nnext: 1", findings(KindSecretAssignment, 1), []string{"line1", fakeSecret}},
		{"single quoted across CRLF", "token = 'a\r\n" + fakeSecret + "'\r\nnext", "token = '[REDACTED:secret_assignment]'\r\nnext", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"escaped newline and quote", `secret: "a\` + "\n" + `\"` + fakeSecret + `"`, `secret: "[REDACTED:secret_assignment]"`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"backtick raw string", "password: `" + fakeSecret + "\\`", "password: `[REDACTED:secret_assignment]`", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"unterminated double quote to end of text", "password: \"" + fakeSecret + "\nmore " + fakeSecret, "password: \"[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"unterminated quote with trailing backslash", `token = '` + fakeSecret + `\`, `token = '[REDACTED:secret_assignment]`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"go short assignment unterminated", `apiKey := "` + fakeSecret, `apiKey := "[REDACTED:secret_assignment]`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"literal block", "token: |\n  " + fakeSecret + "\n  line2\nnext: x", "token: |\n  [REDACTED:secret_assignment]\nnext: x", findings(KindSecretAssignment, 1), []string{fakeSecret, "line2"}},
		{"folded block with blank line", "secret: >\n  part1\n\n  " + fakeSecret + "\n\nafter", "secret: >\n  [REDACTED:secret_assignment]\n\nafter", findings(KindSecretAssignment, 1), []string{"part1", fakeSecret}},
		{"strip chomping with comment", "password: |- # rotate\n    " + fakeSecret + "\n", "password: |- # rotate\n    [REDACTED:secret_assignment]\n", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"indent and keep indicators with CRLF", "token: >2+\r\n  " + fakeSecret + "\r\nnext: 1", "token: >2+\r\n  [REDACTED:secret_assignment]\r\nnext: 1", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"nested key ends at sibling", "db:\n  password: |\n    " + fakeSecret + "\n    password: x\n  user: app", "db:\n  password: |\n    [REDACTED:secret_assignment]\n  user: app", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"block at end of text", "token: |\n  " + fakeSecret, "token: |\n  [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"block without body", "token: |\nnext: x", "token: |\nnext: x", nil, nil},
		{"block header at end of text", "token: >-", "token: >-", nil, nil},
		{"indicator with stray text is a value", "token: |" + fakeSecret, "token: [REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"block header before CR without body", "token: |\r", "token: |\r", nil, nil},
		{"block with CR line breaks", "password: |\r  " + fakeSecret + "\r  line2\rnext: x", "password: |\r  [REDACTED:secret_assignment]\rnext: x", findings(KindSecretAssignment, 1), []string{fakeSecret, "line2"}},
		{"nested block with CR line breaks", "db:\r  token: >\r    " + fakeSecret + "\r  user: app", "db:\r  token: >\r    [REDACTED:secret_assignment]\r  user: app", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"quoted across CR", "token: \"a\r" + fakeSecret + "\"\rnext", "token: \"[REDACTED:secret_assignment]\"\rnext", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"yaml doubled single quote", "password: 'first''" + fakeSecret + "'", "password: '[REDACTED:secret_assignment]'", findings(KindSecretAssignment, 1), []string{"first", fakeSecret}},
		{"yaml empty single quoted", "{'token': '', 'n': 'x'}", "{'token': '', 'n': 'x'}", nil, nil},
		{"triple double quotes", `password = """` + fakeSecret + `"""` + "\nnext", `password = """[REDACTED:secret_assignment]"""` + "\nnext", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"triple single quotes across lines", "password = '''a\n" + fakeSecret + "''' # x", "password = '''[REDACTED:secret_assignment]''' # x", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"triple quotes with escaped quote", `token = """a\"""` + fakeSecret + `"""`, `token = """[REDACTED:secret_assignment]"""`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"unterminated triple quotes", `secret = """` + fakeSecret + `""`, `secret = """[REDACTED:secret_assignment]`, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"pipe after equals is a value", "token=|\n  x", "token=[REDACTED:secret_assignment]", findings(KindSecretAssignment, 1), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}

// After ":=" only quoted literals count, and whole literals such as null are
// never credentials; such material must not be blocked.
func TestLiteralsAndGoCodeAreNotCredentials(t *testing.T) {
	for _, text := range []string{
		"token := scanner.Token()",
		"password := config.Password",
		"accessKey := someVariable",
		`{"token": null}`,
		"password: true",
		"api_key: False",
		"token = None",
		"secret: nil",
		"token: ~",
		"password=undefined;",
		"mytoken=abc",
	} {
		t.Run(text, func(t *testing.T) { checkRedact(t, redactCase{input: text, want: text}) })
	}
}

// An ambiguous value after "=" or ":" is masked, even when it looks like code
// that reads a credential (Martin, 2026-11-10).
func TestAmbiguousValuesAreMasked(t *testing.T) {
	const p = "[REDACTED:secret_assignment]"
	tests := []redactCase{
		{"dotted words", "password: correct.horse", "password: " + p, findings(KindSecretAssignment, 1), []string{"correct.horse"}},
		{"env dotted words", "DB_PASSWORD=correct.horse", "DB_PASSWORD=" + p, findings(KindSecretAssignment, 1), []string{"correct.horse"}},
		{"parentheses", "password=abc(def)", "password=" + p, findings(KindSecretAssignment, 1), []string{"abc(def)"}},
		{"settings attribute", "password = settings.X", "password = " + p, findings(KindSecretAssignment, 1), []string{"settings.X"}},
		{"call before quoted argument", `secret = os.Getenv("X")`, `secret = ` + p, findings(KindSecretAssignment, 1), []string{"os.Getenv"}},
		{"bare call", "const token = getToken();", "const token = " + p, findings(KindSecretAssignment, 1), []string{"getToken"}},
		{"struct field", "Token: cfg.Auth.Token,", "Token: " + p, findings(KindSecretAssignment, 1), []string{"cfg.Auth.Token"}},
		{"self attribute", "password = self.password", "password = " + p, findings(KindSecretAssignment, 1), []string{"self.password"}},
		{"method with argument", "key token=obj.method(arg)", "key token=" + p, findings(KindSecretAssignment, 1), []string{"obj.method"}},
		{"literal with suffix", "token: null.x", "token: " + p, findings(KindSecretAssignment, 1), []string{"null.x"}},
		{"literal with closing bracket and tail", "password: true)x", "password: " + p, findings(KindSecretAssignment, 1), []string{"true)x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}

// Every placeholder output must be stable under a second pass, whatever the
// input. The seeds cover each rule and their overlaps.
func FuzzRedactIsIdempotent(f *testing.F) {
	for _, seed := range []string{
		fakePEM, fakeAWS, fakeGitHub, fakeAnthropic, fakeOpenAI, fakeSlack, fakeJWT,
		"Authorization: Bearer " + fakeJWT + ".x",
		"https://u:" + fakeGitHub + "@h",
		"token=" + fakeGitHub + "." + fakeSecret,
		"password: \"a\nb",
		"token: |\n  a\n b\nc",
		"password=[REDACTED:secret_assignment]x",
		"https://" + fakeGitHub + ":" + fakeURLPass + "@h",
		"password: 'a''b'",
		"password: '[REDACTED:secret_assignment]''x'",
		`password = """a"""`,
		"password = '''a\nb'''",
		"password: |\r  a\rnext: x",
		"token: >\r\n  a\r b\nc",
		"password: correct.horse",
		"password=abc(def) token := x.y()",
		`{"token": null} password: nullable`,
		"token: ~)]} secret: true",
		"password: null horse battery",
		"password: $hunter2 # x",
		"{\"password\"\r\n:\r\n  x,\n\"n\": 1}",
		"password:\n  a: 1\n    b\nc: d",
		"PASSWORD=\n  x\nTOKEN=",
		"https://u-" + fakeGitHub + ":p@h",
		"https://:p@h https://[REDACTED:jwt][REDACTED:jwt]:p@h",
		"token: | # token: | # x\nnext: x",
		"if password == x && token=y",
		"A://ghp_000000000000000000000000000000000000@",
		"a://[REDACTED:fake:pw@h a://[REDACTED:@h a://[REDACTED:x:pw@h",
	} {
		f.Add(seed)
	}
	for _, seed := range overlapInputs() {
		f.Add(seed)
	}
	for _, seed := range []string{"password=123", "token=|", "secret: ==x", "a == b token= =c", "password: a\n  b\n c\nd"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		once := Policy{}.Redact(text)
		twice := Policy{}.Redact(once.Text)
		if twice.Text != once.Text || len(twice.Findings) != 0 {
			t.Fatalf("not idempotent for %q: %q then %q with %v", text, once.Text, twice.Text, twice.Findings)
		}
	})
}

// adversarialInputs build inputs of about n bytes; each shape once took
// quadratic time.
var adversarialInputs = map[string]func(n int) string{
	"rejected variable values": func(n int) string { return strings.Repeat("token==$", n/8) },
	"call-like values":         func(n int) string { return strings.Repeat("token=a.b(", n/10) },
	"literal values":           func(n int) string { return strings.Repeat("token=null}\n", n/12) },
	"go short assignments":     func(n int) string { return strings.Repeat("token:=a.b ", n/11) },
	"keys after word chars":    func(n int) string { return strings.Repeat("xtoken=", n/7) },
	"open quotes":              func(n int) string { return strings.Repeat(`token="`, n/7) },
	"block headers":            func(n int) string { return strings.Repeat("token: |\n", n/9) },
	"cr block headers":         func(n int) string { return strings.Repeat("token: >\r", n/9) },
	"headers in one comment":   func(n int) string { return strings.Repeat("token: | # ", n/11) + "\nnext: x" },
	"comment then long line":   func(n int) string { return strings.Repeat("token: | # ", n/22) + "\n" + strings.Repeat("x", n/2) },
	"keys before line breaks":  func(n int) string { return strings.Repeat("token:\n", n/7) },
	"quoted keys and breaks":   func(n int) string { return strings.Repeat("\"token\"\n:\n", n/11) },
	"private key openings":     func(n int) string { return strings.Repeat("-----BEGIN PRIVATE KEY-----", n/27) },
	"url prefixes":             func(n int) string { return strings.Repeat("a://[REDACTED:x]:", n/17) },
	"authorization prefixes":   func(n int) string { return strings.Repeat("authorization: bearer ", n/22) },
	"key prefixes":             func(n int) string { return strings.Repeat("a_", n/2) },
}

// A quadratic regression takes tens of seconds at 200 KB and roughly
// quadruples when the input doubles; linear time doubles at most.
func TestRedactAdversarialInputsAreLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("large inputs")
	}
	for name, build := range adversarialInputs {
		full := build(200_000)
		start := time.Now()
		Policy{}.Redact(full)
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("%s took %v at %d bytes", name, elapsed, len(full))
		}
		small, large := build(50_000), build(100_000)
		smallTime, largeTime := fastestRedact(small), fastestRedact(large)
		if largeTime > 50*time.Millisecond && largeTime > 3*smallTime {
			t.Errorf("%s: %v at %d bytes but %v at %d bytes", name, smallTime, len(small), largeTime, len(large))
		}
	}
}

// fastestRedact returns the faster of two runs, which damps scheduler noise.
func fastestRedact(text string) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 2 {
		start := time.Now()
		Policy{}.Redact(text)
		best = min(best, time.Since(start))
	}
	return best
}

func BenchmarkRedactAdversarial(b *testing.B) {
	for name, build := range adversarialInputs {
		text := build(200_000)
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				Policy{}.Redact(text)
			}
		})
	}
}

// After "=" or ":" an unquoted value runs to the end of its line; the line
// break may also come between a quoted key, its operator and its value.
func TestEndOfLineAndLaterLineValues(t *testing.T) {
	const p = "[REDACTED:secret_assignment]"
	tests := []redactCase{
		{"literal followed by words", "password: null horse battery", "password: " + p, findings(KindSecretAssignment, 1), []string{"horse"}},
		{"words with spaces", "password: correct horse battery  \nnext", "password: " + p + "  \nnext", findings(KindSecretAssignment, 1), []string{"correct", "battery"}},
		{"comma inside value", "password: abc,def", "password: " + p, findings(KindSecretAssignment, 1), []string{"abc", "def"}},
		{"angle placeholder", "password: <x>", "password: " + p, findings(KindSecretAssignment, 1), []string{"<x>"}},
		{"literal with comma", `{"token": null},`, `{"token": null},`, nil, nil},
		{"json value after LF", "{\"password\":\n  \"" + fakeSecret + "\"}", "{\"password\":\n  \"" + p + "\"}", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"json operator after LF", "{\"password\"\n: \"" + fakeSecret + "\"}", "{\"password\"\n: \"" + p + "\"}", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"json breaks with CRLF", "{\"password\"\r\n:\r\n\"" + fakeSecret + "\"}", "{\"password\"\r\n:\r\n\"" + p + "\"}", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"json breaks with CR", "{'token'\r:\r'" + fakeSecret + "'}", "{'token'\r:\r'" + p + "'}", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"quoted key unquoted value on next line", "{\n\"password\":\n" + fakeSecret + ",\n\"n\": 1\n}", "{\n\"password\":\n" + p + "\n\"n\": 1\n}", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"quoted key null on next line", "{\"token\":\n  null}", "{\"token\":\n  null}", nil, nil},
		{"unquoted key plain scalar on deeper lines", "password:\r\n  " + fakeSecret + "\r\n  more\r\nuser: x", "password:\r\n  " + p + "\r\nuser: x", findings(KindSecretAssignment, 1), []string{fakeSecret, "more"}},
		{"unquoted key after blank lines", "token:\n\n    " + fakeSecret, "token:\n\n    " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"equals then deeper line", "PASSWORD=\n  " + fakeSecret, "PASSWORD=\n  " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"equals then sibling line", "PASSWORD=\nTOKEN=" + fakeSecret, "PASSWORD=\nTOKEN=" + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"break between unquoted key and operator", "password\n: x", "password\n: x", nil, nil},
		{"comparison keeps later key", "if password == x && token=" + fakeSecret, "if password == x && token=" + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"go short assignment after break", "token :=\n\t\"" + fakeSecret + "\"", "token :=\n\t\"" + p + "\"", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"go short assignment code after break", "token :=\n\tscanner.Token()", "token :=\n\tscanner.Token()", nil, nil},
		{"indentation indicator then sign", "token: |2-\n   " + fakeSecret, "token: |2-\n   " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"indicator glued to comment is a value", "token: |#x", "token: " + p, findings(KindSecretAssignment, 1), nil},
		{"vendor token after user prefix", "https://u-" + fakeGitHub + ":" + fakeURLPass + "@host.example.test", "https://u-[REDACTED:github_token]:[REDACTED:url_password]@host.example.test", findings(KindGitHubToken, 1, KindURLPassword, 1), []string{fakeGitHub, fakeURLPass}},
		{"two vendor tokens as user", "https://" + fakeGitHub + "." + fakeSlack + ":" + fakeURLPass + "@h.example.test", "https://[REDACTED:github_token].[REDACTED:slack_token]:[REDACTED:url_password]@h.example.test", findings(KindGitHubToken, 1, KindSlackToken, 1, KindURLPassword, 1), []string{fakeGitHub, fakeSlack, fakeURLPass}},
		{"two placeholders as user", "https://[REDACTED:jwt][REDACTED:github_token]:" + fakeURLPass + "@h.example.test", "https://[REDACTED:jwt][REDACTED:github_token]:[REDACTED:url_password]@h.example.test", findings(KindURLPassword, 1), []string{fakeURLPass}},
		{"vendor token as whole userinfo", "A://" + fakeGitHub + "@h", "A://[REDACTED:github_token]@h", findings(KindGitHubToken, 1), []string{fakeGitHub}},
		{"unterminated placeholder in user", "a://[REDACTED:fake:" + fakeURLPass + "@h", "a://[REDACTED:[REDACTED:url_password]@h", findings(KindURLPassword, 1), []string{fakeURLPass}},
		{"placeholder prefix without kind", "a://[REDACTED:]:" + fakeURLPass + "@h", "a://[REDACTED:[REDACTED:url_password]@h", findings(KindURLPassword, 1), []string{fakeURLPass}},
		{"placeholder prefix at end of userinfo", "a://u[REDACTED:x@h", "a://u[REDACTED:[REDACTED:url_password]@h", findings(KindURLPassword, 1), nil},
		{"empty password", "https://u:@h", "https://u:@h", nil, nil},
		{"empty user", "https://:" + fakeURLPass + "@host.example.test", "https://:[REDACTED:url_password]@host.example.test", findings(KindURLPassword, 1), []string{fakeURLPass}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}

// Numbers, "=x" and a lone "|" after "=" are values; only an adjacent "==" is
// a comparison. A same-line value continues over deeper lines.
func TestValuesWithoutExemptions(t *testing.T) {
	const p = "[REDACTED:secret_assignment]"
	tests := []redactCase{
		{"digits", "password=123456789", "password=" + p, findings(KindSecretAssignment, 1), []string{"123456789"}},
		{"yaml digits", "token: 123456", "token: " + p, findings(KindSecretAssignment, 1), []string{"123456"}},
		{"signs and digits", "password=++--123", "password=" + p, findings(KindSecretAssignment, 1), []string{"++--123"}},
		{"zero", "token: 0", "token: " + p, findings(KindSecretAssignment, 1), nil},
		{"lone pipe after equals", "token=|", "token=" + p, findings(KindSecretAssignment, 1), nil},
		{"equals sign after colon", "password: =" + fakeSecret, "password: " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"spaced equals sign", "password = =" + fakeSecret, "password = " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"adjacent comparison", "password == other", "password == other", nil, nil},
		{"spaced comparison", "if password== other", "if password== other", nil, nil},
		{"plain scalar continuation", "password: first\n  " + fakeSecret + "\n\n  more\nuser: x", "password: " + p + "\nuser: x", findings(KindSecretAssignment, 1), []string{"first", fakeSecret, "more"}},
		{"continuation with CR", "db:\r  token: first\r    " + fakeSecret + "\r  user: x", "db:\r  token: " + p + "\r  user: x", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"sibling is not a continuation", "password: first\nuser: x", "password: " + p + "\nuser: x", findings(KindSecretAssignment, 1), []string{"first"}},
		{"literal without deeper lines", "token: null\nuser: x", "token: null\nuser: x", nil, nil},
		{"literal with deeper line LF", "password: null\n  " + fakeSecret, "password: " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"literal with deeper line CRLF", "password: true\r\n  " + fakeSecret + "\r\nuser: x", "password: " + p + "\r\nuser: x", findings(KindSecretAssignment, 1), []string{fakeSecret}},
		{"literal with deeper line CR", "token: ~\r    " + fakeSecret, "token: " + p, findings(KindSecretAssignment, 1), []string{fakeSecret}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}

// overlapInputs compose rules so that one replacement could hide what another
// should mask. Each contains fakeSecret, which must never survive.
func overlapInputs() []string {
	ghpA, ghpB := "ghp_"+strings.Repeat("A", 36), "ghp_"+strings.Repeat("B", 36)
	return []string{
		"Authorization: Bearer " + ghpA + "." + ghpB + "." + fakeSecret,
		"Authorization: Bearer [REDACTED:jwt][REDACTED:jwt]" + fakeSecret,
		"Authorization: Bearer [REDACTED:jwt" + fakeSecret,
		"authorization: basic [REDACTED:" + fakeSecret + "]x",
		`{"Authorization": "Bearer ` + fakeJWT + "." + fakeSecret + `"}`,
		"Proxy-Authorization: token " + fakeAWS + ":" + fakeSecret,
		"token: " + fakeAWS + "." + fakeSecret,
		"password=" + fakeGitHub + "." + fakeSlack + "." + fakeSecret,
		"https://" + fakeGitHub + ":" + fakeSecret + "@h.example.test",
		"https://u:" + fakeGitHub + "." + fakeSecret + "@h.example.test",
		"https://[REDACTED:jwt" + ":" + fakeSecret + "@h.example.test",
		`api_key = "` + fakeAnthropic + " " + fakeSecret + `"`,
		"password: |\n  " + fakeJWT + "\n  " + fakeSecret,
		"password: first\n  " + fakeSecret,
		"secret: =" + fakeSecret,
		"password = =" + fakeSecret,
		"\"password\":\n \"" + fakeSlack + " " + fakeSecret + "\"",
		"password: '[REDACTED:jwt]''" + fakeSecret + "'",
		"token=[REDACTED:jwt][REDACTED:jwt]" + fakeSecret,
		"token: " + fakeOpenAI + "," + fakeSecret,
		"password = '''" + fakeGitHub + "\n" + fakeSecret + "'''",
		"password: null\n  " + fakeSecret,
		fakeSlack + "-password=" + fakeSecret,
		fakeSlack + "token: " + fakeSecret,
		ghpA + "_secret_key = " + fakeSecret,
		ghpA + "password=" + fakeSecret,
		fakeAWS + "password=" + fakeSecret,
		fakeAWS + "_api_key: " + fakeSecret,
		fakeOpenAI + "-token=" + fakeSecret,
		fakeOpenAIOld + "Password = " + fakeSecret,
		fakeAnthropic + "_passwd=" + fakeSecret,
		fakeJWT + "-secret: " + fakeSecret,
		`{"` + fakeSlack + `-token": "` + fakeSecret + `"}`,
	}
}

func TestComposedRulesLeaveNoSecret(t *testing.T) {
	for _, input := range overlapInputs() {
		got := Policy{}.Redact(input)
		if strings.Contains(got.Text, fakeSecret) || leaksVendorToken(got.Text) || len(got.Findings) == 0 {
			t.Errorf("%q became %q with %v", input, got.Text, got.Findings)
		}
		if again := (Policy{}).Redact(got.Text); again.Text != got.Text || len(again.Findings) != 0 {
			t.Errorf("not idempotent for %q: %q then %q", input, got.Text, again.Text)
		}
	}
}

// leaksVendorToken reports whether a fixture token survived.
func leaksVendorToken(text string) bool {
	for _, token := range []string{fakeSlack, fakeAWS, fakeOpenAI, fakeOpenAIOld, fakeAnthropic, fakeJWT, fakeGitHub, "ghp_AAAA"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

// A key glued to a vendor token keeps its value masked; the token is counted
// under its own kind.
func TestGluedKeyAfterVendorToken(t *testing.T) {
	const p = "[REDACTED:secret_assignment]"
	tests := []redactCase{
		{"slack", fakeSlack + "-password=" + fakeSecret, "[REDACTED:slack_token]-password=" + p, findings(KindSecretAssignment, 1, KindSlackToken, 1), []string{fakeSlack, fakeSecret}},
		{"aws without separator", fakeAWS + "password=" + fakeSecret, "[REDACTED:aws_access_key_id]password=" + p, findings(KindAWSAccessKeyID, 1, KindSecretAssignment, 1), []string{fakeAWS, fakeSecret}},
		{"aws with separator", fakeAWS + "_api_key: " + fakeSecret, "[REDACTED:aws_access_key_id]_api_key: " + p, findings(KindAWSAccessKeyID, 1, KindSecretAssignment, 1), []string{fakeAWS, fakeSecret}},
		{"aws followed by key name in prose", fakeAWS + "password here", "[REDACTED:aws_access_key_id] here", findings(KindAWSAccessKeyID, 1), []string{fakeAWS}},
		{"github with separator", fakeGitHub + "_secret_key = " + fakeSecret, "[REDACTED:github_token]_secret_key = [REDACTED:secret_assignment]", findings(KindGitHubToken, 1, KindSecretAssignment, 1), []string{fakeGitHub, fakeSecret}},
		{"openai legacy with separator", fakeOpenAIOld + "_token: " + fakeSecret, "[REDACTED:openai_api_key]_token: [REDACTED:secret_assignment]", findings(KindOpenAIAPIKey, 1, KindSecretAssignment, 1), []string{fakeOpenAIOld, fakeSecret}},
		{"github", fakeGitHub + "token=" + fakeSecret, "[REDACTED:github_token]token=" + p, findings(KindGitHubToken, 1, KindSecretAssignment, 1), []string{fakeGitHub, fakeSecret}},
		{"openai quoted key", `{"` + fakeOpenAI + `-token": "` + fakeSecret + `"}`, `{"[REDACTED:openai_api_key]-token": "` + p + `"}`, findings(KindOpenAIAPIKey, 1, KindSecretAssignment, 1), []string{fakeOpenAI, fakeSecret}},
		{"key name without operator stays in token", fakeSlack + "-password and more", "[REDACTED:slack_token] and more", findings(KindSlackToken, 1), []string{fakeSlack}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkRedact(t, tt) })
	}
}
