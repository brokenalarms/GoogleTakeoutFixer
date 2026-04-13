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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache for directory entries to prevent excessive disk reads (issue #5)
var (
	dirCache     = make(map[string][]os.DirEntry)
	dirLookup    = make(map[string]map[string]string) // dir -> lowercase name -> actual name
	dirCacheLock sync.RWMutex
)

// ReadDirCached returns cached directories or reads them it not present
func ReadDirCached(dir string) ([]os.DirEntry, error) {
	dirCacheLock.RLock()
	entries, ok := dirCache[dir]
	dirCacheLock.RUnlock()

	// Cache hit, return entries
	if ok {
		return entries, nil
	}

	dirCacheLock.Lock()
	defer dirCacheLock.Unlock()

	// Check again in case it was created while waiting for lock
	if entries, ok = dirCache[dir]; ok {
		return entries, nil
	}

	// Read directory and cache results
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	dirCache[dir] = entries

	// Build a lookup map for O(1) case-insensitive search
	lookup := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			lookup[strings.ToLower(entry.Name())] = entry.Name()
		}
	}
	dirLookup[dir] = lookup

	return entries, nil
}

// ClearCache clears the directory cache for all paths
func ClearCache() {
	dirCacheLock.Lock()
	defer dirCacheLock.Unlock()
	// Reallocate map to clear everything
	dirCache = make(map[string][]os.DirEntry)
	dirLookup = make(map[string]map[string]string)
}

// ClearCacheDir clears the directory cache for a specific path
func ClearCacheDir(dir string) {
	dirCacheLock.Lock()
	defer dirCacheLock.Unlock()
	delete(dirCache, dir)
	delete(dirLookup, dir)
}

// All media extension to differ between media files and other files
var imageExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".heic": {},
	".webp": {},
	".gif":  {},
	".tiff": {},
	".tif":  {},
	".bmp":  {},
}

var videoExtensions = map[string]struct{}{
	".mp4": {},
	".mov": {},
	".avi": {},
	".mkv": {},
	".m4v": {},
	".3gp": {},
	".wmv": {},
}

// Checks whether a file is a video file based on its extension
func IsVideoFile(path string) bool {
	extension := filepath.Ext(path)
	_, ok := videoExtensions[strings.ToLower(extension)]
	return ok
}

func deduplicatePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)

	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

var trailingParenNum = regexp.MustCompile(`\s*\(\d+\)$`)

// DedupKey returns a canonical lowercase filename for dedup lookup.
// Strips any trailing parenthesized number like (1), (2) before the extension,
// which Google adds for duplicate uploads.
func DedupKey(fileName string) string {
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	base = trailingParenNum.ReplaceAllString(base, "")
	return strings.ToLower(base + ext)
}

// FindDuplicateMatch checks if a file with the same original name has already been written
func FindDuplicateMatch(fixerCtx *FixerContext, originalFileName string) (WrittenFile, bool) {
	if wf, ok := fixerCtx.WrittenFiles[DedupKey(originalFileName)]; ok {
		return wf, true
	}
	return WrittenFile{}, false
}

// RegisterWrittenFile records a file in the dedup map under its original source filename
func RegisterWrittenFile(fixerCtx *FixerContext, originalFileName string, wf WrittenFile) {
	fixerCtx.WrittenFiles[DedupKey(originalFileName)] = wf
}

// MoveToDuplicates moves a file to the _duplicates folder preserving the relative path structure
func MoveToDuplicates(fixerCtx *FixerContext, filePath string) error {
	relPath, err := filepath.Rel(fixerCtx.OutputRoot, filePath)
	if err != nil {
		return err
	}
	dupPath := filepath.Join(fixerCtx.OutputRoot, "_duplicates", relPath)
	dupDir := filepath.Dir(dupPath)
	if err := os.MkdirAll(dupDir, 0755); err != nil {
		return err
	}

	// Retry mechanism for macOS Error -36
	var lastErr error
	for i := 0; i < 3; i++ {
		if err := os.Rename(filePath, dupPath); err == nil {
			return nil
		} else {
			lastErr = err
			time.Sleep(500 * time.Millisecond) // Wait for OS to release metadata locks
		}
	}
	return fmt.Errorf("failed to move to duplicates after 3 attempts: %w", lastErr)
}

