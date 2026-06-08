package configdoctor

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/andrepester/rag-search-mcp/internal/config"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Finding struct {
	Severity    Severity `json:"severity"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation"`
}

type Report struct {
	Findings []Finding `json:"findings"`
}

func (r Report) ErrorCount() int {
	count := 0
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			count++
		}
	}
	return count
}

func (r Report) WarningCount() int {
	count := 0
	for _, finding := range r.Findings {
		if finding.Severity == SeverityWarning {
			count++
		}
	}
	return count
}

func (r Report) HasErrors() bool {
	return r.ErrorCount() > 0
}

type Options struct {
	RepoRoot     string
	HostRepoRoot string
	HostHome     string
	Environ      []string
}

type checker struct {
	repoRoot     string
	hostRepoRoot string
	hostHome     string
	environ      map[string]string
	dotenv       map[string]string
	report       Report
}

type valueSource struct {
	value  string
	source string
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var defaults = map[string]string{
	"RAG_HTTP_HOST":           "127.0.0.1",
	"RAG_HTTP_PORT":           "8765",
	"HOST_DOCS_DIR":           "./data/docs",
	"HOST_CODE_DIR":           "./data/code",
	"HOST_INDEX_DIR":          "./data/index",
	"EMBED_MODEL":             "nomic-embed-text",
	"RAG_ENABLE_CODE_INGEST":  "true",
	"RAG_CHROMA_TENANT":       "default_tenant",
	"RAG_CHROMA_DATABASE":     "default_database",
	"RAG_COLLECTION_NAME":     "rag",
	"RAG_SCOPE_DEFAULT":       "all",
	"RAG_CHUNK_SIZE":          "1200",
	"RAG_CHUNK_OVERLAP":       "200",
	"RAG_MAX_TOP_K":           "50",
	"RAG_MAX_SEARCH_DISTANCE": "0.50",
	"RAG_INDEX_LIMIT":         "0",
	"RAG_LOG_LEVEL":           "info",
	"RAG_LOG_FORMAT":          "json",
}

var hostPathKeys = []string{
	"HOST_DOCS_DIR",
	"HOST_CODE_DIR",
	"HOST_INDEX_DIR",
}

func Check(repoRoot string, environ []string) (Report, error) {
	return CheckWithOptions(Options{RepoRoot: repoRoot, Environ: environ})
}

func CheckWithOptions(opts Options) (Report, error) {
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return Report{}, fmt.Errorf("resolve repo root: %w", err)
	}

	hostRepoRoot := strings.TrimSpace(opts.HostRepoRoot)
	if hostRepoRoot == "" {
		hostRepoRoot = absRoot
	}
	if absHostRoot, err := filepath.Abs(hostRepoRoot); err == nil {
		hostRepoRoot = absHostRoot
	}

	env := opts.Environ
	if env == nil {
		env = os.Environ()
	}

	c := &checker{
		repoRoot:     filepath.Clean(absRoot),
		hostRepoRoot: filepath.Clean(hostRepoRoot),
		hostHome:     filepath.Clean(strings.TrimSpace(opts.HostHome)),
		environ:      environMap(env),
		dotenv:       map[string]string{},
	}

	c.checkDotEnv()
	c.checkRuntimeValues()
	c.checkHostPaths()
	c.checkComposeSecurity()

	return c.report, nil
}

func (c *checker) checkDotEnv() {
	path := filepath.Join(c.repoRoot, ".env")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.add(SeverityWarning, "DOTENV_MISSING", ".env is missing; Compose will use defaults only.", "Run make install to bootstrap .env from .env.example, or create .env with the documented values.")
			return
		}
		c.add(SeverityError, "DOTENV_STAT", fmt.Sprintf("cannot stat .env: %v", err), "Fix file permissions or recreate .env with make install.")
		return
	}
	if info.IsDir() {
		c.add(SeverityError, "DOTENV_IS_DIRECTORY", ".env is a directory.", "Replace .env with a regular environment file.")
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		c.add(SeverityWarning, "DOTENV_PERMISSIONS", ".env is readable by group or others.", "Run chmod 600 .env to keep local configuration private.")
	}

	values, findings, err := parseDotEnv(path)
	if err != nil {
		c.add(SeverityError, "DOTENV_READ", fmt.Sprintf("cannot read .env: %v", err), "Fix file permissions or recreate .env with make install.")
		return
	}
	c.dotenv = values
	for _, finding := range findings {
		c.report.Findings = append(c.report.Findings, finding)
	}

}

