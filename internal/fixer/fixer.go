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

package fixer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Progress struct {
	Total     int
	Processed int
	Succeeded int
	Failed    int
	Current   string
}

// TODO: Add more options
// TODO: Disable checkboxes when processing
type ProcessOptions struct {
	UseSymlinks               bool
	WriteMetadata             bool
	MonthSubfolders           bool
	IgnoreAlbums              bool
	Flatten                   bool
	RestoreMOVExtension       bool // See issue #2
	UseFilenameTimestamp       bool
	PreferFilenameOverSidecar bool
	DateFolders              bool
	AppendDateToFilename     bool
	DeduplicateOutput        bool
}

type WrittenFile struct {
	DestPath     string
	HasSidecar   bool
	DateOriginal string
	ImageWidth   string
	ImageHeight  string
	FileSize     int64
	BaseNameLen  int
}

type FixerContext struct {
	Ctx          context.Context
	SourceRoot   string
	AllRoots     []string
	OutputRoot   string
	Options      ProcessOptions
	ProgressCh   chan<- Progress
	WrittenFiles map[string]WrittenFile
}

// Process is the main fixer entry point.
// Process
// -> DiscoverDirs
// --> ProcessDirectory
// ---> ProcessFile
// TODO: Do something in case files already exists instead of overwriting them
func Process(
	ctx context.Context,
	sourcePath string,
	outputPath string,
	progressCh chan<- Progress,
	options ProcessOptions,
) error {
	err := InitializeFileLogger()
	if err != nil {
		if LogHandler != nil {
			LogHandler(LoggerWarn, fmt.Sprintf("Failed to initialize file logger: %v", err))
		}
	} else {
		defer func() {
			err := CloseFileLogger()
			if err != nil && LogHandler != nil {
				LogHandler(LoggerWarn, fmt.Sprintf("Failed to close file logger: %v", err))
			}
		}()
	}

	Log(LoggerInfo, "Starting processing with source: %s and output: %s", sourcePath, outputPath)

	// Log total processing time when processing is done
	startTime := time.Now()
	defer func() {
		Log(LoggerInfo, "Total processing time: %s", time.Since(startTime).Round(time.Second))
		ClearCache()
	}()

	defer close(progressCh)
	p := Progress{}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if options.WriteMetadata || options.RestoreMOVExtension {
		if err := InitializeExifTool(); err != nil {
			Log(LoggerError, "Failed to initialize exiftool: %v", err)
			return err
		}
		defer CloseExifTool()
	}

	amountImages, err := CountProcessableFiles(sourcePath, options)
	if err != nil {
		Log(LoggerError, "Error counting images: %v", err)
		return err
	}
	p.Total = amountImages
	progressCh <- p

	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		Log(LoggerError, "Error getting file info: %v", err)
		return err
	}

	fixerCtx := &FixerContext{
		Ctx:          ctx,
		SourceRoot:   sourcePath,
		OutputRoot:   outputPath,
		Options:      options,
		ProgressCh:   progressCh,
		WrittenFiles: make(map[string]WrittenFile),
	}

	if fileInfo.IsDir() {
		roots, err := FindSourceRoots(sourcePath)
		if err != nil {
			Log(LoggerError, "Error finding source roots: %v", err)
			return err
		}

		fixerCtx.AllRoots = roots

		for _, root := range roots {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fixerCtx.SourceRoot = root
			Log(LoggerInfo, "Processing source root: %s", root)

			dirs, err := DiscoverDirs(root)
			if err != nil {
				Log(LoggerError, "Error discovering directories in %s: %v", root, err)
				continue
			}

			for _, dir := range dirs {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				dirPath := filepath.Join(root, dir.Name())

				isYearFolder, err := IsYearFolder(dir.Name())
				if err != nil {
					Log(LoggerWarn, "Failed to determine if folder is a year folder for %s: %v", dir.Name(), err)
				}
				if options.IgnoreAlbums && !isYearFolder {
					Log(LoggerInfo, "Skipping album folder: %s", dir.Name())
					continue
				}

				outputDirName := dir.Name()
				if isYearFolder {
					outputDirName = ExtractYearFromFolder(dir.Name())
				}
				targetPath := filepath.Join(outputPath, outputDirName)

				p = ProcessDirectory(fixerCtx, dirPath, targetPath, isYearFolder, p)
			}
		}
	} else {
		err = ProcessFile(fixerCtx, sourcePath, "", false)
		p.Processed++
		p.Current = sourcePath
		if err != nil {
			p.Failed++
			Log(LoggerError, "Error processing file: %v", err)
		} else {
			p.Succeeded++
		}
		progressCh <- p
	}

	return nil
}

