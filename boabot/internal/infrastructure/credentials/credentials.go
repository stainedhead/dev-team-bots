// Package credentials provides a minimal INI-format credentials file parser
// for ~/.boabot/credentials, following the same convention as the AWS CLI.
package credentials

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultPath returns the default credentials file path: ~/.boabot/credentials.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("credentials: determine home dir: %w", err)
	}
	return filepath.Join(home, ".boabot", "credentials"), nil
}

// Load reads the INI credentials file at path, selects the active profile
// (from BOABOT_PROFILE env var, default "default"), and returns a flat map of
// key → value for that profile.
//
// If the file does not exist, Load returns an empty map and nil error —
// boabot must still start if credentials come from env vars.
//
// If the file exists and is world-readable (mode & 0o004 != 0), Load returns
// an error — boabot must refuse to start.
func Load(path string) (map[string]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("credentials: stat %s: %w", path, err)
	}

	if info.Mode()&0o004 != 0 {
		return nil, fmt.Errorf(
			"credentials: file %s is world-readable (mode %s); fix permissions with: chmod 600 %s",
			path, info.Mode(), path,
		)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("credentials: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	profile := activeProfile()
	return parseINI(f, profile)
}

// activeProfile returns the profile name from BOABOT_PROFILE, defaulting to
// "default".
func activeProfile() string {
	if p := os.Getenv("BOABOT_PROFILE"); p != "" {
		return p
	}
	return "default"
}

// parseINI reads an INI-format reader and returns the key-value pairs for the
// named profile.  Unknown profiles yield an empty map with nil error.
func parseINI(r interface{ Read([]byte) (int, error) }, profile string) (map[string]string, error) {
	result := map[string]string{}
	currentSection := ""
	inTarget := false

	scanner := bufio.NewScanner(readerFrom(r))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header.
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(line[1 : len(line)-1])
			inTarget = currentSection == profile
			continue
		}

		// Key-value pair.
		if inTarget {
			idx := strings.IndexByte(line, '=')
			if idx < 0 {
				continue // malformed line; skip
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" {
				result[key] = val
			}
		}
	}
	_ = currentSection // suppress unused warning

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("credentials: read file: %w", err)
	}
	return result, nil
}

// readerFrom adapts the minimal Read interface to io.Reader so bufio.Scanner
// can use it.  We accept the Read-only interface to keep parseINI testable with
// strings.Reader without importing io.
type ioReader struct {
	r interface{ Read([]byte) (int, error) }
}

func (r *ioReader) Read(p []byte) (int, error) { return r.r.Read(p) }

func readerFrom(r interface{ Read([]byte) (int, error) }) *ioReader {
	return &ioReader{r: r}
}

// Get returns the value for key from the credentials map, falling back to the
// environment variable envVar if the key is absent or empty.
func Get(creds map[string]string, key, envVar string) string {
	if v, ok := creds[key]; ok && v != "" {
		return v
	}
	if envVar != "" {
		return os.Getenv(envVar)
	}
	return ""
}
