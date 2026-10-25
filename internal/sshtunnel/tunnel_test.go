package sshtunnel

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type fixedAddr string

func (a fixedAddr) Network() string { return "tcp" }
func (a fixedAddr) String() string  { return string(a) }

type inertListener struct {
	addr   net.Addr
	closed bool
}

func (l *inertListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *inertListener) Close() error {
	l.closed = true
	return nil
}
func (l *inertListener) Addr() net.Addr { return l.addr }

type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}
func (l *pipeListener) Addr() net.Addr { return fixedAddr("127.0.0.1:43210") }
func (l *pipeListener) connect(t *testing.T) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	select {
	case l.connections <- server:
		return client
	case <-l.done:
		_ = client.Close()
		_ = server.Close()
		t.Fatal("listener closed before connection")
		return nil
	}
}

func usePipeListener(t *testing.T) *pipeListener {
	t.Helper()
	l := newPipeListener()
	original := listen
	listen = func(string, string) (net.Listener, error) { return l, nil }
	t.Cleanup(func() { listen = original })
	return l
}

func TestOpenRejectsInvalidInputsAndListenerFailures(t *testing.T) {
	for _, target := range []string{"", "  ", "-oProxyCommand=bad"} {
		if _, _, err := Open("postgres://db.example/app", target); err == nil {
			t.Errorf("Open accepted SSH target %q", target)
		}
	}

	if _, _, err := Open("postgres://%", "bastion"); err == nil || !strings.Contains(err.Error(), "parse tunnel DSN") {
		t.Errorf("invalid DSN error = %v", err)
	}
	if _, _, err := Open("host=/tmp dbname=app", "bastion"); err == nil || !strings.Contains(err.Error(), "TCP database host") {
		t.Errorf("Unix-socket DSN error = %v", err)
	}

	originalListen := listen
	t.Cleanup(func() { listen = originalListen })
	want := errors.New("no sockets")
	listen = func(string, string) (net.Listener, error) { return nil, want }
	if _, _, err := Open("postgres://db.example/app", "bastion"); !errors.Is(err, want) {
		t.Errorf("listener error = %v, want %v", err, want)
	}

	bad := &inertListener{addr: fixedAddr("not-an-address")}
	listen = func(string, string) (net.Listener, error) { return bad, nil }
	if _, _, err := Open("postgres://db.example/app", "bastion"); err == nil || !strings.Contains(err.Error(), "listener address") {
		t.Errorf("bad listener address error = %v", err)
	}
	if !bad.closed {
		t.Error("listener was not closed after DSN rewriting failed")
	}
}

func TestRewriteDSNPreservesConnectionDetailsAndReplacesTheDestination(t *testing.T) {
	uri := "postgres://ada:secret@old.example:5433/app?application_name=dbx&host=override&port=9999"
	rewritten, err := rewriteDSN(uri, "127.0.0.1:43210")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgconn.ParseConfig(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 43210 || cfg.Database != "app" || cfg.User != "ada" || cfg.Password != "secret" {
		t.Errorf("rewritten config = host %q port %d database %q user %q password %q", cfg.Host, cfg.Port, cfg.Database, cfg.User, cfg.Password)
	}
	if cfg.RuntimeParams["application_name"] != "dbx" {
		t.Errorf("application_name = %q, want dbx", cfg.RuntimeParams["application_name"])
	}

	keyword, err := rewriteDSN("host=old.example port=5433 dbname=app user=ada", "127.0.0.1:43211")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = pgconn.ParseConfig(keyword)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 43211 {
		t.Errorf("keyword destination = %s:%d", cfg.Host, cfg.Port)
	}

	if _, err := rewriteDSN(uri, "bad"); err == nil {
		t.Error("rewriteDSN accepted a malformed listener address")
	}
	if _, err := rewriteDSN("postgres://%gh", "127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "rewrite tunnel DSN") {
		t.Errorf("malformed URL error = %v", err)
	}
}

func TestTunnelForwardsThroughOpenSSHAndUsesTheRemoteDSNAddress(t *testing.T) {
	listener := usePipeListener(t)
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeSSH(t, dir, "printf '%s\\n' \"$@\" > \"$DBX_ARGS\"\ncat\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DBX_ARGS", argsFile)

	rewritten, tunnel, err := Open("postgres://ada@db.internal:5544/app", "production-bastion")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgconn.ParseConfig(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 43210 {
		t.Errorf("rewritten destination = %s:%d", cfg.Host, cfg.Port)
	}
	conn := listener.connect(t)
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("forwarded bytes = %q", got)
	}
	_ = conn.Close()
	if err := tunnel.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.Close(); err != nil {
		t.Fatal(err)
	}
	if tunnel.Err() != nil {
		t.Errorf("successful tunnel error = %v", tunnel.Err())
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	joined := string(args)
	for _, want := range []string{"BatchMode=yes", "ExitOnForwardFailure=yes", "db.internal:5544", "production-bastion"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh arguments %q do not contain %q", joined, want)
		}
	}
}

func TestTunnelReportsSSHStderrAndCancellationIsClean(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		listener := usePipeListener(t)
		dir := t.TempDir()
		writeSSH(t, dir, "echo authentication-denied >&2\nexit 7\n")
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		_, tunnel, err := Open("postgres://db.internal/app", "bastion")
		if err != nil {
			t.Fatal(err)
		}
		conn := listener.connect(t)
		_ = conn.Close()
		deadline := time.Now().Add(2 * time.Second)
		for tunnel.Err() == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		_ = tunnel.Close()
		if err := tunnel.Err(); err == nil || !strings.Contains(err.Error(), "authentication-denied") || !strings.Contains(err.Error(), "bastion") {
			t.Errorf("tunnel error = %v", err)
		}
		tunnel.setErr(errors.New("later error"))
		if !strings.Contains(tunnel.Err().Error(), "authentication-denied") {
			t.Errorf("first error was replaced: %v", tunnel.Err())
		}
	})

	t.Run("close cancels an active process", func(t *testing.T) {
		listener := usePipeListener(t)
		dir := t.TempDir()
		marker := filepath.Join(dir, "started")
		writeSSH(t, dir, "touch \"$DBX_MARKER\"\nsleep 10\n")
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("DBX_MARKER", marker)
		_, tunnel, err := Open("postgres://db.internal/app", "bastion")
		if err != nil {
			t.Fatal(err)
		}
		conn := listener.connect(t)
		defer func() { _ = conn.Close() }()
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fake SSH process did not start")
			}
			time.Sleep(time.Millisecond)
		}
		_ = tunnel.Close()
		if tunnel.Err() != nil {
			t.Errorf("cancelling the tunnel recorded an SSH failure: %v", tunnel.Err())
		}
	})
}

func writeSSH(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}
