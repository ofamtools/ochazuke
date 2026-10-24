package ochazuke

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const maxRequestBytes = 1 << 20

type Response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CallMsg is delivered to the UI loop; only that loop may call Runtime.Execute.
type CallMsg struct {
	Request Request
	Reply   chan Response
}

func (r *Runtime) HandleCall(msg CallMsg) {
	restore := r.Sourced("cli")
	result, err := r.Execute(msg.Request)
	restore()
	response := Response{Result: result}
	if err != nil {
		response = Response{Error: err.Error()}
	}
	// Detach live slices/maps before another message can mutate application state.
	if lines, ok := result.(Lines); ok && response.Error == "" {
		response.Result, err = detachLines(lines) // still Lines, fields in order
		if err != nil {
			response = Response{Error: err.Error()}
		}
		msg.Reply <- response
		return
	}
	data, err := json.Marshal(response)
	if err != nil {
		response = Response{Error: err.Error()}
	} else if err = json.Unmarshal(data, &response); err != nil {
		response = Response{Error: err.Error()}
	}
	msg.Reply <- response
}

// Lines is a result printed as JSON Lines (one compact value per line, no
// outer array) by a CLI, so a reader can decode it as it arrives; over
// JSON it is an array. HandleCall keeps the type through its copy.
type Lines []any

// detachLines copies lines as encoded JSON values.
func detachLines(lines Lines) (Lines, error) {
	out := make(Lines, len(lines))
	for i, v := range lines {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[i] = json.RawMessage(data)
	}
	return out, nil
}

type Server struct {
	listener net.Listener
	done     chan struct{}
	once     sync.Once
}

// Listen requires a private, current-user-owned socket directory and never removes an existing socket.
func Listen(path string, send func(CallMsg)) (*Server, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("socket parent must be a private directory owned by the current user (mode 0700)")
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{listener: ln, done: make(chan struct{})}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, send)
		}
	}()
	return s, nil
}
func (s *Server) Close() error {
	var err error
	s.once.Do(func() { close(s.done); err = s.listener.Close() })
	return err
}
func (s *Server) serve(conn net.Conn, send func(CallMsg)) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), maxRequestBytes)
	if !scanner.Scan() {
		return
	}
	var q Request
	if err := decodeRequest(scanner.Bytes(), &q); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: err.Error()})
		return
	}
	reply := make(chan Response, 1)
	send(CallMsg{Request: q, Reply: reply})
	select {
	case response := <-reply:
		_ = json.NewEncoder(conn).Encode(response)
	case <-s.done:
		select {
		case response := <-reply:
			_ = json.NewEncoder(conn).Encode(response)
		default:
			_ = json.NewEncoder(conn).Encode(Response{Error: "application is shutting down"})
		}
	case <-time.After(10 * time.Second):
		_ = json.NewEncoder(conn).Encode(Response{Error: "application did not answer within 10 seconds"})
	}
}

func Call(ctx context.Context, path string, q Request) (any, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = json.NewEncoder(conn).Encode(q); err != nil {
		return nil, err
	}
	var response Response
	if err = json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, err
	}
	if response.Error != "" {
		return nil, fmt.Errorf("%s", response.Error)
	}
	return response.Result, nil
}
