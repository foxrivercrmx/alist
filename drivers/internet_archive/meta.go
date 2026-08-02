package internet_archive

import (
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
)

type Addition struct {
	driver.RootPath
	Identifier string `json:"identifier" required:"true" help:"Internet Archive Item Identifier (e.g., from archive.org/details/IDENTIFIER)"`
}

var config = driver.Config{
	Name:              "InternetArchive",
	LocalSort:         true,
	OnlyLocal:         false,
	OnlyProxy:         false, // Mengizinkan 302 Redirect
	NoCache:           false,
	NoUpload:          true,  // Internet Archive (tanpa S3 auth) bersifat Read-Only
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