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

// Response is the reply to a socket call: a result or an error message.
type Response struct {
	// Result is the operation's result. From HandleCall it is detached
	// from application state: Lines stays Lines (each line encoded), any
	// other value is its JSON encoding as a json.RawMessage, so object
	// keys keep the order the handler's value marshals in (a struct's
	// field order); a nil or null result is nil.
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// Argument is true when Error is an ArgumentError: the request was
	// refused for its arguments, before any instance was chosen.
	Argument bool `json:"argument,omitempty"`
	// Messages are what the call told the user while it ran (ctx.Notify,
	// ctx.ClearNotice, ctx.Status), in order; the runtime's sink got them
	// too. Messages posted later, by a job's Then, reach only the sink.
	Messages []UIMessage `json:"messages,omitempty"`
}

// CallMsg is delivered to the UI loop; only that loop may call Runtime.Execute.
type CallMsg struct {
	Request Request
	Reply   chan Response
	// Stream asks for a watch (the watch verb's *Watch) live, as the
	// result, for a caller that reads its events on the loop (the CLI
	// shell); without it the result is the events so far and the watch
	// ends.
	Stream bool
}

func (r *Runtime) HandleCall(msg CallMsg) {
	restore := r.Sourced("cli")
	stop := r.UI().capture()
	result, err := r.Execute(msg.Request)
	messages := stop()
	restore()
	var response Response
	if w, ok := result.(*Watch); ok && err == nil && msg.Stream {
		response = Response{Result: w} // live: the caller reads it on the loop
	} else {
		response = callResponse(result, err)
	}
	response.Messages = messages
	msg.Reply <- response
}

// callResponse is the reply for an executed call, its result detached from
// application state.
func callResponse(result any, err error) Response {
	if err != nil {
		var bad *ArgumentError
		return Response{Error: err.Error(), Argument: errors.As(err, &bad)}
	}
	// Detach live slices/maps before another message can mutate application state.
	if lines, ok := result.(Lines); ok {
		detached, err := detachLines(lines) // still Lines, fields in order
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Result: detached}
	}
	if text, ok := result.(Verbatim); ok {
		return Response{Result: text} // a string: nothing to detach
	}
	if result == nil {
		return Response{}
	}
	// Marshal once and keep the bytes: decoding them into an any would
	// sort an object's keys.
	data, err := json.Marshal(result)
	switch {
	case err != nil:
		return Response{Error: err.Error()}
	case string(data) == "null":
		return Response{}
	}
	return Response{Result: json.RawMessage(data)}
}

// Lines is a result printed as JSON Lines (one compact value per line, no
// outer array) by a CLI, so a reader can decode it as it arrives; over
// JSON it is an array. HandleCall keeps the type through its copy.
type Lines []any

// Verbatim is a result a CLI prints as its text, unquoted (a file the
// verb writes: bindings defaults); over JSON it is a string.
type Verbatim string

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

// Server serves runtime requests over a Unix socket; see Listen.
type Server struct {
	listener net.Listener
	done     chan struct{}
	once     sync.Once
}

// Listen requires a private, current-user-owned socket directory. A socket
// file left by an application that is gone (nothing answers a connect) is
// removed and listened on again; a live one is never removed.
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
	switch {
	case !info.IsDir():
		return nil, fmt.Errorf("socket parent %s is not a directory: choose a socket path in a private directory (mode 0700) you own", dir)
	case !ok || stat.Uid != uint32(os.Getuid()):
		return nil, fmt.Errorf("socket parent %s is not owned by the current user: choose a socket path in a private directory (mode 0700) you own", dir)
	case info.Mode().Perm()&0077 != 0:
		return nil, fmt.Errorf("socket parent %s is mode %04o, readable by others: run chmod 700 %s, or choose a socket path in a private directory you own", dir, info.Mode().Perm(), dir)
	}
	ln, err := net.Listen("unix", path)
	if errors.Is(err, syscall.EADDRINUSE) {
		if serr := removeStaleSocket(path); serr != nil {
			return nil, serr
		}
		ln, err = net.Listen("unix", path)
	}
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

// removeStaleSocket removes the socket file at path when no application
// answers on it (a connect is refused: its listener is gone). A socket
// that answers, or a file that is not a socket, is left alone and named in
// the error.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("listen %s: the path exists and is not a socket", path)
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		conn.Close()
		return fmt.Errorf("listen %s: another application is listening on this socket; quit it or choose another socket path", path)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("listen %s: the socket is in use and did not answer: %w", path, err)
	}
	return os.Remove(path)
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

// Call runs q in the application listening on the socket at path and
// returns its result (CallResponse keeps the messages too).
func Call(ctx context.Context, path string, q Request) (any, error) {
	response, err := CallResponse(ctx, path, q)
	if err != nil {
		return nil, err
	}
	return response.Result, nil
}

// CallResponse is Call returning the whole Response: the result and the
// Messages the call told the user. A failed call is an error.
func CallResponse(ctx context.Context, path string, q Request) (Response, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, err
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
		return Response{}, err
	}
	var response Response
	if err = json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}
