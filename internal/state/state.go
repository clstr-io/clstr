package state

import (
	"fmt"
	"os"
	"strings"
)

const statePath = "clstr.state"

// State represents the challenge progress.
type State struct {
	Challenge string
	Stage     string
}

// Load reads and parses the clstr.state file.
func Load() (*State, error) {
	_, err := os.Stat(statePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("Not in a challenge directory\nRun this command from a directory created with 'clstr init <challenge>'")
	}

	bytes, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read state file: %w", err)
	}

	content := strings.TrimSpace(string(bytes))
	parts := strings.SplitN(content, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("Invalid state format. Expected '<challenge>:<stage>', got: %s", content)
	}

	return &State{
		Challenge: strings.TrimSpace(parts[0]),
		Stage:     strings.TrimSpace(parts[1]),
	}, nil
}

// Save writes the state to the default clstr.state file.
func Save(st *State) error {
	return SaveTo(st, statePath)
}

// SaveTo writes the state to the specified path.
func SaveTo(st *State, path string) error {
	content := fmt.Sprintf("%s:%s\n", st.Challenge, st.Stage)
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("Failed to write state file: %w", err)
	}

	return nil
}
