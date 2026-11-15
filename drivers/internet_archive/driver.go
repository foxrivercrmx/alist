package internet_archive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
)

type InternetArchive struct {
	model.Storage
	Addition
}

func (d *InternetArchive) Config() driver.Config {
	return config
}

func (d *InternetArchive) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *InternetArchive) Init(ctx context.Context) error {
	if d.Identifier == "" {
		return fmt.Errorf("identifier is required")
	}
	return nil
}

func (d *InternetArchive) Drop(ctx context.Context) error {
	return nil
}

// Internet Archive metadata structure adapted with key "result"
type IAMetadata struct {
	Result []struct {
		Name   string `json:"name"`
		Size   string `json:"size"`
		Mtime  string `json:"mtime"`
		Format string `json:"format"`
	} `json:"result"`
}

// Structure for files within ZIP
type ZipFileInfo struct {
	Name     string
	Size     int64
	Modified time.Time
}

// Helper functions for ZIP path handling
func isZipPath(path string) bool {
	return strings.Contains(path, ".zip/")
}

func extractZipPath(fullPath string) (zipName, innerPath string) {
	// Example: "file.zip/sub-folder/sub-folder2/"
	// → zipName: "file.zip", innerPath: "sub-folder/sub-folder2/"

	if idx := strings.Index(fullPath, ".zip/"); idx > 0 {
		zipName = fullPath[:idx+4]   // ".zip"
		innerPath = fullPath[idx+5:] // after ".zip/"
		return zipName, innerPath
	}

	// Path is the ZIP file itself (without inner path), e.g. "dir/file.zip"
	base := strings.TrimSuffix(fullPath, "/")
	if isZipFile(base) {
		return base, ""
	}
	return "", ""
}

func splitZipPath(fullPath string) (zipPart, filePart string) {
	// Example: "file.zip/path/to/file.txt"
	// → zipPart: "file.zip", filePart: "path/to/file.txt"

	if idx := strings.Index(fullPath, ".zip/"); idx > 0 {
		zipPart = fullPath[:idx+4]
		filePart = fullPath[idx+5:]
	}
	return zipPart, filePart
}

func isZipFile(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".zip")
}

func (d *InternetArchive) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	dirPath := dir.GetPath()

	// Browse within ZIP, either when already inside a ZIP sub-folder
	// (path contains ".zip/") or when opening the ZIP file itself
	// (path ends with ".zip"). Content within ZIP is not available via
	// metadata API, so it must be fetched from Archive.org HTML listing.
	if isZipPath(dirPath) || isZipFile(strings.TrimSuffix(dirPath, "/")) {
		return d.listZipContents(ctx, dirPath)
	}

	// Browse regular files in identifier
	return d.listRegularFiles(ctx, dirPath)
}

func (d *InternetArchive) listZipContents(ctx context.Context, dirPath string) ([]model.Obj, error) {
	// Check if ZIP browsing is enabled
	if !d.EnableZipBrowsing {
		return nil, fmt.Errorf("ZIP browsing is disabled")
	}

	// Extract ZIP name and path within ZIP
	zipName, innerPath := extractZipPath(dirPath)
	if zipName == "" {
		return nil, fmt.Errorf("invalid ZIP path format: %s", dirPath)
	}

	// Normalize paths
	zipName = strings.TrimPrefix(zipName, "/")
	innerPath = strings.TrimPrefix(innerPath, "/")

	// Ensure innerPath has trailing slash if not empty
	if innerPath != "" && !strings.HasSuffix(innerPath, "/") {
		innerPath = innerPath + "/"
	}

	// Archive.org only returns full listing (recursive, containing all
	// paths from ZIP root) at the ZIP root URL. Adding sub-path to URL
	// (e.g. .../file.zip/subdir/) gets redirected to
	// view_archive.php?...&file=subdir/ whose body is EMPTY, resulting in
	// empty listing. Therefore listing is ALWAYS fetched from ZIP root and
	// per-folder filtering is done locally in parseZipListingHTML
	// using currentPath.
	archiveURL := fmt.Sprintf("https://archive.org/download/%s/%s/", d.Identifier, zipName)

	// Fetch HTML content from Archive.org
	req, err := http.NewRequestWithContext(ctx, "GET", archiveURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch ZIP contents: status %d", resp.StatusCode)
	}

	// Read HTML response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse HTML to get file list
	return d.parseZipListingHTML(body, zipName, innerPath)
}

