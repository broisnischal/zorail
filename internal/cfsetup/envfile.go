package cfsetup

import (
	"os"
	"path/filepath"
)

// RepoEnvFile returns the dotenv path the Zorail server actually reads. It walks
// up from the working directory looking for a go.mod (the repo root, where the
// server runs and auto-loads ./.env) and returns <root>/.env. If no go.mod is
// found — e.g. a distributed binary run outside the source tree — it falls back
// to ./.env. This keeps `zorail setup` (often launched from bin/) and the server
// pointed at the same file, instead of silently writing bin/.env.
func RepoEnvFile() string {
	dir, err := os.Getwd()
	if err != nil {
		return ".env"
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, ".env")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ".env" // reached filesystem root, no go.mod
		}
		dir = parent
	}
}

// writeServerEnv upserts each key/value into the dotenv file at path, in a
// stable order so the file stays readable across runs.
func writeServerEnv(path string, kv [][2]string) error {
	for _, pair := range kv {
		if err := upsertEnvValue(path, pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}