// Process a directory and fix all files within the directory. Ignores sub-directories.
func ProcessDirectory(
	fixerCtx *FixerContext,
	dirPath string,
	outputPath string,
	isYearFolder bool,
	p Progress,
) Progress {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		Log(LoggerError, "Error reading directory: %v", err)
		return p
	}

	// Buffered channel to avoid blocking
	jobs := make(chan string, len(files))
	completed := make(chan string)
	errors := make(chan error)

	sourceDirName := filepath.Base(dirPath)

	var wg sync.WaitGroup
	// Use 1 worker for now to avoid any complex race conditions with the shared exiftool
	// while we debug the freeze. We can scale this back up once stable.
	workerCount := 1 

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		go func() {
			for imagePath := range jobs {
				if fixerCtx.Ctx.Err() != nil {
					wg.Done()
					continue
				}
				err := ProcessFile(fixerCtx, imagePath, sourceDirName, isYearFolder)
				if err != nil {
					errors <- fmt.Errorf("error processing file %s: %w", imagePath, err)
				} else {
					completed <- imagePath
				}
				wg.Done()
			}
		}()
	}

	// Send jobs
	for _, file := range files {
		if fixerCtx.Ctx.Err() != nil {
			break
		}
		if file.IsDir() {
			continue
		}

		imagePath := filepath.Join(dirPath, file.Name())

		if !IsMediaFile(imagePath) {
			continue
		}

		wg.Add(1)
		jobs <- imagePath
	}

	close(jobs)

	go func() {
		wg.Wait()
		close(completed)
		close(errors)
	}()

	// Update progress and handle outcomes
	for {
		select {
		case ev, ok := <-completed:
			if !ok {
				completed = nil
			} else {
				p.Processed++
				p.Succeeded++
				p.Current = ev
				fixerCtx.ProgressCh <- p
			}
		case err, ok := <-errors:
			if !ok {
				errors = nil
			} else {
				p.Processed++
				p.Failed++
				Log(LoggerError, "%v", err)
				fixerCtx.ProgressCh <- p
			}
		case <-fixerCtx.Ctx.Done():
		}

		if completed == nil && errors == nil {
			break
		}
	}

	return p
}

