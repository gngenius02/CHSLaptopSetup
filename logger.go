package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type logLevel string

const (
	logINFO  logLevel = "INFO"
	logWARN  logLevel = "WARN"
	logERROR logLevel = "ERROR"
)

type logEntry struct {
	Timestamp string            `json:"ts"`
	Level     logLevel          `json:"level"`
	Phase     string            `json:"phase,omitempty"`
	Step      string            `json:"step,omitempty"`
	Message   string            `json:"msg"`
	Fields    map[string]string `json:"fields,omitempty"`
}

var log struct {
	mu      sync.Mutex
	f       *os.File
	phase   string
	encoder *json.Encoder
}

func logInit() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".chs-onboard")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "run.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	log.f = f
	log.encoder = json.NewEncoder(f)
	logInfo("session_start", "chs-onboard started", nil)
	return nil
}

func logClose() {
	if log.f != nil {
		log.f.Close()
	}
}

func logSetPhase(phase string) {
	log.mu.Lock()
	log.phase = phase
	log.mu.Unlock()
}

func logWrite(level logLevel, step, msg string, fields map[string]string) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.encoder == nil {
		return
	}
	_ = log.encoder.Encode(logEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Phase:     log.phase,
		Step:      step,
		Message:   msg,
		Fields:    fields,
	})
}

func logInfo(step, msg string, fields map[string]string) {
	logWrite(logINFO, step, msg, fields)
	fmt.Printf("  [→] %s\n", msg)
}

func logWarn(step, msg string, fields map[string]string) {
	logWrite(logWARN, step, msg, fields)
	fmt.Printf("  [!] %s\n", msg)
}

func logError(step, msg string, fields map[string]string) {
	logWrite(logERROR, step, msg, fields)
	fmt.Fprintf(os.Stderr, "  [✗] %s\n", msg)
}

func logFatal(step, msg string, fields map[string]string) {
	logWrite(logERROR, step, msg, fields)
	fmt.Fprintf(os.Stderr, "  [✗] FATAL: %s\n", msg)
	logClose()
	os.Exit(1)
}
