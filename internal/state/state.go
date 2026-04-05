package state

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

const statePath = "clstr.yaml"

// State represents the challenge progress.
type State struct {
	Challenge string `yaml:"challenge"`
	Stage     string `yaml:"stage"`
}

// Load reads and parses the clstr.yaml file.
func Load() (*State, error) {
	_, err := os.Stat(statePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("Not in a challenge directory.\nRun this command from a directory created with 'clstr init <challenge>'.")
	}

	bytes, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read state file: %w", err)
	}

	var st State
	err = yaml.Unmarshal(bytes, &st)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse state file: %w", err)
	}

	return &st, nil
}

// Save writes the state to the default clstr.yaml file.
func Save(st *State) error {
	return SaveTo(st, statePath)
}

// SaveTo writes the state to the specified path.
func SaveTo(st *State, path string) error {
	bytes, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("Failed to serialize state: %w", err)
	}

	err = os.WriteFile(path, bytes, 0644)
	if err != nil {
		return fmt.Errorf("Failed to write state file: %w", err)
	}

	return nil
}
