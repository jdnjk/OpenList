package _123_open

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/OpenListTeam/OpenList/drivers/base"
	"github.com/OpenListTeam/OpenList/internal/driver"
	"github.com/OpenListTeam/OpenList/internal/model"
	"github.com/OpenListTeam/OpenList/pkg/http_range"
	"github.com/OpenListTeam/OpenList/pkg/utils"
	"github.com/avast/retry-go"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

type CreateResp struct {
	PreuploadID string `json:"preuploadID"`
	SliceSize   int64  `json:"sliceSize"`
	FileID      string `json:"fileID"`
	Reuse       bool   `json:"reuse"`
}

type PartInfo struct {
	PartNumber int    `json:"partNumber"`
	Etag       string `json:"etag"`
}

type PartListResp struct {
	Parts []PartInfo `json:"parts"`
}

func (d *Open123) upload(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	token, err := d.getAccessToken()
	if err != nil {
		return nil, err
	}

	createData := base.Json{
		"parentFileID": dstDir.GetID(),
		"filename":     stream.GetName(),
		"etag":         stream.GetHash().GetHash(utils.MD5),
		"size":         stream.GetSize(),
		"duplicate":    1,
	}

	var createResp CreateResp
	_, err = d.request("/upload/v1/file/create", http.MethodPost, func(req *resty.Request) {
		req.SetHeader("Platform", "open_platform")
		req.SetBody(createData).SetResult(&createResp)
	}, token)
	if err != nil {
		return nil, err
	}

	if createResp.Reuse {
		return d.getFileInfo(createResp.FileID, token)
	}

	uploadId := createResp.PreuploadID
	sliceSize := createResp.SliceSize

	var partResp PartListResp
	_, err = d.request("/upload/v1/file/list_upload_parts", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"preuploadID": uploadId,
		}).SetResult(&partResp)
	}, token)
	if err != nil {
		return nil, err
	}

	uploadedParts := map[int]PartInfo{}
	for _, part := range partResp.Parts {
		uploadedParts[part.PartNumber] = part
	}

	offset := int64(0)
	partNum := 1
	for offset < stream.GetSize() {
		if utils.IsCanceled(ctx) {
			return nil, ctx.Err()
		}

		length := sliceSize
		if remain := stream.GetSize() - offset; length > remain {
			length = remain
		}

		if _, exists := uploadedParts[partNum]; exists {
			log.Debugf("[123yunpan] Part %d already uploaded, skipping.", partNum)
			offset += length
			partNum++
			up(float64(offset) / float64(stream.GetSize()) * 100)
			continue
		}

		partUrl, err := d.getPartUploadUrl(uploadId, partNum, token)
		if err != nil {
			return nil, err
		}

		srd, err := stream.RangeRead(http_range.Range{Start: offset, Length: length})
		if err != nil {
			return nil, err
		}

		err = retry.Do(func() error {
			return d.uploadPart(ctx, srd, partUrl)
		}, retry.Attempts(3), retry.Delay(time.Second))
		if err != nil {
			return nil, err
		}

		offset += length
		partNum++
		up(float64(offset) / float64(stream.GetSize()) * 100)
	}

	_, err = d.request("/upload/v1/file/complete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{"preuploadID": uploadId})
	}, token)
	if err != nil {
		return nil, err
	}

	return d.getFileInfo(createResp.FileID, token)
}

func (d *Open123) uploadPart(ctx context.Context, r io.Reader, url string) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", url, r)
	if err != nil {
		return err
	}
	res, err := base.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("upload failed, status: %d", res.StatusCode)
	}
	return nil
}

func (d *Open123) getPartUploadUrl(uploadId string, partNum int, token string) (string, error) {
	var resp struct {
		URL string `json:"url"`
	}
	_, err := d.request("/upload/v1/file/get_upload_url", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"preuploadID": uploadId,
			"partNumber":  partNum,
		}).SetResult(&resp)
	}, token)
	if err != nil {
		return "", err
	}
	return resp.URL, nil
}
