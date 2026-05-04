package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

const credentialsFile = ".baobotctl/credentials"

func Save(token string) error {
	path, err := credPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	return os.WriteFile(path, []byte(token), 0600)
}

func Load() (string, error) {
	path, err := credPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no credentials found — run 'baobotctl login': %w", err)
	}
	return string(data), nil
}

func Clear() error {
	path, err := credPath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func credPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, credentialsFile), nil
}
