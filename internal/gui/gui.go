/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package gui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
	version "github.com/feloex/GoogleTakeoutFixer/internal/version"
	"github.com/ncruces/zenity"
)

func Main() {
	var inputPath, outputPath string

	// Create app / window
	a := app.New()
	a.SetIcon(resourceGoogleTakeoutFixerPng)
	w := a.NewWindow("GoogleTakeoutFixer " + version.Tag)
	w.Resize(fyne.NewSize(800, 400))

	var useSymlinks bool = false
	var writeMetadata bool = true
	var flatten bool = false
	var ignoreAlbums bool = false
	var monthSubfolders bool = false
	var restoreMOVExtension bool = false
	var useFilenameTimestamp bool = true
	var preferFilenameOverSidecar bool = false
	var dateFolders bool = false
	var appendDate bool = false

	progressLabel := widget.NewLabel("Ready to start")
	progressLabel.Truncation = fyne.TextTruncateEllipsis
	progressBar := widget.NewProgressBar()
	var cancelFn context.CancelFunc
	var cancelButton *widget.Button

	// Button for opening file dialog for choosing google takeout path and output path
	var inputButton *widget.Button
	inputButton = widget.NewButtonWithIcon("Select Google Takeout Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Google Takeout Folder"), zenity.Directory())
		if err == nil {
			inputPath = dir
			inputButton.SetText("Input: " + filepath.Base(inputPath))
			fmt.Println("Input folder:", inputPath)
		}
	})

	var outputButton *widget.Button
	outputButton = widget.NewButtonWithIcon("Select Output Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Output Folder"), zenity.Directory())
		if err == nil {
			outputPath = dir
			outputButton.SetText("Output: " + filepath.Base(outputPath))
			fmt.Println("Output folder:", outputPath)
		}
	})

	// Checkboxes for options. Default value is false
	useLinksCheckbox := widget.NewCheck("Use symlinks for albums", func(value bool) {
		useSymlinks = value
		fmt.Println("use symlinks", useSymlinks)
	})

	writeMetadataCheckbox := widget.NewCheck("Write metadata", func(value bool) {
		writeMetadata = value
		fmt.Println("write metadata", writeMetadata)
	})
	writeMetadataCheckbox.SetChecked(true)

	ignoreAlbumsCheckbox := widget.NewCheck("Ignore album folders", func(value bool) {
		ignoreAlbums = value
		fmt.Println("ignore albums", ignoreAlbums)
	})

	dateFoldersCheckbox := widget.NewCheck("Day subfolders (YYYY-MM-DD)", func(value bool) {
		dateFolders = value
	})

	monthSubfoldersCheckbox := widget.NewCheck("Create month subfolders", func(value bool) {
		monthSubfolders = value
	})

	flattenCheckbox := widget.NewCheck("Flatten album structure", func(value bool) {
		flatten = value
		fmt.Println("flatten", flatten)
	})

	restoreMOVExtensionCheckbox := widget.NewCheck("Restore .MOV file extension", func(value bool) {
		restoreMOVExtension = value
		fmt.Println("restore MOV extension", restoreMOVExtension)
	})

	appendDateCheckbox := widget.NewCheck("Append date to filename", func(value bool) {
		appendDate = value
	})

	preferFilenameOverSidecarCheckbox := widget.NewCheck("Prefer filename over sidecar when dates conflict", func(value bool) {
		preferFilenameOverSidecar = value
	})
	preferFilenameOverSidecarCheckbox.Disable()

	useFilenameTimestampCheckbox := widget.NewCheck("Use filename timestamp (YYYYMMDD / YYYY-MM-DD)", func(value bool) {
		useFilenameTimestamp = value
		if value {
			preferFilenameOverSidecarCheckbox.Enable()
		} else {
			preferFilenameOverSidecar = false
			preferFilenameOverSidecarCheckbox.SetChecked(false)
			preferFilenameOverSidecarCheckbox.Disable()
		}
	})

	// Fix conflicting options
	updateCheckboxStates := func() {
		setEnabled := func(cb *widget.Check, enabled bool) {
			if enabled {
				cb.Enable()
			} else {
				cb.Disable()
			}
		}

		// Logic:
		// 1. If Flatten is ON: monthSubfolders, useSymlinks, and ignoreAlbums are irrelevant/impossible.
		// 2. If IgnoreAlbums is ON: useSymlinks is impossible.
		// 3. useSymlinks and ignoreAlbums are mutually exclusive modes for album handling.

		setEnabled(useLinksCheckbox, !ignoreAlbums && !flatten)
		setEnabled(ignoreAlbumsCheckbox, !useSymlinks && !flatten)
		setEnabled(flattenCheckbox, !useSymlinks && !ignoreAlbums && !monthSubfolders)
		setEnabled(monthSubfoldersCheckbox, !flatten)
	}

	// Set initial states for global vars based on defaults or previous logic
	useLinksCheckbox.SetChecked(useSymlinks)
	writeMetadataCheckbox.SetChecked(writeMetadata)
	ignoreAlbumsCheckbox.SetChecked(ignoreAlbums)
	monthSubfoldersCheckbox.SetChecked(monthSubfolders)
	flattenCheckbox.SetChecked(flatten)
	restoreMOVExtensionCheckbox.SetChecked(restoreMOVExtension)
	dateFoldersCheckbox.SetChecked(dateFolders)
	appendDateCheckbox.SetChecked(appendDate)
	useFilenameTimestampCheckbox.SetChecked(useFilenameTimestamp)
	preferFilenameOverSidecarCheckbox.SetChecked(preferFilenameOverSidecar)

	// Refresh states once at start
	updateCheckboxStates()

	for _, cb := range []*widget.Check{useLinksCheckbox, ignoreAlbumsCheckbox, flattenCheckbox, monthSubfoldersCheckbox} {
		cb := cb
		prev := cb.OnChanged
		cb.OnChanged = func(v bool) {
			if prev != nil {
				prev(v)
			}
			updateCheckboxStates()
		}
	}

	// Button to start processing
	var startButton *widget.Button
	startButton = widget.NewButtonWithIcon("Start Processing", theme.MediaPlayIcon(), func() {
		// one of the folders has not been selected
		if inputPath == "" || outputPath == "" {
			fixer.Log(fixer.LoggerInfo, "Select both input and output folders")
			return
		}

		// Disable buttons while processing
		inputButton.Disable()
		outputButton.Disable()
		startButton.Disable()

		useLinksCheckbox.Disable()
		writeMetadataCheckbox.Disable()
		ignoreAlbumsCheckbox.Disable()
		monthSubfoldersCheckbox.Disable()
		flattenCheckbox.Disable()
		restoreMOVExtensionCheckbox.Disable()
		dateFoldersCheckbox.Disable()
		appendDateCheckbox.Disable()
		useFilenameTimestampCheckbox.Disable()
		preferFilenameOverSidecarCheckbox.Disable()

		fixer.Log(fixer.LoggerInfo, "Processing...")
		progressBar.SetValue(0)

		ctx, cancel := context.WithCancel(context.Background())
		cancelFn = cancel
		cancelButton.Enable()

		progressCh := make(chan fixer.Progress)

		opts := fixer.ProcessOptions{
			UseSymlinks:         useSymlinks,
			WriteMetadata:       writeMetadata,
			Flatten:             flatten,
			IgnoreAlbums:        ignoreAlbums,
			MonthSubfolders:     monthSubfolders,
			RestoreMOVExtension: restoreMOVExtension,
			UseFilenameTimestamp:       useFilenameTimestamp,
			PreferFilenameOverSidecar: preferFilenameOverSidecar,
			DateFolders:               dateFolders,
			AppendDateToFilename:     appendDate,
		}
		go func() {
			if err := fixer.Process(ctx, inputPath, outputPath, progressCh, opts); err != nil {
				if ctx.Err() == nil {
					fyne.Do(func() {
						fixer.Log(fixer.LoggerError, "%s", "Error: "+err.Error())
					})
				}
			}
		}()

		// Update progress

		go func() {
			var lastUpdate time.Time
			var lastP fixer.Progress

			for p := range progressCh {
				lastP = p
				if time.Since(lastUpdate) >= 100*time.Millisecond {
					lastUpdate = time.Now()

					percentage := 0.0
					if p.Total > 0 {
						percentage = (float64(p.Processed) / float64(p.Total)) * 100.0
					}

					text := fmt.Sprintf("[%.2f%%] %d/%d - %s", percentage, p.Processed, p.Total, filepath.Base(p.Current))
					processed, total := float64(p.Processed), float64(p.Total)

					fyne.Do(func() {
						progressBar.Max = total
						progressBar.SetValue(processed)
						progressLabel.SetText(text)
					})
				}
			}

			// Processing complete
			fyne.Do(func() {
				if lastP.Total > 0 {
					progressBar.Max = float64(lastP.Total)
					progressBar.SetValue(float64(lastP.Total))
				}

				if ctx.Err() != nil {
					fixer.Log(fixer.LoggerInfo, "Cancelled")
					progressLabel.SetText("Cancelled")
				} else {
					fixer.Log(fixer.LoggerInfo, "Detailed logs are saved in the ./logs folder")
					fixer.Log(fixer.LoggerInfo, "Done")

					fixer.Log(fixer.LoggerInfo, "Final progress: Total=%d Processed=%d Succeeded=%d Failed=%d", lastP.Total, lastP.Processed, lastP.Succeeded, lastP.Failed)
					summary := fmt.Sprintf("Finished processing %d files (%d succeeded, %d failed)", lastP.Total, lastP.Succeeded, lastP.Failed)
					progressLabel.SetText(summary)
					fixer.Log(fixer.LoggerInfo, "%s", summary)
				}
				cancelButton.Disable()
				cancelFn = nil
				inputButton.Enable()
				outputButton.Enable()
				startButton.Enable()

				// Manually re-enable restoreMOVExtensionCheckbox and writeMetadataCheckbox
				// since they are not affected by other checboxes in updateCheckboxStates
				restoreMOVExtensionCheckbox.Enable()
				dateFoldersCheckbox.Enable()
				appendDateCheckbox.Enable()
				useFilenameTimestampCheckbox.Enable()
				preferFilenameOverSidecarCheckbox.Enable()
				writeMetadataCheckbox.Enable()
				// Re-enable checboxes based on current states
				updateCheckboxStates()
			})
		}()
	})

	cancelButton = widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		if cancelFn == nil {
			return
		}
		fixer.Log(fixer.LoggerInfo, "Cancelling...")
		cancelButton.Disable()
		cancelFn()
	})
	cancelButton.Disable()

	logEntry := widget.NewMultiLineEntry()
	const maxVisibleLogLines = 200
	visibleLogLines := make([]string, 0, maxVisibleLogLines)

	// Prevent user from editing the log while keeping text selectable
	// This is not optimal but fyne does not provide a better way to do this
	var logUpdating bool
	logEntry.OnChanged = func(_ string) {
		if logUpdating {
			return
		}
		logUpdating = true
		logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
		logUpdating = false
	}

	logCh := make(chan string, 1000)
	fixer.LogHandler = func(level fixer.LogLevel, message string) {
		logCh <- fmt.Sprintf("[%s] %s", level, message)
	}

	// Throttle log updates to the UI using a channel
	go func() {
		for logMsg := range logCh {
			newLogs := []string{logMsg}

			// Group remaining logs
			for len(logCh) > 0 {
				newLogs = append(newLogs, <-logCh)
			}

			fyne.Do(func() {
				visibleLogLines = append(visibleLogLines, newLogs...)
				if len(visibleLogLines) > maxVisibleLogLines {
					visibleLogLines = visibleLogLines[len(visibleLogLines)-maxVisibleLogLines:]
				}

				logUpdating = true
				logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
				logUpdating = false

				logEntry.CursorRow = len(visibleLogLines)
				logEntry.CursorColumn = 0
				logEntry.Refresh()
			})

			// Wait to collect more logs
			time.Sleep(100 * time.Millisecond)
		}
	}()

	fixer.Log(fixer.LoggerInfo, "Logs will appear here...")

	folderButtons := container.NewGridWithColumns(
		2,
		inputButton,
		outputButton,
	)

	CheckBoxRow := container.NewGridWithColumns(
		2,
		useLinksCheckbox,
		writeMetadataCheckbox,
		ignoreAlbumsCheckbox,
		monthSubfoldersCheckbox,
		flattenCheckbox,
		dateFoldersCheckbox,
		restoreMOVExtensionCheckbox,
		appendDateCheckbox,
	)

	filenameTimestampGroup := container.NewVBox(
		useFilenameTimestampCheckbox,
		container.NewPadded(preferFilenameOverSidecarCheckbox),
	)

	StartCancelRow := container.NewGridWithColumns(2, startButton, cancelButton)

	OutputSeparator := container.NewPadded(widget.NewSeparator())
	OptionsSeparator := container.NewPadded(widget.NewSeparator())

	topContent := container.NewVBox(
		folderButtons,
		OutputSeparator,
		CheckBoxRow,
		filenameTimestampGroup,
		OptionsSeparator,
		StartCancelRow,
		progressBar,
		progressLabel,
	)

	w.SetContent(container.NewBorder(
		topContent,
		nil,
		nil,
		nil,
		//logEntry, // expand
		logEntry, // expand
	))

	w.ShowAndRun()
}
