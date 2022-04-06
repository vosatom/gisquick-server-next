package main

import (
	"fmt"
	"os"

	commands "github.com/gisquick/gisquick-server/cmd/commands"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func printCommandsList() {
	fmt.Println("Commands:")
	fmt.Println("  serve")
	fmt.Println("  adduser")
	fmt.Println("  addsuperuser")
	fmt.Println("  dumpusers")
	fmt.Println("  loadusers")
}

func main() {
	if len(os.Args) < 2 {
		printCommandsList()
		return
	}
	cmd := os.Args[1]
	os.Args = os.Args[1:]

	switch cmd {
	case "adduser":
		runCommand(commands.AddUser)
	case "addsuperuser":
		runCommand(commands.AddSuperuser)
	case "dumpusers":
		runCommand(commands.DumpUsers)
	case "loadusers":
		runCommand(commands.LoadUsers)
	case "serve":
		withLogger(commands.Serve)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printCommandsList()
	}
}

func runCommand(command func() error) {
	if err := command(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func withLogger(command func(log *zap.SugaredLogger) error) {
	config := zap.NewProductionConfig()
	// config := zap.NewDevelopmentConfig()

	// config.OutputPaths = []string{"stdout"}
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.DisableStacktrace = true

	logger, err := config.Build()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer logger.Sync()
	log := logger.Sugar()

	if err := command(log); err != nil {
		log.Errorw("startup", "ERROR", err)
		log.Sync()
		os.Exit(1)
	}
}
