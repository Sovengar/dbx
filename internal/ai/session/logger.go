package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LogLevel string

const (
	LogQuery    LogLevel = "query"
	LogError    LogLevel = "error"
	LogConnect  LogLevel = "connect"
	LogSchema   LogLevel = "schema"
)

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     LogLevel  `json:"level"`
	Action    string    `json:"action"`
	SQL       string    `json:"sql,omitempty"`
	Duration  int64     `json:"duration_ms,omitempty"`
	Rows      int       `json:"rows,omitempty"`
	Error     string    `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type Logger struct {
	dir        string
	file       *os.File
	encoder    *json.Encoder
	retention  int // days
}

func NewLogger(dir string, retentionDays int) (*Logger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session dir: %w", err)
	}

	filename := filepath.Join(dir, fmt.Sprintf("%s.jsonl", time.Now().Format("2006-01-02")))
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}

	return &Logger{
		dir:       dir,
		file:      file,
		encoder:   json.NewEncoder(file),
		retention: retentionDays,
	}, nil
}

func (l *Logger) Log(entry LogEntry) error {
	entry.Timestamp = time.Now()
	return l.encoder.Encode(entry)
}

func (l *Logger) LogQuery(sql string, duration time.Duration, rows int) error {
	return l.Log(LogEntry{
		Level:    LogQuery,
		Action:   "execute",
		SQL:      sql,
		Duration: duration.Milliseconds(),
		Rows:     rows,
	})
}

func (l *Logger) LogError(sql string, err error) error {
	return l.Log(LogEntry{
		Level:  LogError,
		Action: "execute",
		SQL:    sql,
		Error:  err.Error(),
	})
}

func (l *Logger) LogConnect(database string) error {
	return l.Log(LogEntry{
		Level:    LogConnect,
		Action:   "connect",
		Metadata: map[string]interface{}{"database": database},
	})
}

func (l *Logger) LogSchema(table string) error {
	return l.Log(LogEntry{
		Level:    LogSchema,
		Action:   "schema",
		Metadata: map[string]interface{}{"table": table},
	})
}

func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *Logger) Cleanup() error {
	if l.retention <= 0 {
		return nil
	}

	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -l.retention)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(l.dir, entry.Name()))
		}
	}

	return nil
}

type Reader struct {
	dir string
}

func NewReader(dir string) *Reader {
	return &Reader{dir: dir}
}

func (r *Reader) Read(date time.Time) ([]LogEntry, error) {
	filename := filepath.Join(r.dir, fmt.Sprintf("%s.jsonl", date.Format("2006-01-02")))
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var entries []LogEntry
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for decoder.More() {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}
