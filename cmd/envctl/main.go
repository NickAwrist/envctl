package main

import (
	"os"

	"github.com/NickAwrist/envctl/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
