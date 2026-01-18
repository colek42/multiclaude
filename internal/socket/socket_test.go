package socket

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientServerCommunication(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create handler
	handler := HandlerFunc(func(req Request) Response {
		if req.Command == "test" {
			return Response{
				Success: true,
				Data:    "test response",
			}
		}
		return Response{
			Success: false,
			Error:   "unknown command",
		}
	})

	// Start server
	server := NewServer(sockPath, handler)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer server.Stop()

	// Run server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create client and send request
	client := NewClient(sockPath)
	req := Request{
		Command: "test",
		Args: map[string]interface{}{
			"key": "value",
		},
	}

	resp, err := client.Send(req)
	if err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	if !resp.Success {
		t.Errorf("Response.Success = false, want true")
	}

	if resp.Data != "test response" {
		t.Errorf("Response.Data = %q, want %q", resp.Data, "test response")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Check for server errors (expect closed connection error)
	select {
	case err := <-errCh:
		// Server should fail with "use of closed network connection" when stopped
		// This is expected and not an error
		_ = err
	case <-time.After(time.Second):
		// Server stopped cleanly
	}
}

func TestServerMultipleRequests(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create counter handler
	counter := 0
	handler := HandlerFunc(func(req Request) Response {
		counter++
		return Response{
			Success: true,
			Data:    counter,
		}
	})

	// Start server
	server := NewServer(sockPath, handler)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer server.Stop()

	go server.Serve()
	time.Sleep(100 * time.Millisecond)

	// Send multiple requests
	client := NewClient(sockPath)
	for i := 1; i <= 5; i++ {
		req := Request{Command: "test"}
		resp, err := client.Send(req)
		if err != nil {
			t.Fatalf("Send(%d) failed: %v", i, err)
		}

		if !resp.Success {
			t.Errorf("Request %d: Success = false", i)
		}

		// Data should be float64 due to JSON unmarshaling of numbers
		if data, ok := resp.Data.(float64); !ok || int(data) != i {
			t.Errorf("Request %d: Data = %v, want %d", i, resp.Data, i)
		}
	}
}

func TestServerErrorResponse(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create handler that returns error
	handler := HandlerFunc(func(req Request) Response {
		return Response{
			Success: false,
			Error:   "something went wrong",
		}
	})

	// Start server
	server := NewServer(sockPath, handler)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer server.Stop()

	go server.Serve()
	time.Sleep(100 * time.Millisecond)

	// Send request
	client := NewClient(sockPath)
	req := Request{Command: "test"}
	resp, err := client.Send(req)
	if err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	if resp.Success {
		t.Error("Response.Success = true, want false")
	}

	if resp.Error != "something went wrong" {
		t.Errorf("Response.Error = %q, want %q", resp.Error, "something went wrong")
	}
}

func TestClientConnectionFailure(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "nonexistent.sock")

	client := NewClient(sockPath)
	req := Request{Command: "test"}

	_, err := client.Send(req)
	if err == nil {
		t.Error("Send() succeeded when server not running")
	}
}

func TestServerRequestWithArgs(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create handler that echoes args
	handler := HandlerFunc(func(req Request) Response {
		if name, ok := req.Args["name"].(string); ok {
			return Response{
				Success: true,
				Data:    "Hello, " + name,
			}
		}
		return Response{
			Success: false,
			Error:   "missing name",
		}
	})

	// Start server
	server := NewServer(sockPath, handler)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer server.Stop()

	go server.Serve()
	time.Sleep(100 * time.Millisecond)

	// Send request with args
	client := NewClient(sockPath)
	req := Request{
		Command: "greet",
		Args: map[string]interface{}{
			"name": "Alice",
		},
	}

	resp, err := client.Send(req)
	if err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	if !resp.Success {
		t.Errorf("Response.Success = false, want true")
	}

	if resp.Data != "Hello, Alice" {
		t.Errorf("Response.Data = %q, want %q", resp.Data, "Hello, Alice")
	}
}

func TestServerStaleSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create a stale socket file
	if err := os.WriteFile(sockPath, []byte{}, 0600); err != nil {
		t.Fatalf("Failed to create stale socket: %v", err)
	}

	// Server should remove stale socket and start successfully
	handler := HandlerFunc(func(req Request) Response {
		return Response{Success: true}
	})

	server := NewServer(sockPath, handler)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed with stale socket: %v", err)
	}
	defer server.Stop()
}
