package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// settleDelay is how long we wait after opening the first document, to give
// the server a chance to announce background work before we start querying.
const settleDelay = 300 * time.Millisecond

// message is a JSON-RPC 2.0 request with ID and Method, notification with
// only Method, or response with ID and Result or Error.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

type response struct {
	result json.RawMessage
	err    *rpcError
}

// client talks JSON-RPC 2.0 to a language server over its stdin and stdout,
// with Content-Length headers for framing.
type client struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  *tailBuffer
	timeout time.Duration

	wmu sync.Mutex // so writes to stdin don't interleave

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan response
	progress map[string]bool // work-done tokens that were created or begun but haven't ended
	sawWork  bool
	changed  chan struct{} // we close and replace this each time progress changes
	// nil rejects workspace/applyEdit requests
	onApplyEdit func(workspaceEdit) error
	folders     []workspaceFolder

	done    chan struct{} // closed after the server process exits
	exitErr error
}

// tailBuffer keeps the last bytes the server wrote to stderr, so we can put
// them in error messages.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailSize = 2048

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = t.buf[len(t.buf)-tailSize:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.TrimSpace(string(t.buf))
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	return s
}

func startClient(argv []string, dir string, timeout time.Duration, folders []workspaceFolder) (*client, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	c := &client{
		name:     argv[0],
		cmd:      cmd,
		stderr:   &tailBuffer{},
		timeout:  timeout,
		pending:  map[int64]chan response{},
		progress: map[string]bool{},
		changed:  make(chan struct{}),
		done:     make(chan struct{}),
		folders:  folders,
	}
	cmd.Stderr = c.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c.stdin = stdin
	go c.readLoop(stdout)
	return c, nil
}

func (c *client) readLoop(stdout io.Reader) {
	r := bufio.NewReader(stdout)
	var readErr error
	for {
		data, err := readMessage(r)
		if err != nil {
			readErr = err
			break
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			readErr = fmt.Errorf("invalid message from server: %w", err)
			break
		}
		c.dispatch(&m)
	}
	waitErr := c.cmd.Wait()
	c.mu.Lock()
	switch {
	case readErr != nil && !errors.Is(readErr, io.EOF):
		c.exitErr = readErr
	case waitErr != nil:
		c.exitErr = fmt.Errorf("server exited: %w", waitErr)
	default:
		c.exitErr = errors.New("server exited")
	}
	if tail := c.stderr.String(); tail != "" {
		c.exitErr = fmt.Errorf("%w (stderr: %s)", c.exitErr, tail)
	}
	pending := c.pending
	c.pending = map[int64]chan response{}
	c.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
	close(c.done)
}

// readMessage reads the body of a Content-Length framed message.
func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" && length < 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("reading message header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if length < 0 {
				continue
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length header %q", line)
			}
			length = n
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("reading message body: %w", err)
	}
	return body, nil
}

