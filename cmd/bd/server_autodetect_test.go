package main

import (
	"fmt"
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestDetectServerMode_EnvVar(t *testing.T) {
	t.Setenv("BEADS_DOLT_HOST", "myhost.example.com")
	t.Setenv("BEADS_DOLT_PORT", "3307")
	t.Setenv("BEADS_DOLT_USER", "admin")

	enabled, host, port, user := detectServerMode()
	if !enabled {
		t.Fatal("expected enabled=true when BEADS_DOLT_HOST is set")
	}
	if host != "myhost.example.com" {
		t.Errorf("host = %q, want %q", host, "myhost.example.com")
	}
	if port != 3307 {
		t.Errorf("port = %d, want 3307", port)
	}
	if user != "admin" {
		t.Errorf("user = %q, want %q", user, "admin")
	}
}

func TestDetectServerMode_EnvVarDefaultPort(t *testing.T) {
	t.Setenv("BEADS_DOLT_HOST", "db.internal")
	// No BEADS_DOLT_PORT set — should use default

	enabled, host, port, user := detectServerMode()
	if !enabled {
		t.Fatal("expected enabled=true when BEADS_DOLT_HOST is set")
	}
	if host != "db.internal" {
		t.Errorf("host = %q, want %q", host, "db.internal")
	}
	if port != defaultDoltAutodetectPort {
		t.Errorf("port = %d, want %d", port, defaultDoltAutodetectPort)
	}
	if user != "root" {
		t.Errorf("user = %q, want %q", user, "root")
	}
}

func TestDetectServerMode_NoEnvNoServer(t *testing.T) {
	// Ensure env vars are cleared
	t.Setenv("BEADS_DOLT_HOST", "")
	t.Setenv("BEADS_DOLT_PORT", "")
	t.Setenv("BEADS_DOLT_USER", "")

	// This test only verifies the false path. On Linux in a non-k8s env
	// the probe to dolt.databases.svc.cluster.local will fail (correct).
	// On non-Linux it always returns false.
	if runtime.GOOS != "linux" {
		enabled, _, _, _ := detectServerMode()
		if enabled {
			t.Error("expected enabled=false on non-Linux with no env vars")
		}
	}
	// On Linux we can't assert false (CI might be in k8s), so we skip.
}

func TestProbeTCPPort_Reachable(t *testing.T) {
	// Start a local listener to prove the probe works.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	if !probeTCPPort("127.0.0.1", port, time.Second) {
		t.Error("expected probe to succeed for listening port")
	}
}

func TestProbeTCPPort_Unreachable(t *testing.T) {
	// Port 1 is almost never listening.
	if probeTCPPort("127.0.0.1", 1, 200*time.Millisecond) {
		t.Error("expected probe to fail for port 1")
	}
}

func TestProbeTCPPort_Timeout(t *testing.T) {
	// Use a non-routable IP to force a timeout (RFC 5737 documentation range).
	start := time.Now()
	timeout := 100 * time.Millisecond
	probeTCPPort("192.0.2.1", 3306, timeout)
	elapsed := time.Since(start)

	// Should complete in roughly timeout + small overhead, not hang indefinitely.
	maxAllowed := timeout + 500*time.Millisecond
	if elapsed > maxAllowed {
		t.Errorf("probe took %v, expected <= %v", elapsed, maxAllowed)
	}
}

func TestDetectServerMode_InvalidPort(t *testing.T) {
	t.Setenv("BEADS_DOLT_HOST", "db.example.com")
	t.Setenv("BEADS_DOLT_PORT", "not-a-number")

	enabled, _, port, _ := detectServerMode()
	if !enabled {
		t.Fatal("expected enabled=true when BEADS_DOLT_HOST is set")
	}
	if port != defaultDoltAutodetectPort {
		t.Errorf("invalid port string should fall back to default %d, got %d",
			defaultDoltAutodetectPort, port)
	}
}

// Ensure the k8s constants have the expected values (guards against typos).
func TestAutodetectConstants(t *testing.T) {
	if k8sDoltHost != "dolt.databases.svc.cluster.local" {
		t.Errorf("unexpected k8sDoltHost: %q", k8sDoltHost)
	}
	if k8sDoltPort != 3306 {
		t.Errorf("unexpected k8sDoltPort: %d", k8sDoltPort)
	}
	if autodetectProbeTimeout != 300*time.Millisecond {
		t.Errorf("unexpected autodetectProbeTimeout: %v", autodetectProbeTimeout)
	}
	_ = fmt.Sprintf // import used by other tests
}
