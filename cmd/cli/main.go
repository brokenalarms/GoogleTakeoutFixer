package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Flags missing! Enter InputPath and OutputPath.")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	progressCh := make(chan fixer.Progress)

	go func() {
		if err := fixer.Process(inputPath, outputPath, progressCh); err != nil {
			fmt.Println("error:", err)
		}
	}()

	// Recieve progress event
	for p := range progressCh {
		if p.Processed == 0 {
			continue
		}

		fmt.Println(
			p.Processed,
			"/",
			p.Total,
			filepath.Base(p.Current),
		)
	}

	fmt.Println("\nDone")
}
