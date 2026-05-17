package main_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestServerReadyLineAndStdinShutdown is the end-to-end handshake test described
// in §2 task 2.4 of the standalone-desktop-app change: spawn the binary with
// WEBRTS_PORT=0, scrape NOMADS_READY for the assigned port, hit /health on it,
// close stdin, assert the process exits within 5s.
func TestServerReadyLineAndStdinShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: needs to build and spawn the api binary")
	}

	// Build the server binary into a temp dir.
	tmpDir := t.TempDir()
	binName := "api"
	if runtime.GOOS == "windows" {
		binName = "api.exe"
	}
	binPath := filepath.Join(tmpDir, binName)
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(), "WEBRTS_PORT=0")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	// Inherit stderr so test output shows server log lines on failure.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	readyURL, err := scrapeReadyURL(stdout, 10*time.Second)
	if err != nil {
		t.Fatalf("scrape NOMADS_READY: %v", err)
	}
	if readyURL == "" {
		t.Fatal("readyURL empty")
	}
	t.Logf("server ready at %s", readyURL)

	// Hit /health to confirm the assigned port is actually serving.
	healthClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := healthClient.Get(readyURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health status = %d, want 200", resp.StatusCode)
	}

	// Drain remaining stdout so the child doesn't block writing later log lines.
	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	// Close stdin → the watchStdin goroutine sees EOF → stop() → graceful shutdown.
	if err := stdin.Close(); err != nil {
		t.Fatalf("stdin Close: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case waitErr := <-waitDone:
		// We expect a clean exit. log.Fatal would be an exit code 1 so log it
		// but don't fail unless the process couldn't exit at all.
		if waitErr != nil {
			t.Logf("cmd.Wait returned: %v (acceptable for clean shutdown on some platforms)", waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit within 5s of stdin close")
	}
}

// scrapeReadyURL reads stdout until it sees a NOMADS_READY line and extracts
// the url=... value, or returns an error if no such line appears before timeout.
func scrapeReadyURL(r io.Reader, timeout time.Duration) (string, error) {
	type result struct {
		url string
		err error
	}
	out := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "NOMADS_READY ") {
				continue
			}
			for _, part := range strings.Fields(line) {
				if v, ok := strings.CutPrefix(part, "url="); ok {
					out <- result{url: v}
					return
				}
			}
			out <- result{err: fmt.Errorf("NOMADS_READY line had no url= field: %q", line)}
			return
		}
		out <- result{err: fmt.Errorf("stdout closed before NOMADS_READY: %v", scanner.Err())}
	}()

	select {
	case r := <-out:
		return r.url, r.err
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout after %s waiting for NOMADS_READY", timeout)
	}
}
