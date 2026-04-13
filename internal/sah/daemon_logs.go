package sah

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	defaultDaemonStdoutMaxBytes = 20 * 1024 * 1024
	defaultDaemonStderrMaxBytes = 10 * 1024 * 1024
	defaultDaemonLogBackups     = 5
)

type DaemonLogs struct {
	Stdout io.WriteCloser
	Stderr io.WriteCloser
}

func OpenDaemonLogs(paths Paths) (*DaemonLogs, error) {
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	stdout, err := OpenRotatingFileWriter(paths.DaemonStdoutLog, defaultDaemonStdoutMaxBytes, defaultDaemonLogBackups)
	if err != nil {
		return nil, fmt.Errorf("open daemon stdout log: %w", err)
	}

	stderr, err := OpenRotatingFileWriter(paths.DaemonStderrLog, defaultDaemonStderrMaxBytes, defaultDaemonLogBackups)
	if err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("open daemon stderr log: %w", err)
	}

	return &DaemonLogs{
		Stdout: stdout,
		Stderr: stderr,
	}, nil
}

func (logs *DaemonLogs) Close() error {
	if logs == nil {
		return nil
	}

	var errs []error
	if logs.Stdout != nil {
		errs = append(errs, logs.Stdout.Close())
	}
	if logs.Stderr != nil {
		errs = append(errs, logs.Stderr.Close())
	}
	return errors.Join(errs...)
}

type RotatingFileWriter struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

func OpenRotatingFileWriter(path string, maxBytes int64, maxBackups int) (*RotatingFileWriter, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("max bytes must be positive")
	}
	if maxBackups < 0 {
		return nil, fmt.Errorf("max backups must not be negative")
	}

	writer := &RotatingFileWriter{
		path:       path,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
	}
	if err := writer.openCurrentFile(false); err != nil {
		return nil, err
	}
	return writer, nil
}

func (writer *RotatingFileWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.file == nil {
		if err := writer.openCurrentFile(false); err != nil {
			return 0, err
		}
	}
	if writer.size > 0 && writer.size+int64(len(data)) > writer.maxBytes {
		if err := writer.rotate(); err != nil {
			return 0, err
		}
	}

	written, err := writer.file.Write(data)
	writer.size += int64(written)
	return written, err
}

func (writer *RotatingFileWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.file == nil {
		return nil
	}

	err := writer.file.Close()
	writer.file = nil
	writer.size = 0
	return err
}

func (writer *RotatingFileWriter) rotate() error {
	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			return err
		}
		writer.file = nil
	}

	if writer.maxBackups == 0 {
		if err := writer.openCurrentFile(true); err != nil {
			return err
		}
		writer.size = 0
		return nil
	}

	oldest := rotatedLogPath(writer.path, writer.maxBackups)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	for index := writer.maxBackups - 1; index >= 1; index-- {
		source := rotatedLogPath(writer.path, index)
		target := rotatedLogPath(writer.path, index+1)
		if err := os.Rename(source, target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if err := os.Rename(writer.path, rotatedLogPath(writer.path, 1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := writer.openCurrentFile(true); err != nil {
		return err
	}
	writer.size = 0
	return nil
}

func (writer *RotatingFileWriter) openCurrentFile(truncate bool) error {
	if err := os.MkdirAll(filepath.Dir(writer.path), 0o755); err != nil {
		return err
	}

	flags := os.O_CREATE | os.O_WRONLY
	if truncate {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}

	file, err := os.OpenFile(writer.path, flags, 0o644)
	if err != nil {
		return err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}

	writer.file = file
	writer.size = info.Size()
	return nil
}

func rotatedLogPath(path string, index int) string {
	return fmt.Sprintf("%s.%d", path, index)
}