func (c *checker) checkRuntimeValues() {
	c.checkPort("RAG_HTTP_PORT")

	chunkSize := c.checkPositiveInt("RAG_CHUNK_SIZE")
	chunkOverlap := c.checkNonNegativeInt("RAG_CHUNK_OVERLAP")
	if chunkSize > 0 && chunkOverlap >= chunkSize {
		c.add(SeverityError, "CHUNK_OVERLAP_RANGE", fmt.Sprintf("RAG_CHUNK_OVERLAP resolves to %d, which is not smaller than RAG_CHUNK_SIZE %d.", chunkOverlap, chunkSize), "Set RAG_CHUNK_OVERLAP to a non-negative value smaller than RAG_CHUNK_SIZE.")
	}
	c.checkPositiveInt("RAG_MAX_TOP_K")
	c.checkNonNegativeInt("RAG_INDEX_LIMIT")
	c.checkSearchDistance()
	c.checkHTTPHost()
	c.checkBool("RAG_ENABLE_CODE_INGEST")
	c.checkOneOf("RAG_SCOPE_DEFAULT", []string{"all", "docs", "code"})
	c.checkOneOf("RAG_LOG_LEVEL", []string{"debug", "info", "warn", "error"})
	c.checkOneOf("RAG_LOG_FORMAT", []string{"json", "text"})
	c.checkNonEmpty("EMBED_MODEL")
	c.checkNonEmpty("RAG_CHROMA_TENANT")
	c.checkNonEmpty("RAG_CHROMA_DATABASE")
	c.checkNonEmpty("RAG_COLLECTION_NAME")
	c.checkNonEmpty("OLLAMA_HOST")
	c.checkURL("OLLAMA_HOST")
}

func (c *checker) checkHostPaths() {
	codeIngestEnabled := true
	if parsed, ok := parseBool(c.effective("RAG_ENABLE_CODE_INGEST").value); ok {
		codeIngestEnabled = parsed
	}

	resolvedPaths := map[string]string{}
	for _, key := range hostPathKeys {
		src := c.effective(key)
		raw := strings.TrimSpace(src.value)
		if raw == "" {
			c.add(SeverityError, key+"_EMPTY", fmt.Sprintf("%s resolves to an empty value.", key), fmt.Sprintf("Set %s to a repository-relative or absolute host directory.", key))
			continue
		}
		resolved, err := c.resolveHostPath(raw)
		if err != nil {
			c.add(SeverityError, key+"_PATH", fmt.Sprintf("%s=%q cannot be resolved: %v.", key, raw, err), fmt.Sprintf("Set %s to a normal directory path, not %q.", key, raw))
			continue
		}
		resolvedPaths[key] = resolved

		displayPath := c.displayPath(resolved)
		info, err := os.Stat(resolved)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if key == "HOST_CODE_DIR" && !codeIngestEnabled {
					c.add(SeverityWarning, key+"_MISSING", fmt.Sprintf("%s points to %s, which does not exist; code ingest is disabled.", key, displayPath), fmt.Sprintf("Create %s if you plan to enable RAG_ENABLE_CODE_INGEST.", displayPath))
				} else {
					c.add(SeverityWarning, key+"_MISSING", fmt.Sprintf("%s points to %s, which does not exist.", key, displayPath), "Run make install to create default host directories, or update .env to an existing source/persistence path.")
				}
			} else {
				c.add(SeverityError, key+"_STAT", fmt.Sprintf("cannot stat %s at %s: %v.", key, displayPath, err), "Fix path permissions or choose a readable host directory.")
			}
		} else if !info.IsDir() {
			c.add(SeverityError, key+"_NOT_DIRECTORY", fmt.Sprintf("%s points to %s, which is not a directory.", key, displayPath), fmt.Sprintf("Set %s to a directory path.", key))
		}

		if key == "HOST_INDEX_DIR" {
			c.checkPersistentPathSafety(key, resolved)
		}
	}
	c.checkHostPathRelations(resolvedPaths)
}

