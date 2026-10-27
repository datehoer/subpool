package xlog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLogsDoNotContainURLSecrets(t *testing.T) {
	l := &Logger{level: Debug, filePath: filepath.Join(t.TempDir(), "subpool.log")}
	if err := l.openFileLocked(time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.file.Close() })
	l.logf(Info, "[listen] %s", "mixed://canary-user:canary-pass@0.0.0.0:6666")
	l.logf(Debug, "node: %s", "vmess://encoded-private-node")
	l.logf(Warn, `refresh failed: Get "%s": timeout`, "https://example.com/private-path?token=canary-token")
	b, err := os.ReadFile(l.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"canary-user", "canary-pass", "encoded-private-node", "private-path", "canary-token"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("log contains private diagnostic data: %s", secret)
		}
	}
	if !strings.Contains(string(b), "mixed://0.0.0.0:6666") {
		t.Error("listener address is missing from sanitized log")
	}
}

func TestLogFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions")
	}
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Now()
			path := filepath.Join(dir, "subpool-"+now.Format("2006-01-02")+".log")
			if existing {
				if err := os.WriteFile(path, []byte("previous log\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			l := &Logger{filePath: dir}
			if err := l.openFileLocked(now); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = l.file.Close() })
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("log permissions = %04o, want 0600", got)
			}
		})
	}
}
