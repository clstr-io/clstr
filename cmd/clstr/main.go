package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/clstr-io/clstr/internal/cli"
	commands "github.com/urfave/cli/v3"
)

func main() {
	log.SetFlags(0)

	cmd := &commands.Command{
		Name:  "clstr",
		Usage: "Learn distributed systems by building them from scratch",
		Commands: []*commands.Command{
			{
				Name:      "init",
				Aliases:   []string{"i"},
				Usage:     "Initialize a challenge",
				ArgsUsage: "<challenge> [path]",
				Action:    cli.InitChallenge,
			},
			{
				Name:      "test",
				Aliases:   []string{"t"},
				Usage:     "Test your implementation",
				ArgsUsage: "[stage]",
				Flags: []commands.Flag{
					&commands.BoolFlag{
						Name:  "so-far",
						Usage: "Test all stages up to the specified stage",
					},
				},
				Action: cli.Test,
			},
			{
				Name:    "next",
				Aliases: []string{"n"},
				Usage:   "Advance to the next stage",
				Action:  cli.NextStage,
			},
			{
				Name:    "status",
				Aliases: []string{"s"},
				Usage:   "Show current progress",
				Action:  cli.ShowStatus,
			},
			{
				Name:    "list",
				Aliases: []string{"l", "ls"},
				Usage:   "List available challenges",
				Action:  cli.ListChallenges,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	err := cmd.Run(ctx, os.Args)
	if err != nil {
		if ctx.Err() == context.Canceled {
			os.Exit(0)
		}

		log.Fatal(err)
	}
}
