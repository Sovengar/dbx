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
	LogQuery   LogLevel = "query"
	LogError   LogLevel = "error"
	LogConnect LogLevel = "connect"
	LogSchema  LogLevel = "schema"
)

type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Action    string                 `json:"action"`
	SQL       string                 `json:"sql,omitempty"`
	Duration  int64                  `json:"duration_ms,omitempty"`
	Rows      int                    `json:"rows,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type Logger struct {
	dir       string
	file      *os.File
	encoder   *json.Encoder
	retention int // days
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
			// Best-effort cleanup: skip files that cannot be removed and
			// keep removing the rest.
			_ = os.Remove(filepath.Join(l.dir, entry.Name()))
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

type SessionInfo struct {
	Date      time.Time `json:"date"`
	Filename  string    `json:"filename"`
	Entries   int       `json:"entries"`
	Queries   int       `json:"queries"`
	Errors    int       `json:"errors"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

func (r *Reader) ListSessions() ([]SessionInfo, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		_, err := entry.Info()
		if err != nil {
			continue
		}

		dateStr := strings.TrimSuffix(entry.Name(), ".jsonl")
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		si := SessionInfo{
			Date:     date,
			Filename: entry.Name(),
		}

		logEntries, err := r.Read(date)
		if err == nil {
			si.Entries = len(logEntries)
			for _, e := range logEntries {
				if e.Level == LogQuery {
					si.Queries++
				}
				if e.Level == LogError {
					si.Errors++
				}
			}
			if len(logEntries) > 0 {
				si.StartTime = logEntries[0].Timestamp
				si.EndTime = logEntries[len(logEntries)-1].Timestamp
			}
		}

		sessions = append(sessions, si)
	}

	return sessions, nil
}

func (r *Reader) ReadFile(filename string) ([]LogEntry, error) {
	path := filepath.Join(r.dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseEntries(data)
}

func (r *Reader) ReadAll() ([]LogEntry, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}

	var allEntries []LogEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		logEntries, err := r.ReadFile(entry.Name())
		if err != nil {
			continue
		}
		allEntries = append(allEntries, logEntries...)
	}

	return allEntries, nil
}

func (r *Reader) Read(date time.Time) ([]LogEntry, error) {
	filename := fmt.Sprintf("%s.jsonl", date.Format("2006-01-02"))
	data, err := os.ReadFile(filepath.Join(r.dir, filename))
	if err != nil {
		return nil, err
	}

	return parseEntries(data)
}

func parseEntries(data []byte) ([]LogEntry, error) {
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
