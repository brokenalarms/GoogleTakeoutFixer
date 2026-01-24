package main

import (
	"fmt"
	"os"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Flags missing! Enter InputPath and OutputPath.")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	/*
		useLinks := false
		if len(os.Args) >= 4 {
			useLinks = strings.Contains(strings.ToLower(os.Args[3]), "link")
		}*/

	//fixer.ProcessTakeout(inputPath, outputPath /*useLinks*/)
	fixer.Process(inputPath, outputPath)
}
