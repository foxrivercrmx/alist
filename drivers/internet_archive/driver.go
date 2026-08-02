package internet_archive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
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
		OnlyProxy:   false, // Mengizinkan 302 redirect
		NoCache:     false,
		NoUpload:    true,  // Read-only
		NeedMs:      false,
		DefaultRoot: "/",
	}
}

func (d *InternetArchive) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *InternetArchive) Init(ctx context.Context) error {
	if d.Identifier == "" {
		return fmt.Errorf("identifier cannot be empty")
	}
	return nil
}

func (d *InternetArchive) Drop(ctx context.Context) error {
	return nil
}

// Struktur metadata Internet Archive
type IAMetadata struct {
	Files []struct {
		Name   string `json:"name"`
		Size   string `json:"size"`
		Mtime  string `json:"mtime"`
		Format string `json:"format"`
	} `json:"files"`
}

// List fungsi untuk mengambil dan menstruktur file dari API Internet Archive
func (d *InternetArchive) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
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
	dirPath := dir.GetPath()
	
	// Normalisasi dirPath untuk logika prefix IA
	if dirPath == "/" || dirPath == "" {
		dirPath = ""
	} else {
		dirPath = strings.TrimPrefix(dirPath, "/") + "/"
	}

	addedDirs := make(map[string]bool)

	for _, f := range metadata.Files {
		// Abaikan file metadata dan hidden files
		if strings.HasPrefix(f.Name, ".") || f.Format == "Metadata" {
			continue
		}

		if dirPath != "" && !strings.HasPrefix(f.Name, dirPath) {
			continue
		}

		relPath := strings.TrimPrefix(f.Name, dirPath)

		if strings.Contains(relPath, "/") {
			// Jika ada sisa slash, berarti ini adalah direktori/folder
			dirName := strings.Split(relPath, "/")[0]
			if !addedDirs[dirName] {
				files = append(files, &model.Object{
					Name:     dirName,
					Size:     0,
					Modified: time.Now(),
					IsFolder: true,
				})
				addedDirs[dirName] = true
			}
		} else {
			// Ini adalah file
			// Parse string size ke int64
			size, err := strconv.ParseInt(f.Size, 10, 64)
			if err != nil {
				size = 0
			}

			// Parse string mtime ke int64 lalu ke time.Time
			mtimeInt, err := strconv.ParseInt(f.Mtime, 10, 64)
			var mtime time.Time
			if err == nil {
				mtime = time.Unix(mtimeInt, 0)
			} else {
				mtime = time.Now()
			}

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

// Link fungsi vital untuk melakukan 302 Redirect
func (d *InternetArchive) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// Menggunakan GetPath() dari object file dan membuang slash di awal agar URL IA valid
	filePath := file.GetPath()
	filePath = strings.TrimPrefix(filePath, "/")
	
	// Karena IA memerlukan URL Path, kita gunakan path.Join untuk memastikan format aman
	// IA tidak perlu encoding karakter aneh-aneh selama kita menyusun path-nya dengan benar.
	cleanPath := path.Clean(filePath)

	url := fmt.Sprintf("https://archive.org/download/%s/%s", d.Identifier, cleanPath)

	return &model.Link{
		URL: url,
	}, nil
}