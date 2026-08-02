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

// Struktur metadata Internet Archive yang sudah disesuaikan dengan key "result"
type IAMetadata struct {
	Result []struct {
		Name   string `json:"name"`
		Size   string `json:"size"`
		Mtime  string `json:"mtime"`
		Format string `json:"format"`
	} `json:"result"`
}

func (d *InternetArchive) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
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
	// Decode langsung ke struct metadata, bukan ke slice-nya
	if err := json.NewDecoder(res.Body).Decode(&metadata); err != nil {
		return nil, err
	}

	var files []model.Obj
	dirPath := dir.GetPath()
	if dirPath == "/" || dirPath == "" {
		dirPath = ""
	} else {
		dirPath = strings.TrimPrefix(dirPath, "/") + "/"
	}

	addedDirs := make(map[string]bool)

	// Looping menggunakan metadata.Result
	for _, f := range metadata.Result {
		if strings.HasPrefix(f.Name, ".") || f.Format == "Metadata" {
			continue
		}
		if dirPath != "" && !strings.HasPrefix(f.Name, dirPath) {
			continue
		}

		relPath := strings.TrimPrefix(f.Name, dirPath)

		if strings.Contains(relPath, "/") {
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
			size, _ := strconv.ParseInt(f.Size, 10, 64)
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

func (d *InternetArchive) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	filePath := file.GetPath()
	filePath = strings.TrimPrefix(filePath, "/")
	cleanPath := path.Clean(filePath)

	// Format URL direct untuk 302 redirect
	url := fmt.Sprintf("https://archive.org/download/%s/%s", d.Identifier, cleanPath)

	return &model.Link{
		URL: url,
	}, nil
}

// Implementasi method lain yang diwajibkan oleh interface dengan return errs.NotImplement

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

// Memastikan struct InternetArchive memenuhi interface driver.Driver
var _ driver.Driver = (*InternetArchive)(nil)