package session

import (
	"bufio"
	"bytes"
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
	// readDir is how the retention sweep lists what it is about to delete. A FIELD rather
	// than a direct os.ReadDir call for one reason, and it is the same reason the tests'
	// other guards could never be reached.
	//
	// The sweep skips an entry whose Info() fails — `if err != nil { continue }` — and that
	// only happens when the file disappears BETWEEN the listing and the stat. No fixture
	// produces it: os.ReadDir's DirEntry.Info() calls LSTAT, so a dangling symlink stats
	// fine, and a permission problem fails for EVERY entry at once, which the sweep reads as
	// "nothing to do". Only a race reaches the guard.
	//
	// So the listing is injected, and a test supplies a three-line entry whose Info() fails.
	// The behaviour is unchanged: production passes os.ReadDir and nothing else can tell.
	readDir func(string) ([]os.DirEntry, error)
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
		readDir:   os.ReadDir,
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

	entries, err := l.listDir()
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
	// readDir is the same seam as the Logger's, for the same reason: the listing's
	// per-entry Info() guard is only reachable through a race, so the listing is injected and
	// the guard is driven with an entry that cannot be stat'ed. Production passes os.ReadDir.
	readDir func(string) ([]os.DirEntry, error)
}

func NewReader(dir string) *Reader {
	return &Reader{dir: dir, readDir: os.ReadDir}
}

// listDir is os.ReadDir unless the caller supplied its own listing, which is what the tests
// do. Both call sites go through it rather than through os.ReadDir directly, because a seam
// that two of the three call sites bypass is not a seam.
func (l *Logger) listDir() ([]os.DirEntry, error) {
	if l.readDir != nil {
		return l.readDir(l.dir)
	}
	return os.ReadDir(l.dir)
}

func (r *Reader) listDir() ([]os.DirEntry, error) {
	if r.readDir != nil {
		return r.readDir(r.dir)
	}
	return os.ReadDir(r.dir)
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
	entries, err := r.listDir()
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
	entries, err := r.listDir()
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

// maxLogLine bounds one entry's line. A query's SQL is the only unbounded field and
// a megabyte of it is already unreadable in a picker, so a longer line is a corrupt
// file rather than a real entry.
const maxLogLine = 1 << 20

// parseEntries reads a JSONL log: one JSON object per line, as written by
// json.Encoder in Logger.Log.
//
// It reads line by line rather than streaming a json.Decoder over the whole buffer.
// The streaming form does not terminate on a malformed line: Decode returns a syntax
// error WITHOUT advancing the reader, so More() stays true, `continue` comes straight
// back around, and the loop spins forever. Inputs that trigger it are ordinary
// accident rather than adversarial: "garbage", a truncated "{", a binary file that
// happens to be named YYYY-MM-DD.jsonl. A log written by a process that was killed
// mid-write is exactly the case this file format is supposed to survive, so it was
// also the case most likely to hang.
//
// A bad line is skipped and the rest are read, which is the whole point: one
// corrupt entry must not make a session unreadable. That contract is unchanged from
// the streaming version; only the way it terminates is new.
func parseEntries(data []byte) ([]LogEntry, error) {
	var entries []LogEntry

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxLogLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	// A line over the limit makes Scan stop. Whatever was read up to that point is
	// still returned: a truncated tail is exactly the case this function exists to
	// survive, and reporting an error would make every caller treat a long-but-good
	// prefix as a failure.
	return entries, nil
}