// Duplicate a file from one path to another
func DuplicateFile(inputPath string, outputPath string) error {
	// On macOS (darwin), try os.Link first for "instant" copies if on same volume
	if runtime.GOOS == "darwin" {
		if err := os.Link(inputPath, outputPath); err == nil {
			return nil
		}
		// If Link fails (different volume), fall back to standard copy
	}

	sourceFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// Discover directories within a path non recursively
func DiscoverDirs(path string) ([]os.DirEntry, error) {
	var dirList []os.DirEntry

	files, err := os.ReadDir(path)

	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			dirList = append(dirList, file)
		}
	}

	return dirList, nil
}

// Find a matching sidecar JSON by searching in multiple locations.
func FindSidecar(imagePath string, fixerCtx *FixerContext) (string, error) {
	fileName := filepath.Base(imagePath)
	Log(LoggerInfo, "FindSidecar: Searching for sidecar for %s", fileName)

	// Define the core search logic as a closure so we can reuse it.
	searchLogic := func(dirToSearch string) (string, error) {
		ext := filepath.Ext(fileName)
		base := strings.TrimSuffix(fileName, ext)

		_, err := ReadDirCached(dirToSearch)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", err
		}

		dirCacheLock.RLock()
		lookup := dirLookup[dirToSearch]
		entries := dirCache[dirToSearch]
		dirCacheLock.RUnlock()

		targets := []string{
			strings.ToLower(fileName + ".json"),
			strings.ToLower(base + ".json"),
			strings.ToLower(fileName + ".supplemental-metadata.json"),
			strings.ToLower(base + ".supplemental-metadata.json"),
			strings.ToLower(fileName + ".." + ".json"),
		}

		if strings.Contains(base, "(") && strings.HasSuffix(base, ")") {
			start := strings.LastIndex(base, "(")
			parenCleanBase := base[:start]
			parenSuffix := base[start:]
			targets = append(targets,
				strings.ToLower(parenCleanBase+ext+parenSuffix+".json"),
				strings.ToLower(base+".json"),
				strings.ToLower(parenCleanBase+ext+".supplemental-metadata"+parenSuffix+".json"),
			)
		}

		cleanBase := base
		wasCleaned := false
		if strings.HasSuffix(cleanBase, "-edited") {
			cleanBase = strings.TrimSuffix(cleanBase, "-edited")
			wasCleaned = true
		}
		if regexp.MustCompile(`\([0-9]+\)$`).MatchString(cleanBase) {
			cleanBase = regexp.MustCompile(`\([0-9]+\)$`).ReplaceAllString(cleanBase, "")
			wasCleaned = true
		}
		if regexp.MustCompile(`~[0-9]+$`).MatchString(cleanBase) {
			cleanBase = regexp.MustCompile(`~[0-9]+$`).ReplaceAllString(cleanBase, "")
			wasCleaned = true
		}

		if wasCleaned {
			targets = append(targets,
				strings.ToLower(cleanBase+ext+".json"),
				strings.ToLower(cleanBase+".json"),
				strings.ToLower(cleanBase+ext+".supplemental-metadata.json"),
				strings.ToLower(cleanBase+".supplemental-metadata.json"),
			)
		}

		// O(1) lookup
		for _, target := range targets {
			if actualName, ok := lookup[target]; ok {
				return filepath.Join(dirToSearch, actualName), nil
			}
		}

		// Fallback O(N) only for truncated prefixes
		prefix := strings.ToLower(cleanBase)
		if len(prefix) > 46 {
			prefix = prefix[:46]
			for _, entry := range entries {
				if !entry.IsDir() {
					lower := strings.ToLower(entry.Name())
					if strings.HasSuffix(lower, ".json") && strings.HasPrefix(lower, prefix) {
						return filepath.Join(dirToSearch, entry.Name()), nil
					}
				}
			}
		}
		return "", nil
	}

	// --- Phase 1: Search in the same directory as the media file ---
	currentDir := filepath.Dir(imagePath)
	sidecarPath, err := searchLogic(currentDir)
	if err != nil {
		return "", err
	}
	if sidecarPath != "" {
		Log(LoggerInfo, "FindSidecar: SUCCESS, found sidecar in local directory.")
		return sidecarPath, nil
	}

	// --- Phase 2: If not found, search in the root of the Google Photos folder ---
	if fixerCtx != nil && currentDir != fixerCtx.SourceRoot {
		Log(LoggerInfo, "FindSidecar: Not found locally. Searching in root: %s", fixerCtx.SourceRoot)
		sidecarPath, err = searchLogic(fixerCtx.SourceRoot)
		if err != nil {
			return "", err
		}
		if sidecarPath != "" {
			Log(LoggerInfo, "FindSidecar: SUCCESS, found sidecar in root directory.")
			return sidecarPath, nil
		}
	}

	// --- Phase 3: If still not found, check the corresponding "Photos from YYYY" folder ---
	var year string
	if t, ok := parseDateFromFileName(fileName); ok {
		year = strconv.Itoa(t.Year())
	}
	if year != "" && fixerCtx != nil {
		yearFolderNames := []string{"Photos from " + year, year}
		for _, yearFolderName := range yearFolderNames {
			yearFolderPath := filepath.Join(fixerCtx.SourceRoot, yearFolderName)
			if _, statErr := os.Stat(yearFolderPath); statErr == nil {
				Log(LoggerInfo, "FindSidecar: Not found. Searching in year folder: %s", yearFolderPath)
				sidecarPath, err = searchLogic(yearFolderPath)
				if err != nil {
					return "", err
				}
				if sidecarPath != "" {
					Log(LoggerInfo, "FindSidecar: SUCCESS, found sidecar in year folder.")
					return sidecarPath, nil
				}
			}
		}
	}

	// --- Phase 4: Search matching directories across all source roots ---
	if fixerCtx != nil && len(fixerCtx.AllRoots) > 1 {
		currentDirName := filepath.Base(currentDir)
		for _, root := range fixerCtx.AllRoots {
			if root == fixerCtx.SourceRoot {
				continue
			}

			// Search in the equivalent directory name in other roots
			otherDir := filepath.Join(root, currentDirName)
			if _, statErr := os.Stat(otherDir); statErr == nil {
				sidecarPath, err = searchLogic(otherDir)
				if err != nil {
					return "", err
				}
				if sidecarPath != "" {
					Log(LoggerInfo, "FindSidecar: SUCCESS, found sidecar in other root: %s", root)
					return sidecarPath, nil
				}
			}

			// Also search year folders in other roots
			if year != "" {
				for _, yearFolderName := range []string{"Photos from " + year, year} {
					yearFolderPath := filepath.Join(root, yearFolderName)
					if _, statErr := os.Stat(yearFolderPath); statErr == nil {
						sidecarPath, err = searchLogic(yearFolderPath)
						if err != nil {
							return "", err
						}
						if sidecarPath != "" {
							Log(LoggerInfo, "FindSidecar: SUCCESS, found sidecar in other root year folder: %s", yearFolderPath)
							return sidecarPath, nil
						}
					}
				}
			}
		}
	}

	Log(LoggerWarn, "FindSidecar: FAILED to find any sidecar for %s in any location.", fileName)
	return "", nil
}