func (c *checker) checkPersistentPathSafety(key string, resolved string) {
	displayPath := c.displayPath(resolved)
	candidates := uniqueNonEmpty([]string{
		filepath.Clean(resolved),
		filepath.Clean(displayPath),
	})
	repoCandidates := uniqueNonEmpty([]string{
		c.repoRoot,
		c.hostRepoRoot,
	})
	homeCandidates := uniqueNonEmpty([]string{
		c.hostHome,
	})

	for _, candidate := range candidates {
		if candidate == string(filepath.Separator) || candidate == "." {
			c.add(SeverityError, key+"_UNSAFE_ROOT", fmt.Sprintf("%s resolves to unsafe path %s.", key, displayPath), fmt.Sprintf("Set %s to a dedicated persistence directory such as ./data/index.", key))
			return
		}
		for _, repo := range repoCandidates {
			if candidate == repo {
				c.add(SeverityError, key+"_UNSAFE_REPO", fmt.Sprintf("%s points at the repository root %s.", key, displayPath), fmt.Sprintf("Set %s to a dedicated child directory such as ./data/index.", key))
				return
			}
			if candidate == filepath.Dir(repo) {
				c.add(SeverityError, key+"_UNSAFE_REPO_PARENT", fmt.Sprintf("%s points at the repository parent %s.", key, displayPath), fmt.Sprintf("Set %s to a dedicated persistence directory.", key))
				return
			}
			if isAncestor(candidate, repo) {
				c.add(SeverityError, key+"_UNSAFE_REPO_ANCESTOR", fmt.Sprintf("%s=%s is an ancestor of the repository.", key, displayPath), fmt.Sprintf("Set %s to a dedicated persistence directory, not a broad parent path.", key))
				return
			}
		}
		for _, home := range homeCandidates {
			if candidate == home {
				c.add(SeverityError, key+"_UNSAFE_HOME", fmt.Sprintf("%s points at HOME (%s).", key, displayPath), fmt.Sprintf("Set %s to a dedicated persistence directory.", key))
				return
			}
		}
		if isBroadPath(candidate) {
			c.add(SeverityError, key+"_UNSAFE_BROAD_PATH", fmt.Sprintf("%s resolves to broad path %s.", key, displayPath), fmt.Sprintf("Set %s to a narrower dedicated persistence directory.", key))
			return
		}
	}
}

func (c *checker) resolveHostPath(raw string) (string, error) {
	return resolvePath(c.hostRepoRoot, raw)
}

func (c *checker) checkHostPathRelations(paths map[string]string) {
	for _, persistenceKey := range []string{"HOST_INDEX_DIR"} {
		persistencePath, ok := paths[persistenceKey]
		if !ok {
			continue
		}
		for _, sourceKey := range []string{"HOST_DOCS_DIR", "HOST_CODE_DIR"} {
			sourcePath, ok := paths[sourceKey]
			if !ok {
				continue
			}
			if filepath.Clean(persistencePath) == filepath.Clean(sourcePath) || isAncestor(sourcePath, persistencePath) {
				c.add(SeverityError, persistenceKey+"_INSIDE_SOURCE", fmt.Sprintf("%s resolves inside %s at %s.", persistenceKey, sourceKey, c.displayPath(persistencePath)), "Keep source mounts and persistence directories separate so generated runtime data is not ingested as source content.")
				continue
			}
			if isAncestor(persistencePath, sourcePath) {
				c.add(SeverityError, sourceKey+"_INSIDE_PERSISTENCE", fmt.Sprintf("%s resolves inside %s at %s.", sourceKey, persistenceKey, c.displayPath(sourcePath)), "Keep source mounts and persistence directories separate so generated runtime data is not ingested as source content.")
			}
		}
	}

	docsPath, hasDocs := paths["HOST_DOCS_DIR"]
	codePath, hasCode := paths["HOST_CODE_DIR"]
	if hasDocs && hasCode && filepath.Clean(docsPath) == filepath.Clean(codePath) {
		c.add(SeverityWarning, "HOST_SOURCE_PATH_OVERLAP", fmt.Sprintf("HOST_DOCS_DIR and HOST_CODE_DIR both resolve to %s.", c.displayPath(docsPath)), "Use separate docs and code source directories unless this overlap is intentional.")
	}
}

