package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/liansishen/go-webssh/internal/ws"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("GOWEBSSH_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"go-webssh-cli"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func cliCommand(t *testing.T, env map[string]string, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GOWEBSSH_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GOWEBSSH_TEST_PROCESS=1")
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd
}

func TestCLITunnelEnvironmentRelaysBytes(t *testing.T) {
	input := []byte{'S', 'S', 'H', '-', 0, 0xff, '\r', '\n'}
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "environment only"},
		{name: "environment mode with destination arguments", args: []string{"other.example.com", "2222"}},
		{name: "legacy explicit mode", args: []string{"--stdio", "other.example.com", "2222"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := make(chan ws.TunnelConnectData, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/login":
					http.SetCookie(w, &http.Cookie{Name: "gowebssh_session", Value: "token", Path: "/"})
					_, _ = w.Write([]byte(`{"ok":true}`))
				case "/api/ws/tunnel":
					cookie, err := r.Cookie("gowebssh_session")
					if err != nil || cookie.Value != "token" {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
					conn, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer conn.Close()
					_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
					_, payload, err := conn.ReadMessage()
					if err != nil {
						return
					}
					message, err := ws.DecodeMessage(payload)
					if err != nil || message.Type != "tunnel-connect" {
						return
					}
					var destination ws.TunnelConnectData
					if json.Unmarshal(message.Data, &destination) != nil {
						return
					}
					target <- destination
					connected, _ := ws.EncodeMessage("tunnel-connected", ws.TunnelConnectedData{Host: destination.Host, Port: destination.Port})
					if conn.WriteMessage(websocket.TextMessage, connected) != nil {
						return
					}
					for {
						kind, payload, err := conn.ReadMessage()
						if err != nil {
							return
						}
						if kind == websocket.BinaryMessage {
							if conn.WriteMessage(kind, payload) != nil {
								return
							}
							continue
						}
						message, err := ws.DecodeMessage(payload)
						if err == nil && message.Type == "tunnel-eof" {
							_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
							return
						}
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			cmd := cliCommand(t, map[string]string{
				"GOWEBSSH_STDIO":       "true",
				"GOWEBSSH_TARGET_HOST": "992319.xyz",
				"GOWEBSSH_TARGET_PORT": "8822",
				"GOWEBSSH_URL":         server.URL,
				"GOWEBSSH_USERNAME":    "admin",
				"GOWEBSSH_PASSWORD":    "test-password",
			}, append([]string{"--no-proxy"}, tt.args...)...)
			var stdout, stderr bytes.Buffer
			cmd.Stdin = bytes.NewReader(input)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("CLI failed: %v, stderr=%q", err, stderr.String())
			}
			if !bytes.Equal(stdout.Bytes(), input) || stderr.Len() != 0 {
				t.Fatalf("stdout=%v stderr=%q, want raw input=%v and empty stderr", stdout.Bytes(), stderr.String(), input)
			}
			wantHost, wantPort := "992319.xyz", 8822
			if len(tt.args) > 0 {
				wantHost, wantPort = "other.example.com", 2222
			}
			select {
			case got := <-target:
				if got.Host != wantHost || got.Port != wantPort {
					t.Fatalf("destination=%+v, want %s:%d", got, wantHost, wantPort)
				}
			default:
				t.Fatal("CLI did not send a tunnel destination")
			}
		})
	}
}

func TestCLIInvalidEnvironmentKeepsStdoutEmpty(t *testing.T) {
	cmd := cliCommand(t, map[string]string{"GOWEBSSH_STDIO": "invalid"})
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("exit=%v, want code 2", err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "GOWEBSSH_STDIO must be a boolean") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCLIVersionIgnoresTunnelEnvironment(t *testing.T) {
	output, err := cliCommand(t, map[string]string{"GOWEBSSH_STDIO": "invalid"}, "--version").CombinedOutput()
	if err != nil || string(output) != fmt.Sprintln(version) {
		t.Fatalf("output=%q error=%v, want version %q", output, err, version)
	}
}

func TestCLIPrivateConnectEnvironmentRequiresVerifiedTLS(t *testing.T) {
	cmd := cliCommand(t, map[string]string{
		"GOWEBSSH_STDIO":           "true",
		"GOWEBSSH_TARGET_HOST":     "target.example.test",
		"GOWEBSSH_URL":             "https://webssh.example.test",
		"GOWEBSSH_USERNAME":        "admin",
		"GOWEBSSH_PASSWORD":        "test-password",
		"GOWEBSSH_PRIVATE_CONNECT": "true",
	}, "--insecure")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("exit=%v", err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "requires certificate verification") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
