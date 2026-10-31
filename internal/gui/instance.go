package gui

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Trlydev/WBMM/internal/manager"
)

// One window at a time: the running window listens on 127.0.0.1 and writes
// its port and a random key to instance.json. Starting wbmm again sends the
// key, the first window comes to the front, and the new process exits.

type instanceInfo struct {
	Addr string `json:"addr"`
	Key  string `json:"key"`
}

func instanceFile() string {
	d, err := manager.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "instance.json")
}

// forwardToRunning asks a running window to show itself. It reports
// whether one answered.
func forwardToRunning() bool {
	b, err := os.ReadFile(instanceFile())
	if err != nil {
		return false
	}
	var in instanceInfo
	if json.Unmarshal(b, &in) != nil || !strings.HasPrefix(in.Addr, "127.0.0.1:") {
		return false
	}
	c, err := net.DialTimeout("tcp", in.Addr, time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte(in.Key + "\n")); err != nil {
		return false
	}
	reply, _ := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(reply) == "ok"
}

var (
	listenMu sync.Mutex
	listener net.Listener
)

// listenForOthers starts answering later starts; show is called for each.
func listenForOthers(show func()) (stop func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return func() {}
	}
	kb := make([]byte, 16)
	rand.Read(kb)
	key := hex.EncodeToString(kb)
	b, _ := json.Marshal(instanceInfo{Addr: ln.Addr().String(), Key: key})
	os.WriteFile(instanceFile(), b, 0o600)
	listenMu.Lock()
	listener = ln
	listenMu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				got, _ := bufio.NewReader(c).ReadString('\n')
				if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(key)) == 1 {
					c.Write([]byte("ok\n"))
					show()
				}
			}()
		}
	}()
	return stopListening
}

// stopListening lets a new wbmm start its own window (restarts).
func stopListening() {
	listenMu.Lock()
	defer listenMu.Unlock()
	if listener != nil {
		listener.Close()
		listener = nil
		os.Remove(instanceFile())
	}
}