func (c *checker) checkComposeSecurity() {
	path := filepath.Join(c.repoRoot, "docker", "docker-compose.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		c.add(SeverityError, "COMPOSE_READ", fmt.Sprintf("cannot read docker/docker-compose.yml: %v.", err), "Restore docker/docker-compose.yml or fix file permissions.")
		return
	}
	content := string(raw)
	if hasHostlessPortPublish(content, "RAG_HTTP_PORT", "8765") {
		c.add(SeverityError, "COMPOSE_MCP_HOSTLESS_PUBLISH", "docker-compose.yml publishes rag-mcp without an explicit host bind.", "Use RAG_HTTP_HOST with a loopback default so LAN-only access remains an explicit opt-in.")
	}
	if regexp.MustCompile(`(?m)^\s*-\s*"?0\.0\.0\.0:\$\{RAG_HTTP_PORT`).MatchString(content) {
		c.add(SeverityError, "COMPOSE_MCP_PUBLIC_BIND", "docker-compose.yml publishes rag-mcp on 0.0.0.0 without the LAN opt-in variable.", "Keep the Compose default loopback-only and use RAG_HTTP_HOST for explicit LAN-only operation.")
	}
	if strings.Contains(content, "${RAG_HTTP_HOST:-0.0.0.0}") || strings.Contains(content, "${RAG_HTTP_HOST:-[::]}") {
		c.add(SeverityError, "COMPOSE_MCP_PUBLIC_DEFAULT", "docker-compose.yml defaults RAG_HTTP_HOST to all interfaces.", "Keep RAG_HTTP_HOST defaulted to 127.0.0.1 and opt into LAN-only operation through .env or the process environment.")
	}
	if regexp.MustCompile(`(?m)^\s*-\s*"?\[::\]:\$\{RAG_HTTP_PORT`).MatchString(content) {
		c.add(SeverityError, "COMPOSE_MCP_PUBLIC_IPV6_BIND", "docker-compose.yml publishes rag-mcp on all IPv6 interfaces.", "Keep the Compose default loopback-only and use RAG_HTTP_HOST for explicit LAN-only operation.")
	}
	if !strings.Contains(content, `"${RAG_HTTP_HOST:-127.0.0.1}:${RAG_HTTP_PORT:-8765}:8765"`) {
		c.add(SeverityWarning, "COMPOSE_MCP_LOOPBACK_DEFAULT", "docker-compose.yml no longer contains the expected loopback publish default for rag-mcp.", "Verify that /mcp is still localhost-only by default and LAN-only access requires RAG_HTTP_HOST opt-in.")
	}
}

func (c *checker) checkPort(key string) int {
	value := c.effective(key).value
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		c.add(SeverityError, key+"_INTEGER", fmt.Sprintf("%s resolves to %q, which is not an integer.", key, value), fmt.Sprintf("Set %s to a TCP port between 1 and 65535.", key))
		return 0
	}
	if port < 1 || port > 65535 {
		c.add(SeverityError, key+"_RANGE", fmt.Sprintf("%s resolves to %d, outside the allowed range.", key, port), fmt.Sprintf("Set %s to a TCP port between 1 and 65535.", key))
		return 0
	}
	return port
}

func (c *checker) checkHTTPHost() {
	src := c.effective("RAG_HTTP_HOST")
	host := strings.TrimSpace(src.value)
	if host == "" {
		c.add(SeverityError, "RAG_HTTP_HOST_EMPTY", "RAG_HTTP_HOST resolves to an empty value.", "Set RAG_HTTP_HOST to 127.0.0.1 for the default localhost-only mode, or to an approved LAN bind address for explicit LAN-only operation.")
		return
	}
	normalized := strings.ToLower(strings.Trim(host, "[] "))
	if isLoopbackHost(normalized) {
		return
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		c.add(SeverityWarning, "RAG_HTTP_HOST_NAME", fmt.Sprintf("RAG_HTTP_HOST resolves to non-loopback hostname %q in %s.", host, src.source), "Ensure this hostname is constrained to the approved LAN-only network boundary; default installs should use 127.0.0.1.")
		return
	}
	if ip.IsUnspecified() {
		c.add(SeverityWarning, "RAG_HTTP_HOST_ALL_INTERFACES", fmt.Sprintf("RAG_HTTP_HOST resolves to all interfaces (%s) in %s.", host, src.source), "Prefer a specific approved LAN interface IP when possible, and ensure host firewall rules exclude WAN/public reachability.")
		return
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		c.add(SeverityWarning, "RAG_HTTP_HOST_LAN_OPT_IN", fmt.Sprintf("RAG_HTTP_HOST resolves to LAN address %s in %s.", host, src.source), "LAN-only operation is active; ensure only approved source networks can reach /mcp and WAN/VPN exposure remains out of scope.")
		return
	}
	c.add(SeverityError, "RAG_HTTP_HOST_PUBLIC", fmt.Sprintf("RAG_HTTP_HOST resolves to non-private address %s in %s.", host, src.source), "Use 127.0.0.1 for default operation or an approved private LAN address for LAN-only opt-in; WAN/public exposure is out of scope for v1.")
}

