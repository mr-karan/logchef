package notify

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a minimal SMTP server. It refuses the recipients in refuse
// with the given reply, and after DATA it either accepts the message or,
// with dropAfterData, takes it and closes the connection without a reply.
type fakeSMTP struct {
	ln            net.Listener
	mu            sync.Mutex
	refuse        map[string]string
	dropAfterData bool
	rcpts         []string // every RCPT the server saw
	received      []string // the recipient of every message whose DATA ended
	messages      []string // the content of every message whose DATA ended
	wg            sync.WaitGroup
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, refuse: map[string]string{}}
	f.wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			f.wg.Go(func() { f.serve(conn) })
		}
	})
	t.Cleanup(func() { _ = ln.Close(); f.wg.Wait() })
	return f
}

func (f *fakeSMTP) options() MailerOptions {
	_, port, _ := net.SplitHostPort(f.ln.Addr().String())
	n, _ := strconv.Atoi(port)
	return MailerOptions{Host: "127.0.0.1", Port: n, From: "logchef@example.com", Security: "none", Timeout: 2 * time.Second}
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply("220 fake")
	var rcpt string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250 fake")
		case strings.HasPrefix(cmd, "MAIL"):
			if !validReversePath(line) {
				reply("501 5.5.4 Syntax error in parameters or arguments (invalid FROM parameter)")
				continue
			}
			reply("250 ok")
		case strings.HasPrefix(cmd, "RCPT"):
			rcpt = strings.Trim(strings.TrimSpace(line[strings.Index(line, ":")+1:]), "<>")
			f.mu.Lock()
			f.rcpts = append(f.rcpts, rcpt)
			refusal, refused := f.refuse[rcpt]
			f.mu.Unlock()
			if refused {
				reply(refusal)
			} else {
				reply("250 ok")
			}
		case cmd == "DATA":
			reply("354 go ahead")
			var msg strings.Builder
			for {
				body, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if body == ".\r\n" {
					break
				}
				msg.WriteString(body)
			}
			f.mu.Lock()
			f.received = append(f.received, rcpt)
			f.messages = append(f.messages, msg.String())
			drop := f.dropAfterData
			f.mu.Unlock()
			if drop {
				return
			}
			reply("250 queued")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

// validReversePath reports whether a MAIL command names a bare address, as a
// strict server such as Mailpit requires: "MAIL FROM:<a@b>", never a display
// name inside the brackets.
func validReversePath(line string) bool {
	_, arg, _ := strings.Cut(line, ":")
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return false
	}
	path, ok := strings.CutPrefix(fields[0], "<")
	if !ok {
		return false
	}
	path, ok = strings.CutSuffix(path, ">")
	return ok && !strings.ContainsAny(path, "<>") && strings.Contains(path, "@")
}

func (f *fakeSMTP) snapshot() (rcpts, received []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.rcpts...), append([]string(nil), f.received...)
}
