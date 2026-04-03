package bootstrap

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultHTTPPort  = 8765
	envFileName      = ".env"
	envExampleName   = ".env.example"
	opencodeName     = "opencode.json"
	mcpAlias         = "rag-search-mcp"
	hostIndexDir     = "./data/index"
	hostModelsDir    = "./data/models"
	hostDocsDir      = "./data/docs"
	hostCodeDir      = "./data/code"
	hostIndexEnvKey  = "HOST_INDEX_DIR"
	hostModelsEnvKey = "HOST_MODELS_DIR"
	hostDocsEnvKey   = "HOST_DOCS_DIR"
	hostCodeEnvKey   = "HOST_CODE_DIR"
)

func EnsureEnvFile(repoRoot string) (bool, error) {
	envPath := filepath.Join(repoRoot, envFileName)
	if _, err := os.Stat(envPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat %s: %w", envPath, err)
	}

	content, err := os.ReadFile(filepath.Join(repoRoot, envExampleName))
	if err != nil {
		return false, fmt.Errorf("read .env.example: %w", err)
	}
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		return false, fmt.Errorf("write .env: %w", err)
	}
	if err := os.Chmod(envPath, 0o600); err != nil {
		return false, fmt.Errorf("chmod .env: %w", err)
	}
	return true, nil
}

func ResolvePort(repoRoot string) (int, error) {
	if value, ok := os.LookupEnv("RAG_HTTP_PORT"); ok && strings.TrimSpace(value) != "" {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("RAG_HTTP_PORT must be an integer")
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("RAG_HTTP_PORT must be between 1 and 65535")
		}
		return port, nil
	}

	envPath := filepath.Join(repoRoot, envFileName)
	file, err := os.Open(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultHTTPPort, nil
		}
		return 0, fmt.Errorf("open .env: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != "RAG_HTTP_PORT" {
			continue
		}
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if value == "" {
			return defaultHTTPPort, nil
		}
		port, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("RAG_HTTP_PORT in .env must be an integer")
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("RAG_HTTP_PORT in .env must be between 1 and 65535")
		}
		return port, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read .env: %w", err)
	}

	return defaultHTTPPort, nil
}

func EnsureHostDataDirs(repoRoot string) error {
	envValues, err := loadEnvFile(repoRoot)
	if err != nil {
		return err
	}

	docsDir, err := resolveHostDir(repoRoot, envValues, hostDocsEnvKey, hostDocsDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", hostDocsEnvKey, err)
	}
	codeDir, err := resolveHostDir(repoRoot, envValues, hostCodeEnvKey, hostCodeDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", hostCodeEnvKey, err)
	}
	indexDir, err := resolveHostDir(repoRoot, envValues, hostIndexEnvKey, hostIndexDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", hostIndexEnvKey, err)
	}
	modelsDir, err := resolveHostDir(repoRoot, envValues, hostModelsEnvKey, hostModelsDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", hostModelsEnvKey, err)
	}

	for _, dir := range []string{docsDir, codeDir, indexDir, modelsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	return nil
}

func resolveHostDir(repoRoot string, envValues map[string]string, key string, fallback string) (string, error) {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return resolveHostPath(repoRoot, value, fallback)
	}
	return resolveHostPath(repoRoot, envValues[key], fallback)
}

func loadEnvFile(repoRoot string) (map[string]string, error) {
	values := map[string]string{}
	envPath := filepath.Join(repoRoot, envFileName)

	file, err := os.Open(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return values, nil
		}
		return nil, fmt.Errorf("open .env: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if key == "" || value == "" {
			continue
		}

		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}

	return values, nil
}

func resolveHostPath(repoRoot string, configured string, fallback string) (string, error) {
	rawPath := strings.TrimSpace(configured)
	if rawPath == "" {
		rawPath = fallback
	}

	if filepath.IsAbs(rawPath) {
		return rawPath, nil
	}

	if rawPath == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	return filepath.Abs(filepath.Join(repoRoot, rawPath))
}

func UpsertOpenCodeConfig(repoRoot string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	path := filepath.Join(repoRoot, opencodeName)
	cfg := map[string]any{}

	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			backup := path + ".invalid"
			if removeErr := os.Remove(backup); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("remove stale backup: %w", removeErr)
			}
			if err := os.Rename(path, backup); err != nil {
				return fmt.Errorf("backup invalid opencode.json: %w", err)
			}
			cfg = map[string]any{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read opencode.json: %w", err)
	}

	cfg["$schema"] = "https://opencode.ai/config.json"

	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		mcp = map[string]any{}
	}
	mcp[mcpAlias] = map[string]any{
		"type":    "remote",
		"url":     fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		"enabled": true,
		"timeout": 10000,
	}
	cfg["mcp"] = mcp

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal opencode config: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("write opencode.json: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod opencode.json: %w", err)
	}
	return nil
}
