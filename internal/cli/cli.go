package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	_ "github.com/clstr-io/clstr/challenges"
	"github.com/clstr-io/clstr/internal/registry"
	"github.com/clstr-io/clstr/internal/state"
	commands "github.com/urfave/cli/v3"
)

const (
	DocsBaseURL = "https://docs.clstr.io"
)

var (
	yellow = color.New(color.FgYellow).SprintFunc()
)

// createChallengeFiles creates the initial project files for a new challenge.
func createChallengeFiles(challenge *registry.Challenge, targetPath string) error {
	// run.sh
	scriptPath := filepath.Join(targetPath, "run.sh")
	scriptTemplate := `#!/bin/bash -e

# This script builds and runs your implementation.
# clstr will execute this script to start your program.
# "$@" passes command-line arguments from clstr to your program, e.g.:
#   --working-dir=<path>: Directory where your program should write files

echo "Replace this line with the command that runs your implementation."
# Examples:
#   exec go run ./cmd/server "$@"
#   exec python main.py "$@"
#   exec ./my-program "$@"
`

	err := os.WriteFile(scriptPath, []byte(scriptTemplate), 0755)
	if err != nil {
		return fmt.Errorf("Failed to create run.sh: %w", err)
	}

	// README.md
	readmePath := filepath.Join(targetPath, "README.md")
	err = os.WriteFile(readmePath, []byte(challenge.README()), 0644)
	if err != nil {
		return fmt.Errorf("Failed to create README.md: %w", err)
	}

	// clstr.state
	cfg := &state.State{
		Challenge: challenge.Key,
		Stage:     challenge.StageOrder[0],
	}
	statePath := filepath.Join(targetPath, "clstr.state")
	err = state.SaveTo(cfg, statePath)
	if err != nil {
		return fmt.Errorf("Failed to create clstr.state: %w", err)
	}

	// .gitignore
	gitignorePath := filepath.Join(targetPath, ".gitignore")
	gitignoreContent := `.clstr/`
	err = os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644)
	if err != nil {
		return fmt.Errorf("Failed to create .gitignore: %w", err)
	}

	return nil
}

// InitChallenge initializes a challenge in the specified directory.
func InitChallenge(ctx context.Context, cmd *commands.Command) error {
	// Get Challenge
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return fmt.Errorf("Challenge name is required.\nUsage: clstr init <challenge> [path]")
	}

	challengeKey := args[0]
	challenge, err := registry.GetChallenge(challengeKey)
	if err != nil {
		return err
	}

	// Create Directory
	var targetPath string
	if len(args) > 1 {
		targetPath = args[1]
		err := os.MkdirAll(targetPath, 0755)
		if err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", targetPath, err)
		}
	} else {
		targetPath = "."
	}

	err = createChallengeFiles(challenge, targetPath)
	if err != nil {
		return err
	}

	if targetPath == "." {
		fmt.Println("Created challenge in current directory.")
	} else {
		fmt.Printf("Created challenge in directory: ./%s\n", targetPath)
	}

	fmt.Println("  run.sh         - Builds and runs your implementation")
	fmt.Println("  README.md      - Challenge overview and requirements")
	fmt.Println("  clstr.state    - Tracks your progress")
	fmt.Printf("  .gitignore     - Ignores .clstr/ working directory (server files and logs)\n\n")

	firstStageKey := challenge.StageOrder[0]
	if targetPath == "." {
		fmt.Printf("Implement %s stage, then run %s.\n", firstStageKey, yellow("'clstr test'"))
	} else {
		fmt.Printf("cd %s and implement %s stage, then run %s.\n", targetPath, firstStageKey, yellow("'clstr test'"))
	}

	return nil
}