// Checks if the file at the given path has the specified extension
func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}

// Year folder prefixes of some countries
var yearPrefixes = []string{
	"Photos from ",     // English
	"Fotos von ",       // German
	"Photos de ",       // French
	"Foto del ",        // Italian
	"Fotos de ",        // Spanish / Portuguese
	"Foto's van ",      // Dutch
	"Zdjęcia z ",       // Polish
	"Фотографии из ",   // Russian
	"Foton från ",      // Swedish
	"Bilder fra ",      // Norwegian
	"Billeder fra ",    // Danish
	"Fotoğraflar ",     // Turkish
	"Fotografie z ",    // Czech
	"Fotók a ",         // Hungarian
	"Φωτογραφίες από ", // Greek
	"Fotografii din ",  // Romanian
	"Foto dari ",       // Indonesian
	"รูปภาพจาก ",       // Thai
	"Ảnh từ ",          // Vietnamese
}

// Checks whether a directory is a standard google year folder
func IsYearFolder(dirPath string) (bool, error) {
	for _, prefix := range yearPrefixes {
		if strings.HasPrefix(dirPath, prefix) {
			yearPart := strings.TrimPrefix(dirPath, prefix)
			if matched, _ := regexp.MatchString(`^\d{4}$`, yearPart); matched {
				return true, nil
			}
		}
	}
	return false, nil
}