func (d *InternetArchive) parseZipListingHTML(html []byte, zipName, currentPath string) ([]model.Obj, error) {
	// Archive.org renders ZIP contents listing as an HTML "archext" table.
	// Each row has the form:
	//   file  : <tr><td><a href="...">path/file.ext</a><td><td>YYYY-MM-DD HH:MM<td id="size">SIZE</tr>
	//   folder: <tr><td>path/folder/<td><td>YYYY-MM-DD HH:MM<td id="size"></tr>
	// Names are NOT separated from timestamp by whitespace but by HTML tags,
	// so whitespace-based splitting cannot be used. Names can also contain
	// spaces (e.g. "Jak v2"), so <td> content is taken as-is.
	//
	// Group 1: name (folders always end with "/")
	// Group 2: timestamp (YYYY-MM-DD HH:MM)
	// Group 3: size (empty for folders)
	re := regexp.MustCompile(`<tr><td>(?:<a href="[^"]*">)?([^<]+)(?:</a>)?<td><td>([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2})<td[^>]*>([0-9]*)</tr>`)
	matches := re.FindAllStringSubmatch(string(html), -1)

	var files []model.Obj
	addedFolders := make(map[string]bool)

	addFolder := func(name string) {
		if name == "" || addedFolders[name] {
			return
		}
		addedFolders[name] = true
		files = append(files, &model.Object{
			Name:     name,
			Size:     0,
			Modified: time.Now(),
			IsFolder: true,
		})
	}

	for _, match := range matches {
		if len(match) < 4 {
		continue // incomplete: full, name, timestamp, size
		}

		name := match[1]
		mtimeStr := match[2]
		sizeStr := match[3]

		// Folders always end with "/" in Archive.org listing
		isFolderEntry := strings.HasSuffix(name, "/")

		// Only process entries that are under currentPath
		if currentPath != "" {
			if !strings.HasPrefix(name, currentPath) {
				continue
			}
			name = strings.TrimPrefix(name, currentPath)
		}
		if name == "" {
			continue // entry for currentPath itself
		}

		// Parse modification time from timestamp
		mtime, err := time.Parse("2006-01-02 15:04", mtimeStr)
		if err != nil {
			mtime = time.Now()
		}

		if isFolderEntry {
			// Folder entry: "path/folder/"
			folderName := strings.TrimSuffix(name, "/")
			// Only add folder at the first level
			if !strings.Contains(folderName, "/") {
				addFolder(folderName)
			}
			continue
		}

		// File in sub-folder: just add first-level folder,
		// file will appear when that folder is opened.
		if idx := strings.Index(name, "/"); idx >= 0 {
			addFolder(name[:idx])
			continue
		}

		// File directly in this directory
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		files = append(files, &model.Object{
			Name:     name,
			Size:     size,
			Modified: mtime,
			IsFolder: false,
		})
	}

	return files, nil
}

func (d *InternetArchive) listRegularFiles(ctx context.Context, dirPath string) ([]model.Obj, error) {
	apiURL := fmt.Sprintf("https://archive.org/metadata/%s/files", d.Identifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch metadata: status %d", res.StatusCode)
	}

	var metadata IAMetadata
	if err := json.NewDecoder(res.Body).Decode(&metadata); err != nil {
		return nil, err
	}

	// Normalize dirPath
	normalizedDirPath := ""
	if dirPath == "/" || dirPath == "" {
		normalizedDirPath = ""
	} else {
		normalizedDirPath = strings.TrimPrefix(dirPath, "/") + "/"
	}

	var files []model.Obj
	addedItems := make(map[string]bool) // To prevent duplicates

	for _, f := range metadata.Result {
		// Filter unnecessary files
		if strings.HasPrefix(f.Name, ".") || f.Format == "Metadata" {
			continue
		}

		// Filter by directory path
		if normalizedDirPath != "" && !strings.HasPrefix(f.Name, normalizedDirPath) {
			continue
		}

		// Get relative path from current directory
		relPath := strings.TrimPrefix(f.Name, normalizedDirPath)
		if relPath == "" {
			continue // Skip if same as directory path
		}

		// Parse size and mtime
		size, _ := strconv.ParseInt(f.Size, 10, 64)
		mtimeInt, err := strconv.ParseInt(f.Mtime, 10, 64)
		var mtime time.Time
		if err == nil {
			mtime = time.Unix(mtimeInt, 0)
		} else {
			mtime = time.Now()
		}

		// Split for path analysis
		parts := strings.Split(relPath, "/")

		if len(parts) > 1 {
			// This is a file in a sub-directory
			firstPart := parts[0]
			key := firstPart + "_dir" // Unique key for folder

			if !addedItems[key] {
				// Check if this is a ZIP file
				if isZipFile(firstPart) {
					// ZIP file: only display as folder if ZIP browsing enabled
					if d.EnableZipBrowsing {
						files = append(files, &model.Object{
							Name:     firstPart,
							Size:     size,
							Modified: mtime,
							IsFolder: true,
						})
					}
					// If ZIP browsing disabled, skip (not displayed as folder)
				} else {
					// Regular folder
					files = append(files, &model.Object{
						Name:     firstPart,
						Size:     0,
						Modified: time.Now(),
						IsFolder: true,
					})
				}
				addedItems[key] = true
			}
		} else {
			// This is a file directly in this directory
			// Check if this is a ZIP file
			if isZipFile(relPath) {
				if d.EnableZipBrowsing {
					// ZIP file: display as folder if ZIP browsing enabled
					files = append(files, &model.Object{
						Name:     relPath,
						Size:     size,
						Modified: mtime,
						IsFolder: true, // Display as folder for browsing
					})
				} else {
					// ZIP file: display as regular file if ZIP browsing disabled
					files = append(files, &model.Object{
						Name:     relPath,
						Size:     size,
						Modified: mtime,
						IsFolder: false, // Display as regular file
					})
				}
			} else {
				// Regular file
				files = append(files, &model.Object{
					Name:     relPath,
					Size:     size,
					Modified: mtime,
					IsFolder: false,
				})
			}
		}
	}

	return files, nil
}

