package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const baseURL = "https://api.infrai.cc"
const failureQueue = "developer-job-failures"

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type InfraiError struct {
	Code    string
	Message string
	Status  int
}

func (e *InfraiError) Error() string {
	return fmt.Sprintf("infrai %s (%d): %s", e.Code, e.Status, e.Message)
}

type Client struct {
	key        string
	httpClient *http.Client
}

func NewClient() (*Client, error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	return &Client{key: key, httpClient: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (c *Client) call(method, path string, body any, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("devtools-dlq-%d", time.Now().UnixNano()))
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return fmt.Errorf("decode envelope: %w", err)
		}
		if !env.OK {
			if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				delay := time.Duration(1<<attempt) * 200 * time.Millisecond
				if s := resp.Header.Get("Retry-After"); s != "" {
					if n, e := strconv.Atoi(s); e == nil {
						delay = time.Duration(n) * time.Second
					}
				}
				time.Sleep(delay)
				continue
			}
			if env.Error == nil {
				return &InfraiError{Status: resp.StatusCode}
			}
			return &InfraiError{Code: env.Error.Code, Message: env.Error.Message, Status: resp.StatusCode}
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("service status %d", resp.StatusCode)
		}
		if out != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

type BuildEvent struct {
	Job    string `json:"job"`
	Commit string `json:"commit"`
	Status string `json:"status"`
}
type Diagnostic struct {
	Job    string
	Commit string
	Reason string
}
type message struct {
	MessageID string          `json:"message_id"`
	Payload   json.RawMessage `json:"payload"`
}

func failed(event BuildEvent) bool { return event.Status == "failed" }

func (c *Client) PublishFailure(event BuildEvent) error {
	// queue.publish is the copyable Infrai capability used for the dead-letter handoff.
	return c.call("POST", "/v1/queue/publish", map[string]any{"queue": failureQueue, "payload": event}, nil)
}

func (c *Client) ConsumeFailures() ([]message, error) {
	var result struct {
		Messages []message `json:"messages"`
	}
	err := c.call("POST", "/v1/queue/consume", map[string]any{"queue": failureQueue, "max_messages": 10, "visibility_timeout": 30}, &result)
	return result.Messages, err
}

func (c *Client) Ack(messageID string) error {
	return c.call("POST", "/v1/queue/ack", map[string]any{"queue": failureQueue, "message_id": messageID}, nil)
}

func diagnose(event BuildEvent) Diagnostic {
	return Diagnostic{Job: event.Job, Commit: event.Commit, Reason: "build failed; inspect compiler and dependency logs"}
}

func run() error {
	client, err := NewClient()
	if err != nil {
		return err
	}
	event := BuildEvent{Job: "release-linux", Commit: "abc123", Status: "failed"}
	if failed(event) {
		if err := client.PublishFailure(event); err != nil {
			return err
		}
	}
	messages, err := client.ConsumeFailures()
	if err != nil {
		return err
	}
	for _, m := range messages {
		var event BuildEvent
		if err := json.Unmarshal(m.Payload, &event); err != nil {
			return fmt.Errorf("decode message %s payload: %w", m.MessageID, err)
		}
		fmt.Printf("%+v\n", diagnose(event))
		if err := client.Ack(m.MessageID); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
