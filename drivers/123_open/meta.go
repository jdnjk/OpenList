package _123_open

import (
	"github.com/OpenListTeam/OpenList/internal/driver"
	"github.com/OpenListTeam/OpenList/internal/op"
)

type Addition struct {
	DriveType string `json:"drive_type" type:"select" options:"default,resource,backup" default:"resource"`
	driver.RootID
	//RefreshToken       string `json:"refresh_token" required:"true"`
	OrderBy        string `json:"order_by" type:"select" options:"name,size,updated_at,created_at"`
	OrderDirection string `json:"order_direction" type:"select" options:"ASC,DESC"`
	//OauthTokenURL      string `json:"oauth_token_url" default:"https://example.com/alist/ali_open/token"` // TODO: Replace this with a community hosted api endpoint
	ClientID     string `json:"client_id" required:"true""`
	ClientSecret string `json:"client_secret" required:"true"`
	//RemoveWay    string `json:"remove_way" required:"true" type:"select" options:"trash,delete"`
}

var config = driver.Config{
	Name:              "123 Open",
	LocalSort:         false,
	OnlyLocal:         false,
	OnlyProxy:         false,
	NoCache:           false,
	NoUpload:          false,
	NeedMs:            false,
	DefaultRoot:       "root",
	NoOverwriteUpload: true,
}
var API_URL = "https://open-api.123pan.com"

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Open123{}
	})
}