// validateEnvironment checks if run.sh exists and loads the state.
func validateEnvironment() (*state.State, error) {
	if _, err := os.Stat("run.sh"); os.IsNotExist(err) {
		return nil, fmt.Errorf("run.sh not found\nCreate an executable run.sh script that starts your implementation.")
	}

	cfg, err := state.Load()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// runStageTests runs tests for a specific stage and returns success/failure.
func runStageTests(ctx context.Context, challengeKey, stageKey string) (bool, error) {
	challenge, err := registry.GetChallenge(challengeKey)
	if err != nil {
		return false, err
	}

	stage, err := challenge.GetStage(stageKey)
	if err != nil {
		msg := "\nAvailable stages:\n"
		for _, stage := range challenge.StageOrder {
			msg += fmt.Sprintf("- %s\n", stage)
		}

		return false, fmt.Errorf("%w\n%s", err, msg)
	}

	suite := stage.Fn()
	fmt.Printf("Testing %s: %s\n\n", stageKey, stage.Name)
	passed := suite.Run(ctx)
	return passed, nil
}

// Test runs tests for the specified stage(s).
func Test(ctx context.Context, cmd *commands.Command) error {
	cfg, err := validateEnvironment()
	if err != nil {
		return err
	}

	var challengeKey string
	var stageKey string

	switch cmd.NArg() {
	case 0:
		challengeKey = cfg.Challenge
		stageKey = cfg.Stage
	case 1:
		challengeKey = cfg.Challenge
		stageKey = cmd.Args().Slice()[0]
	default:
		return fmt.Errorf("Too many arguments.\nUsage: clstr test [stage]")
	}

	challenge, err := registry.GetChallenge(challengeKey)
	if err != nil {
		return err
	}

	// Determine which stages to test
	var stagesToTest []string
	if cmd.Bool("so-far") {
		targetIndex := challenge.StageIndex(stageKey)
		if targetIndex == -1 {
			return fmt.Errorf("Stage '%s' not found in challenge", stageKey)
		}

		stagesToTest = challenge.StageOrder[:targetIndex+1]
	} else {
		stagesToTest = []string{stageKey}
	}

	// Run tests for all stages
	for _, currentStage := range stagesToTest {
		passed, err := runStageTests(ctx, challengeKey, currentStage)
		if err != nil {
			return err
		}

		if !passed {
			guideURL := fmt.Sprintf("%s/%s/%s", DocsBaseURL, challengeKey, currentStage)
			return fmt.Errorf("\nRead the guide: \033]8;;%s\033\\%s/%s/%s\033]8;;\033\\\n", guideURL, DocsBaseURL, challengeKey, currentStage)
		}

		if len(stagesToTest) > 1 {
			fmt.Println()
		}
	}

	// Success message
	if len(stagesToTest) > 1 {
		fmt.Printf("All stages up to %s passed! ✓\n", stageKey)
	}

	targetIndex := challenge.StageIndex(stageKey)
	if targetIndex < challenge.Len()-1 {
		fmt.Printf("\nRun %s to advance to the next stage.\n", yellow("'clstr next'"))
	}

	return nil
}

// NextStage advances to the next stage after verifying current stage is complete.
func NextStage(ctx context.Context, cmd *commands.Command) error {
	// Get Challenge
	cfg, err := validateEnvironment()
	if err != nil {
		return err
	}

	challenge, err := registry.GetChallenge(cfg.Challenge)
	if err != nil {
		return err
	}

	// Check if current stage is completed
	currentIndex := challenge.StageIndex(cfg.Stage)
	if currentIndex == -1 {
		return fmt.Errorf("Current stage '%s' not found in challenge", cfg.Stage)
	}

	// Run tests for current stage
	passed, err := runStageTests(ctx, cfg.Challenge, cfg.Stage)
	if err != nil {
		return err
	}

	fmt.Println()

	if !passed {
		return fmt.Errorf("Complete %s before advancing.", cfg.Stage)
	}

	// Check if already at final stage
	if currentIndex == challenge.Len()-1 {
		fmt.Printf("You've completed all stages for %s! 🎉\n\n", cfg.Challenge)
		fmt.Printf("Try another challenge at \033]8;;%s/\033\\%s\033]8;;\033\\\n", DocsBaseURL, DocsBaseURL)

		return state.Save(cfg)
	}

	// Advance to next stage
	nextStageKey := challenge.StageOrder[currentIndex+1]
	cfg.Stage = nextStageKey
	err = state.Save(cfg)
	if err != nil {
		return err
	}

	nextStage, err := challenge.GetStage(nextStageKey)
	if err != nil {
		return err
	}

	fmt.Printf("Advanced to %s: %s\n\n", nextStageKey, nextStage.Name)
	guideURL := fmt.Sprintf("%s/%s/%s", DocsBaseURL, cfg.Challenge, nextStageKey)
	fmt.Printf("Read the guide: \033]8;;%s\033\\%s/%s/%s\033]8;;\033\\\n\n", guideURL, DocsBaseURL, cfg.Challenge, nextStageKey)
	fmt.Printf("Run %s when ready.\n", yellow("'clstr test'"))

	return nil
}

// ShowStatus displays the current challenge progress and next steps.
func ShowStatus(ctx context.Context, cmd *commands.Command) error {
	// Summary
	cfg, err := state.Load()
	if err != nil {
		return err
	}

	challenge, err := registry.GetChallenge(cfg.Challenge)
	if err != nil {
		return err
	}

	fmt.Printf("%s\n\n%s\n\n", challenge.Name, challenge.Summary)

	// Progress
	fmt.Println("Progress:")
	currentIndex := challenge.StageIndex(cfg.Stage)
	for i, stageKey := range challenge.StageOrder {
		stage, err := challenge.GetStage(stageKey)
		if err != nil {
			continue
		}

		isCompleted := i < currentIndex
		if isCompleted {
			fmt.Printf("✓ %-18s - %s\n", stageKey, stage.Name)
		} else if stageKey == cfg.Stage {
			fmt.Printf("→ %-18s - %s\n", stageKey, stage.Name)
		} else {
			fmt.Printf("  %-18s - %s\n", stageKey, stage.Name)
		}
	}

	// Next steps
	guideURL := fmt.Sprintf("%s/%s/%s", DocsBaseURL, cfg.Challenge, cfg.Stage)
	fmt.Printf("\nRead the guide: \033]8;;%s\033\\%s/%s/%s\033]8;;\033\\\n\n", guideURL, DocsBaseURL, cfg.Challenge, cfg.Stage)
	fmt.Printf("Implement %s, then run %s.\n", cfg.Stage, yellow("'clstr test'"))

	return nil
}

// ListChallenges displays all available challenges.
func ListChallenges(ctx context.Context, cmd *commands.Command) error {
	fmt.Printf("Available challenges:\n\n")

	challenges := registry.GetAllChallenges()
	for key, challenge := range challenges {
		fmt.Printf("  %-20s - %s (%d stages)\n", key, challenge.Name, challenge.Len())
	}

	fmt.Printf("\nStart with: clstr init <challenge-name>\n")

	return nil
}
