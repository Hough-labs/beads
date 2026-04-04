package main

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"time"
)

// detectServerMode probes the environment to determine whether bd should
// default to server mode. Called during bd init when --server and
// BEADS_DOLT_SERVER_MODE/BEADS_DOLT_SHARED_SERVER have not been set.
//
// Detection order:
//  1. BEADS_DOLT_HOST env var — explicit host means server mode, always.
//  2. Linux + reachable k8s Dolt endpoint — auto-discovery for cluster pods.
//
// Returns (enabled, host, port, user). If enabled is false, other values
// are zero/empty and no mode change should be applied.
func detectServerMode() (enabled bool, host string, port int, user string) {
	// 1. Explicit env var: BEADS_DOLT_HOST
	if h := os.Getenv("BEADS_DOLT_HOST"); h != "" {
		p := defaultDoltAutodetectPort
		if pStr := os.Getenv("BEADS_DOLT_PORT"); pStr != "" {
			if n, err := strconv.Atoi(pStr); err == nil && n > 0 {
				p = n
			}
		}
		u := "root"
		if uStr := os.Getenv("BEADS_DOLT_USER"); uStr != "" {
			u = uStr
		}
		return true, h, p, u
	}

	// 2. On Linux, probe the well-known k8s Dolt endpoint.
	// The TCP probe uses a short timeout so non-k8s Linux boxes don't stall.
	if runtime.GOOS == "linux" {
		if probeTCPPort(k8sDoltHost, k8sDoltPort, autodetectProbeTimeout) {
			return true, k8sDoltHost, k8sDoltPort, "root"
		}
	}

	return false, "", 0, ""
}

// probeTCPPort returns true if a TCP connection to host:port succeeds within timeout.
func probeTCPPort(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

const (
	k8sDoltHost               = "dolt.databases.svc.cluster.local"
	k8sDoltPort               = 3306
	defaultDoltAutodetectPort = 3306
	autodetectProbeTimeout    = 300 * time.Millisecond
)
