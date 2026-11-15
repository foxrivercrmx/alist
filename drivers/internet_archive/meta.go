package internet_archive

import (
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
)

type Addition struct {
	driver.RootPath
	Identifier string `json:"identifier" required:"true" help:"Internet Archive Item Identifier (e.g., from archive.org/details/IDENTIFIER)"`
	EnableZipBrowsing bool `json:"enable_zip_browsing" default:"true" help:"Enable browsing contents of ZIP files"`
}

var config = driver.Config{
	Name:              "InternetArchive",
	LocalSort:         true,
	OnlyLocal:         false,
	OnlyProxy:         false, // Allows 302 Redirect
	NoCache:           false,
	NoUpload:          true,  // Internet Archive (without S3 auth) is Read-Only
	NeedMs:            false,
	DefaultRoot:       "/",
	CheckStatus:       false,
	Alert:             "",
	NoOverwriteUpload: false,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &InternetArchive{}
	})
}