package lsp

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
)

// progressToken preserves the distinction between integer and string tokens.
type progressToken string

// workDoneProgress tracks observed operations, not workspace readiness.
// Creation alone is not work: only begin/end change the active set.
type workDoneProgress struct {
	mu      sync.Mutex
	active  map[progressToken]struct{}
	changed chan struct{}
}

func (p *workDoneProgress) observe(params json.RawMessage) {
	var notification struct {
		Token json.RawMessage `json:"token"`
		Value struct {
			Kind  string  `json:"kind"`
			Title *string `json:"title"`
		} `json:"value"`
	}
	if json.Unmarshal(params, &notification) != nil {
		return
	}
	token, valid := parseProgressToken(notification.Token)
	if !valid {
		return
	}
	kind := notification.Value.Kind
	if kind != "end" && (kind != "begin" || notification.Value.Title == nil) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, active := p.active[token]
	if kind == "begin" && !active {
		if p.active == nil {
			p.active = make(map[progressToken]struct{})
		}
		p.active[token] = struct{}{}
	} else if kind == "end" && active {
		delete(p.active, token)
	} else {
		return
	}
	if p.changed != nil {
		close(p.changed)
	}
	p.changed = make(chan struct{})
}

func (c *Client) waitForProgress(ctx context.Context) error {
	p := &c.progress
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-c.closed:
			return c.Err()
		default:
		}
		p.mu.Lock()
		active, changed := len(p.active), p.changed
		p.mu.Unlock()
		if active == 0 {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closed:
			return c.Err()
		}
	}
}

func parseProgressToken(raw json.RawMessage) (progressToken, bool) {
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return "", false
		}
		return progressToken("s:" + text), true
	}
	var number int32
	if string(raw) == "null" || json.Unmarshal(raw, &number) != nil {
		return "", false
	}
	return progressToken("n:" + strconv.FormatInt(int64(number), 10)), true
}

func progressCreateToken(params json.RawMessage) (progressToken, bool) {
	var request struct {
		Token json.RawMessage `json:"token"`
	}
	if json.Unmarshal(params, &request) != nil {
		return "", false
	}
	return parseProgressToken(request.Token)
}
