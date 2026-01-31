package cli

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func Main() {
	inputPath := flag.String("input", "", "Path to Google takeout directory")

	outputPath := flag.String("output", "", "Path to output directory")

	useSymlinks := flag.Bool("symlink", false, "Use symlinks inside of albums instead of duplicating images")

	flag.Parse()

	if *inputPath == "" || *outputPath == "" {
		fmt.Println("Error: --input and --output are required")
		flag.Usage()
		os.Exit(1)
	}

	progressCh := make(chan fixer.Progress)

	go func() {
		if err := fixer.Process(*inputPath, *outputPath, progressCh, *useSymlinks); err != nil {
			fmt.Printf("Error during processing: %v\n", err)
		}
	}()

	for p := range progressCh {
		if p.Processed == 0 {
			continue
		}

		percentageFinished := math.Round(float64(p.Processed) / float64(p.Total) * 100)

		fmt.Printf("[%3.0f%%] %2d/%2d - %s\n",
			percentageFinished,
			p.Processed,
			p.Total,
			filepath.Base(p.Current),
		)
	}

	fmt.Println("\nDone")
}