// Extracts the year string from a year folder name by stripping the localized prefix
func ExtractYearFromFolder(dirName string) string {
	for _, prefix := range yearPrefixes {
		if strings.HasPrefix(dirName, prefix) {
			yearPart := strings.TrimPrefix(dirName, prefix)
			if matched, _ := regexp.MatchString(`^\d{4}$`, yearPart); matched {
				return yearPart
			}
		}
	}
	return dirName
}

// Checks whether a file, that is provided using its path, is a media file
func IsMediaFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	_, isImage := imageExtensions[extension]
	_, isVideo := videoExtensions[extension]
	return isImage || isVideo
}

// Attempts to find an image file with the same base name as the video file
func FindImagePartner(videoPath string) (string, error) {
	if !IsVideoFile(videoPath) {
		return "", nil
	}

	dir := filepath.Dir(videoPath)
	extension := filepath.Ext(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), extension)

	// Check all image extensions for a match
	for imgExt := range imageExtensions {
		candidate := filepath.Join(dir, base+imgExt)

		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Try uppercase extension
		candidateUpper := filepath.Join(dir, base+strings.ToUpper(imgExt))
		if _, err := os.Stat(candidateUpper); err == nil {
			return candidateUpper, nil
		}
	}

	return "", nil
}

// FindSourceRoots returns directories that contain the expected Google Photos
// folder structure (subdirectories with media files).
func FindSourceRoots(path string) ([]string, error) {
	if dirHasMediaSubdirs(path) {
		return []string{path}, nil
	}

	subdirs, err := DiscoverDirs(path)
	if err != nil {
		return nil, err
	}

	var roots []string
	for _, sub := range subdirs {
		child := filepath.Join(path, sub.Name())
		if dirHasMediaSubdirs(child) {
			roots = append(roots, child)
		} else {
			grandchildren, _ := DiscoverDirs(child)
			for _, gc := range grandchildren {
				gcPath := filepath.Join(child, gc.Name())
				if dirHasMediaSubdirs(gcPath) {
					roots = append(roots, gcPath)
				}
			}
		}
	}

	if len(roots) == 0 {
		return nil, fmt.Errorf("no media files found in folder structure")
	}
	return roots, nil
}

func dirHasMediaSubdirs(path string) bool {
	subdirs, err := DiscoverDirs(path)
	if err != nil {
		return false
	}
	for _, sub := range subdirs {
		files, err := os.ReadDir(filepath.Join(path, sub.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && IsMediaFile(f.Name()) {
				return true
			}
		}
	}
	return false
}

// Counts all processable files across one or more source roots
func CountProcessableFiles(sourcePath string, options ProcessOptions) (int, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}

	if !fileInfo.IsDir() {
		return 0, fmt.Errorf("source path is not a directory")
	}

	roots, err := FindSourceRoots(sourcePath)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, root := range roots {
		subdirs, err := DiscoverDirs(root)
		if err != nil {
			continue
		}
		for _, dir := range subdirs {
			isYearFolder, _ := IsYearFolder(dir.Name())
			if options.IgnoreAlbums && !isYearFolder {
				continue
			}

			files, _ := os.ReadDir(filepath.Join(root, dir.Name()))
			for _, file := range files {
				if !file.IsDir() && IsMediaFile(file.Name()) {
					count++
				}
			}
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("no media files found in folder structure")
	}
	return count, nil
}

// DetectFileDate returns the file's date from sidecar, EXIF, or filename
func DetectFileDate(sourcePath string, sidecarPath string) (time.Time, error) {
	if sidecarPath != "" {
		metadata, err := ReadJsonMetadata(sidecarPath)
		if err == nil {
			timestamp, err := strconv.ParseInt(metadata.PhotoTakenTime.Timestamp, 10, 64)
			if err == nil {
				return time.Unix(timestamp, 0), nil
			}
		}
	}

	fileName := filepath.Base(sourcePath)
	if t, ok := parseDateFromFileName(fileName); ok {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("no date found for %s", filepath.Base(sourcePath))
}

// DetectFileMonth returns the month as 1-12
func DetectFileMonth(sourcePath string, sidecarPath string) (int, error) {
	t, err := DetectFileDate(sourcePath, sidecarPath)
	if err == nil {
		return int(t.Month()), nil
	}
	
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}
	return int(fileInfo.ModTime().Month()), nil
}

const dateSep = `[-:._]?`
const dateTimeSep = `[-:._ ]?`

var dateTimeRe = regexp.MustCompile(
	`(?:^|[^0-9])` +
		`(\d{4})` + dateSep + `(\d{2})` + dateSep + `(\d{2})` +
		`(?:` + dateTimeSep + `(\d{2})` + dateSep + `(\d{2})` + dateSep + `(\d{2}))?`,
)

func parseDateFromFileName(fileName string) (time.Time, bool) {
	name := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	m := dateTimeRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}

	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])

	if year < 1970 || year > 2100 || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}

	hour, min, sec := 0, 0, 0
	if m[4] != "" {
		hour, _ = strconv.Atoi(m[4])
		min, _ = strconv.Atoi(m[5])
		sec, _ = strconv.Atoi(m[6])
	}

	t := time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC)
	return t, true
}