func (c *checker) checkPositiveInt(key string) int {
	value := c.effective(key).value
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		c.add(SeverityError, key+"_INTEGER", fmt.Sprintf("%s resolves to %q, which is not an integer.", key, value), fmt.Sprintf("Set %s to a positive integer.", key))
		return 0
	}
	if parsed <= 0 {
		c.add(SeverityError, key+"_POSITIVE", fmt.Sprintf("%s resolves to %d.", key, parsed), fmt.Sprintf("Set %s to a positive integer.", key))
		return 0
	}
	return parsed
}

func (c *checker) checkNonNegativeInt(key string) int {
	value := c.effective(key).value
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		c.add(SeverityError, key+"_INTEGER", fmt.Sprintf("%s resolves to %q, which is not an integer.", key, value), fmt.Sprintf("Set %s to a non-negative integer.", key))
		return 0
	}
	if parsed < 0 {
		c.add(SeverityError, key+"_NON_NEGATIVE", fmt.Sprintf("%s resolves to %d.", key, parsed), fmt.Sprintf("Set %s to a non-negative integer.", key))
		return 0
	}
	return parsed
}

func (c *checker) checkSearchDistance() {
	key := "RAG_MAX_SEARCH_DISTANCE"
	value := c.effective(key).value
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		c.add(SeverityError, key+"_NUMBER", fmt.Sprintf("%s resolves to %q, which is not a number.", key, value), fmt.Sprintf("Set %s to a number between %.2f and %.2f.", key, config.MinSearchDistance, config.MaxSearchDistance))
		return
	}
	if parsed < config.MinSearchDistance || parsed > config.MaxSearchDistance {
		c.add(SeverityError, key+"_RANGE", fmt.Sprintf("%s resolves to %.2f, outside the allowed range.", key, parsed), fmt.Sprintf("Set %s to a number between %.2f and %.2f.", key, config.MinSearchDistance, config.MaxSearchDistance))
	}
}

func (c *checker) checkBool(key string) {
	value := c.effective(key).value
	if _, ok := parseBool(value); !ok {
		c.add(SeverityError, key+"_BOOLEAN", fmt.Sprintf("%s resolves to %q, which is not a boolean.", key, value), fmt.Sprintf("Set %s to true or false.", key))
	}
}

func (c *checker) checkOneOf(key string, allowed []string) {
	value := strings.ToLower(strings.TrimSpace(c.effective(key).value))
	for _, option := range allowed {
		if value == option {
			return
		}
	}
	c.add(SeverityError, key+"_VALUE", fmt.Sprintf("%s resolves to %q.", key, c.effective(key).value), fmt.Sprintf("Set %s to one of: %s.", key, strings.Join(allowed, ", ")))
}

func (c *checker) checkNonEmpty(key string) {
	if strings.TrimSpace(c.effective(key).value) == "" {
		c.add(SeverityError, key+"_EMPTY", fmt.Sprintf("%s resolves to an empty value.", key), fmt.Sprintf("Set %s to a non-empty value.", key))
	}
}

func (c *checker) checkURL(key string) {
	value := strings.TrimSpace(c.effective(key).value)
	if value == "" {
		return
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		c.add(SeverityError, key+"_URL", fmt.Sprintf("%s resolves to invalid URL %q.", key, value), fmt.Sprintf("Set %s to an http(s) URL, for example http://ollama.example.internal:11434.", key))
		return
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		c.add(SeverityError, key+"_URL_SCHEME", fmt.Sprintf("%s uses unsupported scheme %q.", key, parsed.Scheme), fmt.Sprintf("Set %s to an http(s) URL.", key))
	}
}

