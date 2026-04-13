package sah

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRotatingFileWriterRotatesAndKeepsRecentBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.stdout.log")

	writer, err := OpenRotatingFileWriter(path, 10, 2)
	if err != nil {
		t.Fatalf("OpenRotatingFileWriter returned error: %v", err)
	}
	defer func() {
		_ = writer.Close()
	}()

	for _, chunk := range []string{"aaaaa\n", "bbbbb\n", "ccccc\n", "ddddd\n"} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	assertFileContents(t, path, "ddddd\n")
	assertFileContents(t, path+".1", "ccccc\n")
	assertFileContents(t, path+".2", "bbbbb\n")
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("expected oldest backup to be removed, got err=%v", err)
	}
}

func TestOpenDaemonLogsUsesDedicatedDaemonLogPaths(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		LogsDir:         root,
		DaemonStdoutLog: filepath.Join(root, "daemon.stdout.log"),
		DaemonStderrLog: filepath.Join(root, "daemon.stderr.log"),
	}

	logs, err := OpenDaemonLogs(paths)
	if err != nil {
		t.Fatalf("OpenDaemonLogs returned error: %v", err)
	}
	defer func() {
		_ = logs.Close()
	}()

	if _, err := logs.Stdout.Write([]byte("stdout\n")); err != nil {
		t.Fatalf("stdout write returned error: %v", err)
	}
	if _, err := logs.Stderr.Write([]byte("stderr\n")); err != nil {
		t.Fatalf("stderr write returned error: %v", err)
	}

	assertFileContents(t, paths.DaemonStdoutLog, "stdout\n")
	assertFileContents(t, paths.DaemonStderrLog, "stderr\n")
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("unexpected contents for %s: got %q want %q", path, string(data), want)
	}
}
