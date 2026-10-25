// Package sshtunnel forwards PostgreSQL connections through the user's
// OpenSSH client. Using the client, rather than reimplementing its config,
// preserves Host aliases, ProxyJump, agents, keys, and known-hosts policy.
package sshtunnel

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var listen = net.Listen

// Tunnel owns a loopback listener and every SSH process serving it.
type Tunnel struct {
	listener net.Listener
	remote   string
	via      string
	ctx      context.Context
	cancel   context.CancelFunc

	wg        sync.WaitGroup
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

// Open starts a loopback forward to the host and port in dsn through via. The
// returned DSN points at the listener and is ready to pass to PostgreSQL.
func Open(dsn, via string) (string, *Tunnel, error) {
	via = strings.TrimSpace(via)
	if via == "" || strings.HasPrefix(via, "-") {
		return "", nil, fmt.Errorf("invalid SSH tunnel target %q", via)
	}

	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return "", nil, fmt.Errorf("parse tunnel DSN: %w", err)
	}
	if cfg.Host == "" || strings.HasPrefix(cfg.Host, "/") {
		return "", nil, fmt.Errorf("SSH tunnel requires a TCP database host, got %q", cfg.Host)
	}

	listener, err := listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("open SSH tunnel listener: %w", err)
	}

	rewritten, err := rewriteDSN(dsn, listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return "", nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	t := &Tunnel{
		listener: listener,
		remote:   net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		via:      via,
		ctx:      ctx,
		cancel:   cancel,
	}
	t.wg.Add(1)
	go t.accept()
	return rewritten, t, nil
}

func rewriteDSN(dsn, localAddr string) (string, error) {
	host, port, err := net.SplitHostPort(localAddr)
	if err != nil {
		return "", fmt.Errorf("read SSH tunnel listener address: %w", err)
	}

	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("rewrite tunnel DSN: %w", err)
		}
		u.Host = net.JoinHostPort(host, port)
		query := u.Query()
		query.Del("host")
		query.Del("port")
		u.RawQuery = query.Encode()
		return u.String(), nil
	}

	return fmt.Sprintf("%s host=%s port=%s", dsn, host, port), nil
}

func (t *Tunnel) accept() {
	defer t.wg.Done()
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return
		}
		t.wg.Add(1)
		go t.forward(conn)
	}
}

func (t *Tunnel) forward(conn net.Conn) {
	defer t.wg.Done()
	defer func() { _ = conn.Close() }()

	cmd := exec.CommandContext(t.ctx, "ssh",
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-W", t.remote,
		t.via,
	)
	cmd.Stdin = conn
	cmd.Stdout = conn
	// When SSH exits before PostgreSQL closes its local socket (for example on
	// authentication failure), os/exec's stdin copier would otherwise wait on
	// that socket forever and hide the real SSH error.
	cmd.WaitDelay = 100 * time.Millisecond
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	finished := make(chan struct{})
	go func() {
		select {
		case <-t.ctx.Done():
			_ = conn.Close()
		case <-finished:
		}
	}()
	err := cmd.Run()
	close(finished)
	if err != nil && t.ctx.Err() == nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
		t.setErr(fmt.Errorf("SSH tunnel via %q failed: %w", t.via, err))
	}
}

func (t *Tunnel) setErr(err error) {
	t.errMu.Lock()
	defer t.errMu.Unlock()
	if t.err == nil {
		t.err = err
	}
}

// Err reports the first SSH forwarding failure, if one occurred.
func (t *Tunnel) Err() error {
	t.errMu.Lock()
	defer t.errMu.Unlock()
	return t.err
}

// Close stops accepting connections and terminates active SSH processes.
func (t *Tunnel) Close() error {
	t.closeOnce.Do(func() {
		t.cancel()
		_ = t.listener.Close()
		t.wg.Wait()
	})
	return nil
}
