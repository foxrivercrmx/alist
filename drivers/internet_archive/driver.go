package internet_archive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/utils"
)

// Driver configuration structure
type Addition struct {
	Identifier string `json:"identifier" required:"true" help:"Internet Archive Item Identifier (e.g., from archive.org/details/IDENTIFIER)"`
}

type InternetArchive struct {
	model.Storage
	Addition
}

func (d *InternetArchive) Config() driver.Config {
	return driver.Config{
		Name:        "InternetArchive",
		LocalSort:   true,
		OnlyLocal:   false,
		OnlyProxy:   false, // Explicitly allow 302 redirects
		NoCache:     false,
		NoUpload:    true,  // Read-only driver
		NeedMs:      false,
		DefaultRoot: "/",
	}
}

func (d *InternetArchive) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *InternetArchive) Init(ctx context.Context) error {
	// Optional: Validate that the identifier exists during initialization
	if d.Identifier == "" {
		return fmt.Errorf("identifier cannot be empty")
	}
	return nil
}

func (d *InternetArchive) Drop(ctx context.Context) error {
	return nil
}

// Structure to parse IA Metadata API response
type IAMetadata struct {
	Files []struct {
		Name  string `json:"name"`
		Size  string `json:"size"`
		Mtime string `json:"mtime"`
		Format string `json:"format"`
	} `json:"files"`
}

func (d *InternetArchive) List(ctx context.Context, dir model.Path, args model.ListArgs) ([]model.Obj, error) {
	// IA's metadata API endpoint
	apiURL := fmt.Sprintf("https://archive.org/metadata/%s/files", d.Identifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch metadata: status %d", res.StatusCode)
	}

	var metadata IAMetadata
	if err := json.NewDecoder(res.Body).Decode(&metadata.Files); err != nil {
		return nil, err
	}

	var files []model.Obj
	dirPath := dir.Path()
	if dirPath == "/" {
		dirPath = ""
	} else {
		// Strip leading slash for prefix matching
		dirPath = strings.TrimPrefix(dirPath, "/") + "/"
	}

	// Used to track directories we've already added in this listing
	addedDirs := make(map[string]bool)

	for _, f := range metadata.Files {
		// Ignore hidden files or metadata files IA creates automatically
		if strings.HasPrefix(f.Name, ".") || f.Format == "Metadata" {
			continue
		}

		// Filter files by the requested directory
		if dirPath != "" && !strings.HasPrefix(f.Name, dirPath) {
			continue
		}

		// Calculate relative path within the requested directory
		relPath := strings.TrimPrefix(f.Name, dirPath)

		// Check if the current item is a file or a sub-directory
		if strings.Contains(relPath, "/") {
			// It's a directory
			dirName := strings.Split(relPath, "/")[0]
			if !addedDirs[dirName] {
				files = append(files, &model.Object{
					Name:     dirName,
					Size:     0,
					Modified: time.Now(), // Directories don't have mtime in IA flat structure
					IsFolder: true,
				})
				addedDirs[dirName] = true
			}
		} else {
			// It's a file
			size, _ := utils.ToInt64(f.Size)
			
			// Parse modification time (Unix timestamp as string)
			mtimeInt, _ := utils.ToInt64(f.Mtime)
			mtime := time.Unix(mtimeInt, 0)

			files = append(files, &model.Object{
				Name:     relPath,
				Size:     size,
				Modified: mtime,
				IsFolder: false,
			})
		}
	}

	return files, nil
}

func (d *InternetArchive) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// This is the core magic for 302 Redirects.
	// We construct the direct archive.org download URL.
	// Format: https://archive.org/download/<Identifier>/<Filename>
	
	// AList handles URL encoding internally, but IA requires spaces to be %20
	filePath := path.Join(args.ReqPath)
	
	// Ensure no leading slash in the file path appended to the IA URL
	filePath = strings.TrimPrefix(filePath, "/")

	url := fmt.Sprintf("https://archive.org/download/%s/%s", d.Identifier, filePath)

	return &model.Link{
		URL: url,
	}, nil
}