// ProcessFile processes a single file by finding its sidecar file and then fixing it using the sidecar's metadata
func ProcessFile(
	fixerCtx *FixerContext,
	sourcePath string,
	sourceDirName string,
	isYearFolder bool,
) error {
	if fixerCtx.Ctx.Err() != nil {
		return fixerCtx.Ctx.Err()
	}

	fileName := filepath.Base(sourcePath)

	// See issue #2
	if fixerCtx.Options.RestoreMOVExtension && strings.EqualFold(filepath.Ext(fileName), ".mp4") {
		majorBrand, err := GetMajorBrand(sourcePath)
		if err == nil && strings.HasPrefix(majorBrand, "Apple QuickTime") {
			ext := filepath.Ext(fileName)
			newName := fileName[:len(fileName)-len(ext)] + ".mov"
			if ext == ".MP4" {
				newName = fileName[:len(fileName)-len(ext)] + ".MOV"
			}
			fileName = newName
		}
	}

	sidecarPath, err := FindSidecar(sourcePath, fixerCtx)
	if err != nil {
		Log(LoggerError, "Error finding sidecar for file %s: %v", sourcePath, err)
		return err
	}

	// If no sidecar is found and its a video file, try to find a partner image and use its sidecar
	if sidecarPath == "" && IsVideoFile(sourcePath) {
		partnerImage, err := FindImagePartner(sourcePath)
		if err == nil && partnerImage != "" {
			partnerSidecar, err := FindSidecar(partnerImage, fixerCtx)
			if err == nil && partnerSidecar != "" {
				sidecarPath = partnerSidecar
			}
		}
	}

	outputDir, err := ResolveOutputDir(fixerCtx, sourcePath, sidecarPath, sourceDirName, isYearFolder)
	if err != nil {
		return err
	}

	originalFileName := fileName

	if fixerCtx.Options.AppendDateToFilename {
		if fileDate, err := DetectFileDate(sourcePath, sidecarPath); err == nil {
			dateSuffix := fileDate.Format("2006-01-02")
			if fileDate.Hour() != 0 || fileDate.Minute() != 0 || fileDate.Second() != 0 {
				dateSuffix += fileDate.Format(" 15.04.05")
			}
			
			ext := filepath.Ext(fileName)
			base := strings.TrimSuffix(fileName, ext)
			
			// Robust check: don't append if the filename already contains this YYYY-MM-DD
			datePattern := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
			if !datePattern.MatchString(base) {
				fileName = base + " " + dateSuffix + ext
			}
		}
	}

	destPath := deduplicatePath(filepath.Join(outputDir, fileName))
	hasSidecar := sidecarPath != ""

	if !hasSidecar {
		Log(LoggerWarn, "No sidecar file found for %s — copying without metadata", sourcePath)
	}

	if err := CreateFixedFile(fixerCtx, sourcePath, sidecarPath, destPath, isYearFolder); err != nil {
		Log(LoggerError, "Error creating fixed file for %s: %v", sourcePath, err)
		return err
	}

	if fixerCtx.Options.DeduplicateOutput {
		newSize := int64(0)
		if info, err := os.Stat(destPath); err == nil {
			newSize = info.Size()
		}

		isDuplicate := false
		if existing, found := FindDuplicateMatch(fixerCtx, originalFileName); found {
			// Lazy-read EXIF only when a candidate match is found
			if existing.DateOriginal == "" {
				date, w, h, _ := ReadExifIdentity(existing.DestPath)
				existing.DateOriginal = date
				existing.ImageWidth = w
				existing.ImageHeight = h
				RegisterWrittenFile(fixerCtx, originalFileName, existing)
			}

			newDate, newW, newH, exifErr := ReadExifIdentity(destPath)
			exifMatch := exifErr == nil && newDate != "" &&
				newDate == existing.DateOriginal &&
				newW == existing.ImageWidth &&
				newH == existing.ImageHeight
			sizeMatch := newSize == existing.FileSize

			if exifMatch && sizeMatch {
				isDuplicate = true
				newIsBetter := false
				if hasSidecar && !existing.HasSidecar {
					newIsBetter = true
				} else if hasSidecar == existing.HasSidecar && len(originalFileName) > existing.BaseNameLen {
					newIsBetter = true
				}

				if newIsBetter {
					Log(LoggerInfo, "Dedup: replacing %s with better copy %s", filepath.Base(existing.DestPath), filepath.Base(destPath))
					if err := MoveToDuplicates(fixerCtx, existing.DestPath); err != nil {
						Log(LoggerWarn, "Failed to move duplicate %s: %v", existing.DestPath, err)
					}
				} else {
					Log(LoggerInfo, "Dedup: moving duplicate %s (keeping %s)", filepath.Base(destPath), filepath.Base(existing.DestPath))
					if err := MoveToDuplicates(fixerCtx, destPath); err != nil {
						Log(LoggerWarn, "Failed to move duplicate %s: %v", destPath, err)
					}
				}
			}
		}

		if !isDuplicate {
			RegisterWrittenFile(fixerCtx, originalFileName, WrittenFile{
				DestPath: destPath, HasSidecar: hasSidecar,
				FileSize: newSize, BaseNameLen: len(originalFileName),
			})
		}
	}

	return nil
}