func (d *InternetArchive) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	filePath := file.GetPath()

	// Handle various scenarios:
	// 1. Regular file: /identifier/path/to/file.txt
	// 2. File within ZIP: /identifier/file.zip/path/to/file.txt
	// 3. ZIP file itself: /identifier/file.zip

	if isZipPath(filePath) {
		// File is within ZIP
		zipPart, filePart := splitZipPath(filePath)
		if zipPart == "" || filePart == "" {
			return nil, fmt.Errorf("invalid ZIP file path: %s", filePath)
		}

		// Clean paths
		cleanZipPart := strings.TrimPrefix(zipPart, "/")
		cleanFilePart := strings.TrimPrefix(filePart, "/")

		// Encode ZIP name (keep "/" as path separator) to ensure names
		// with spaces/special characters remain valid URLs.
		encodedZipPart := (&url.URL{Path: cleanZipPart}).EscapedPath()
		// Archive.org uses "%2F" separator for paths within ZIP.
		encodedFilePart := url.PathEscape(cleanFilePart)

		urlStr := fmt.Sprintf("https://archive.org/download/%s/%s/%s",
			d.Identifier, encodedZipPart, encodedFilePart)

		return &model.Link{
			URL: urlStr,
		}, nil
	}

	// Handle ZIP file itself (not file within ZIP)
	if isZipFile(filePath) {
		// This is a full ZIP file download
		cleanPath := path.Clean(strings.TrimPrefix(filePath, "/"))
		encodedPath := (&url.URL{Path: cleanPath}).EscapedPath()

		urlStr := fmt.Sprintf("https://archive.org/download/%s/%s", d.Identifier, encodedPath)

		return &model.Link{
			URL: urlStr,
		}, nil
	}

	// Existing logic for regular files
	cleanPath := path.Clean(strings.TrimPrefix(filePath, "/"))
	encodedPath := (&url.URL{Path: cleanPath}).EscapedPath()

	urlStr := fmt.Sprintf("https://archive.org/download/%s/%s", d.Identifier, encodedPath)

	return &model.Link{
		URL: urlStr,
	}, nil
}

// Implement other interface methods that return errs.NotImplement

func (d *InternetArchive) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) Remove(ctx context.Context, obj model.Obj) error {
	return errs.NotImplement
}

func (d *InternetArchive) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) GetArchiveMeta(ctx context.Context, obj model.Obj, args model.ArchiveArgs) (model.ArchiveMeta, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) ListArchive(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) ([]model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) Extract(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) (*model.Link, error) {
	return nil, errs.NotImplement
}

func (d *InternetArchive) ArchiveDecompress(ctx context.Context, srcObj, dstDir model.Obj, args model.ArchiveDecompressArgs) ([]model.Obj, error) {
	return nil, errs.NotImplement
}

// Ensure InternetArchive struct satisfies driver.Driver interface
var _ driver.Driver = (*InternetArchive)(nil)
