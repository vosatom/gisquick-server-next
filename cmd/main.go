package main

import (
	"fmt"
	"os"

	commands "github.com/gisquick/gisquick-server/cmd/commands"
)

func printCommandsList() {
	fmt.Println("Commands:")
	fmt.Println("  serve")
	fmt.Println("  adduser")
	fmt.Println("  addsuperuser")
	fmt.Println("  dumpusers")
	fmt.Println("  loadusers")
	fmt.Println("  deleteuser")
	fmt.Println("  migrate")
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
	case "deleteuser":
		runCommand(commands.DeleteUser)
	case "addsuperuser":
		runCommand(commands.AddSuperuser)
	case "dumpusers":
		runCommand(commands.DumpUsers)
	case "loadusers":
		runCommand(commands.LoadUsers)
	case "serve":
		runCommand(commands.Serve)
	case "migrate":
		runCommand(commands.Migrate)
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