func CreateFixedFile(
	fixerCtx *FixerContext,
	filePath string,
	fileMetadataPath string,
	destPath string,
	isYearFolder bool,
) error {
	// Ensure output directory exists (create if not)
	destDir := filepath.Dir(destPath)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}

		// Invalidate OutputRoot cache so newly created year folders are visible for symlinks
		ClearCacheDir(fixerCtx.OutputRoot)
	}

	fileName := filepath.Base(destPath)

	if fixerCtx.Options.UseSymlinks && !isYearFolder {
		monthFolder := ""
		if fixerCtx.Options.MonthSubfolders {
			fileDate, err := DetectFileDate(filePath, fileMetadataPath)
			if err == nil {
				monthFolder = fileDate.Format("2006-01")
			}
		}

		// Attempt to find the file inside of any year folder in the output
		entries, _ := ReadDirCached(fixerCtx.OutputRoot)
		for _, curEntry := range entries {
			if !curEntry.IsDir() {
				continue
			}

			isYear, _ := IsYearFolder(curEntry.Name())
			if !isYear {
				continue
			}

			targetPaths := []string{}
			if monthFolder != "" {
				targetPaths = append(targetPaths, filepath.Join(fixerCtx.OutputRoot, curEntry.Name(), monthFolder, fileName))
			}
			targetPaths = append(targetPaths, filepath.Join(fixerCtx.OutputRoot, curEntry.Name(), fileName))

			for _, target := range targetPaths {
				if _, err := os.Stat(target); err == nil {
					if err := os.Symlink(target, destPath); err != nil {
						// Symlink failed, continue with normal copy
						if !os.IsExist(err) {
							return fmt.Errorf("Failed to create symlink: %w", err)
						}
					} else {
						// Symlink successful
						return nil
					}
				}
			}
		}
	}

	if err := DuplicateFile(filePath, destPath); err != nil {
		return err
	}

	if fixerCtx.Options.WriteMetadata && fileMetadataPath != "" {
		metadata, err := ReadJsonMetadata(fileMetadataPath)
		if err != nil {
			Log(LoggerError, "Failed to read metadata from %s: %v", fileMetadataPath, err)
		} else {
			// Only apply metadata if reading was successful
			err = ApplyMetadata(destPath, metadata)
			if err != nil {
				Log(LoggerError, "Failed to apply metadata to %s: %v", destPath, err)
			}
		}
	} else if fixerCtx.Options.WriteMetadata && fileMetadataPath == "" {
		Log(LoggerInfo, "WriteMetadata enabled but no sidecar for %s — attempting EXIF/filename date", fileName)

		if exifDate, err := ReadExifDate(filePath); err == nil {
			Log(LoggerInfo, "Found EXIF date for %s: %s", fileName, exifDate.Format("2006-01-02 15:04:05"))
			if err := SetFileBirthTime(destPath, exifDate); err != nil {
				Log(LoggerWarn, "Failed to set birth time from EXIF for %s: %v", fileName, err)
			}
		} else if fileDate, ok := parseDateFromFileName(filepath.Base(filePath)); ok {
			Log(LoggerInfo, "Using filename date for %s: %s", fileName, fileDate.Format("2006-01-02 15:04:05"))
			if err := SetFileBirthTime(destPath, fileDate); err != nil {
				Log(LoggerWarn, "Failed to set birth time from filename for %s: %v", fileName, err)
			}
		}
	}

	return nil
}