func writeMessage(w io.Writer, m *message) error {
	m.JSONRPC = "2.0"
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func (c *client) write(m *message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return writeMessage(c.stdin, m)
}

func (c *client) dispatch(m *message) {
	hasID := len(m.ID) > 0 && string(m.ID) != "null"
	switch {
	case hasID && m.Method != "":
		// We answer from another goroutine, because if the server is blocked
		// writing to us, our write to its stdin would deadlock.
		go c.answer(m)
	case hasID:
		id, err := strconv.ParseInt(string(m.ID), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- response{result: m.Result, err: m.Error}
		}
	case m.Method == "$/progress":
		c.trackProgress(m.Params)
	}
}

func (c *client) answer(m *message) {
	reply := &message{ID: m.ID}
	var result any
	switch m.Method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(m.Params, &p)
		result = make([]any, len(p.Items))
	case "window/workDoneProgress/create":
		var p struct {
			Token json.RawMessage `json:"token"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			reply.Error = &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
			break
		}
		c.setProgress(string(p.Token), true)
	case "client/registerCapability", "client/unregisterCapability", "window/showMessageRequest",
		"window/showDocument", "workspace/codeLens/refresh", "workspace/semanticTokens/refresh",
		"workspace/inlayHint/refresh", "workspace/diagnostic/refresh", "workspace/inlineValue/refresh":
	case "workspace/workspaceFolders":
		result = c.folders
	case "workspace/applyEdit":
		result = c.applyEdit(m.Params)
	default:
		reply.Error = &rpcError{Code: -32601, Message: "method not supported by myoo: " + m.Method}
	}
	if reply.Error == nil {
		data, _ := json.Marshal(result)
		reply.Result = data
	}
	_ = c.write(reply)
}

type applyEditResult struct {
	Applied       bool   `json:"applied"`
	FailureReason string `json:"failureReason,omitempty"`
}

func (c *client) applyEdit(params json.RawMessage) applyEditResult {
	var p struct {
		Edit workspaceEdit `json:"edit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return applyEditResult{FailureReason: err.Error()}
	}
	c.mu.Lock()
	apply := c.onApplyEdit
	c.mu.Unlock()
	if apply == nil {
		return applyEditResult{FailureReason: "myoo only applies edits during a rename"}
	}
	if err := apply(p.Edit); err != nil {
		return applyEditResult{FailureReason: err.Error()}
	}
	return applyEditResult{Applied: true}
}

func (c *client) setApplyEdit(fn func(workspaceEdit) error) {
	c.mu.Lock()
	c.onApplyEdit = fn
	c.mu.Unlock()
}

func (c *client) trackProgress(params json.RawMessage) {
	var p struct {
		Token json.RawMessage `json:"token"`
		Value struct {
			Kind string `json:"kind"`
		} `json:"value"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	switch p.Value.Kind {
	case "begin":
		c.setProgress(string(p.Token), true)
	case "end":
		c.setProgress(string(p.Token), false)
	}
}

func (c *client) setProgress(token string, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active {
		c.progress[token] = true
		c.sawWork = true
	} else {
		delete(c.progress, token)
	}
	close(c.changed)
	c.changed = make(chan struct{})
}

// waitIdle blocks until all announced work-done progress has ended. If the
// server hasn't announced any work yet, we first give it settleDelay to do
// so.
func (c *client) waitIdle(ctx context.Context) error {
	settle := time.NewTimer(settleDelay)
	defer settle.Stop()
	settled := false
	for {
		c.mu.Lock()
		busy, saw, changed := len(c.progress) > 0, c.sawWork, c.changed
		c.mu.Unlock()
		if !busy && (saw || settled) {
			return nil
		}
		select {
		case <-changed:
		case <-settle.C:
			settled = true
		case <-c.done:
			return c.exitError()
		case <-ctx.Done():
			return c.timeoutError(ctx, "background work")
		}
	}
}

func (c *client) exitError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

func (c *client) timeoutError(ctx context.Context, what string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s waiting for %s, raise --timeout", c.timeout, what)
	}
	return ctx.Err()
}

// call sends a request and, if result isn't nil, decodes the result into it.
func (c *client) call(ctx context.Context, method string, params, result any) error {
	data, err := marshalParams(params)
	if err != nil {
		return err
	}
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.exitErr != nil {
		err := c.exitErr
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(&message{ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method, Params: data}); err != nil {
		c.forget(id)
		select {
		case <-c.done:
			return c.exitError()
		case <-time.After(time.Second):
			return fmt.Errorf("sending %s: %w", method, err)
		}
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return c.exitError()
		}
		if resp.err != nil {
			return fmt.Errorf("%s failed: %w", method, resp.err)
		}
		if result == nil || len(resp.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.result, result); err != nil {
			return fmt.Errorf("invalid %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		_ = c.notify("$/cancelRequest", map[string]int64{"id": id})
		return c.timeoutError(ctx, "an answer to "+method)
	}
}

func (c *client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (c *client) notify(method string, params any) error {
	data, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.write(&message{Method: method, Params: data})
}

// marshalParams encodes params. Nil params stay nil so they're left out of
// the message.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}

// close asks the server to shut down and exit, and kills it if it hasn't
// exited after a short grace period.
func (c *client) close() {
	if !isClosed(c.done) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if c.call(ctx, "shutdown", nil, nil) == nil {
			_ = c.notify("exit", nil)
		}
		cancel()
		_ = c.stdin.Close()
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
			_ = c.cmd.Process.Signal(os.Kill)
			<-c.done
		}
	}
}