func (c *checker) effective(key string) valueSource {
	if value, ok := c.environ[key]; ok && strings.TrimSpace(value) != "" {
		return valueSource{value: strings.TrimSpace(value), source: "process environment"}
	}
	if value, ok := c.dotenv[key]; ok && strings.TrimSpace(value) != "" {
		return valueSource{value: strings.TrimSpace(value), source: ".env"}
	}
	if value, ok := defaults[key]; ok {
		return valueSource{value: value, source: "defaults"}
	}
	return valueSource{}
}

func (c *checker) add(severity Severity, code string, message string, remediation string) {
	c.report.Findings = append(c.report.Findings, Finding{
		Severity:    severity,
		Code:        code,
		Message:     message,
		Remediation: remediation,
	})
}

func (c *checker) displayPath(path string) string {
	cleanPath := filepath.Clean(path)
	if c.hostRepoRoot != "" && c.hostRepoRoot != c.repoRoot {
		if cleanPath == c.repoRoot {
			return c.hostRepoRoot
		}
		prefix := c.repoRoot + string(filepath.Separator)
		if strings.HasPrefix(cleanPath, prefix) {
			return filepath.Join(c.hostRepoRoot, strings.TrimPrefix(cleanPath, prefix))
		}
	}
	return cleanPath
}

func parseDotEnv(path string) (map[string]string, []Finding, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	values := map[string]string{}
	var findings []Finding
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			findings = append(findings, Finding{
				Severity:    SeverityError,
				Code:        "DOTENV_SYNTAX",
				Message:     fmt.Sprintf(".env line %d is not KEY=VALUE syntax.", lineNumber),
				Remediation: "Fix the line or comment it out with #.",
			})
			continue
		}
		key := strings.TrimSpace(parts[0])
		if !envKeyPattern.MatchString(key) {
			findings = append(findings, Finding{
				Severity:    SeverityError,
				Code:        "DOTENV_KEY",
				Message:     fmt.Sprintf(".env line %d has invalid key %q.", lineNumber, key),
				Remediation: "Use shell-compatible environment keys such as RAG_HTTP_PORT.",
			})
			continue
		}
		if _, exists := values[key]; exists {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Code:        "DOTENV_DUPLICATE_KEY",
				Message:     fmt.Sprintf(".env defines %s more than once; the later value wins.", key),
				Remediation: fmt.Sprintf("Keep a single %s entry in .env.", key),
			})
		}
		values[key] = trimEnvValue(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return values, findings, nil
}

func trimEnvValue(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, `"'`)
	return value
}

func hasHostlessPortPublish(content string, hostPortKey string, targetPort string) bool {
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "-") {
			continue
		}
		spec := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		spec = strings.Trim(spec, `"'`)
		if spec == targetPort || spec == targetPort+"/tcp" || strings.HasPrefix(spec, targetPort+":") {
			return true
		}
		if strings.HasPrefix(spec, "${"+hostPortKey) {
			return true
		}
	}
	return false
}

func environMap(environ []string) map[string]string {
	values := map[string]string{}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func resolvePath(repoRoot string, raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	if base == "." || base == ".." {
		return "", fmt.Errorf("terminal path segment must not be %q", base)
	}
	if filepath.IsAbs(clean) {
		return clean, nil
	}
	return filepath.Clean(filepath.Join(repoRoot, clean)), nil
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func isLoopbackHost(host string) bool {
	normalized := strings.ToLower(strings.Trim(host, "[] "))
	if normalized == "localhost" {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "." || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func isAncestor(parent string, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child || parent == "." || child == "." {
		return false
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

func isBroadPath(path string) bool {
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) {
		return true
	}
	if strings.HasPrefix(clean, "/tmp/") || strings.HasPrefix(clean, "/private/tmp/") || strings.HasPrefix(clean, "/mnt/") {
		return false
	}
	depth := strings.Count(strings.Trim(clean, string(filepath.Separator)), string(filepath.Separator)) + 1
	return filepath.IsAbs(clean) && depth < 3
}
