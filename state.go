package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type runState struct {
	CompletedTools map[string]string `json:"completed_tools"`
}

var (
	stateData      = &runState{CompletedTools: map[string]string{}}
	forceReinstall bool
)

func stateFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".chs-onboard")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func loadRunState() error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	if !pathExists(path) {
		stateData = &runState{CompletedTools: map[string]string{}}
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s runState
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("invalid state file: %w", err)
	}
	if s.CompletedTools == nil {
		s.CompletedTools = map[string]string{}
	}
	stateData = &s
	return nil
}

func saveRunState() error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(stateData, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0640)
}

func resetRunState() error {
	stateData = &runState{CompletedTools: map[string]string{}}
	return saveRunState()
}

func isToolCompleted(t toolID) bool {
	if stateData == nil || stateData.CompletedTools == nil {
		return false
	}
	_, ok := stateData.CompletedTools[string(t)]
	return ok
}

func markToolCompleted(t toolID) error {
	if stateData == nil {
		stateData = &runState{CompletedTools: map[string]string{}}
	}
	if stateData.CompletedTools == nil {
		stateData.CompletedTools = map[string]string{}
	}
	stateData.CompletedTools[string(t)] = time.Now().UTC().Format(time.RFC3339)
	return saveRunState()
}