func ResolveOutputDir(
	fixerCtx *FixerContext,
	sourcePath string,
	sidecarPath string,
	sourceDirName string,
	isYearFolder bool,
) (string, error) {
	targetDir := fixerCtx.OutputRoot

	if !fixerCtx.Options.Flatten {
		if isYearFolder {
			folderYear := ExtractYearFromFolder(sourceDirName)
			fileName := filepath.Base(sourcePath)
			fileNameDate, hasFileNameDate := parseDateFromFileName(fileName)

			if fixerCtx.Options.PreferFilenameOverSidecar && hasFileNameDate {
				detectedYear := strconv.Itoa(fileNameDate.Year())
				if detectedYear != folderYear {
					Log(LoggerInfo, "Re-sorting %s from %s to %s (filename timestamp preferred over sidecar)", fileName, folderYear, detectedYear)
				}
				targetDir = filepath.Join(targetDir, detectedYear)
			} else {
				fileDate, err := DetectFileDate(sourcePath, sidecarPath)
				if err == nil {
					detectedYear := strconv.Itoa(fileDate.Year())
					if detectedYear != folderYear {
						if hasFileNameDate {
							Log(LoggerWarn, "File %s has filename date %d but sidecar/EXIF says %d (enable 'Prefer filename over sidecar' to use filename)", fileName, fileNameDate.Year(), fileDate.Year())
						}
					}
					targetDir = filepath.Join(targetDir, detectedYear)
				} else {
					targetDir = filepath.Join(targetDir, folderYear)
				}
			}
		} else if sourceDirName != "" {
			targetDir = filepath.Join(targetDir, sourceDirName)
		}
	}

	if !fixerCtx.Options.MonthSubfolders && !fixerCtx.Options.DateFolders {
		return targetDir, nil
	}

	// Determine the best available date for folder organization
	var fileDate time.Time
	var hasDate bool

	if fixerCtx.Options.PreferFilenameOverSidecar {
		if t, ok := parseDateFromFileName(filepath.Base(sourcePath)); ok {
			fileDate = t
			hasDate = true
		}
	}

	if !hasDate {
		if t, err := DetectFileDate(sourcePath, sidecarPath); err == nil {
			fileDate = t
			hasDate = true
		}
	}

	if !hasDate {
		// Ultimate fallback: use file system modification time
		if info, err := os.Stat(sourcePath); err == nil {
			fileDate = info.ModTime()
		} else {
			// If we can't even stat the file, just return what we have
			return targetDir, nil
		}
	}

	// Now apply folder structure in order
	if fixerCtx.Options.MonthSubfolders {
		targetDir = filepath.Join(targetDir, fileDate.Format("2006-01"))
	}

	if fixerCtx.Options.DateFolders {
		targetDir = filepath.Join(targetDir, fileDate.Format("2006-01-02"))
	}

	return targetDir, nil
}
