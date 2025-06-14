package _123_open

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/OpenListTeam/OpenList/drivers/base"
	"github.com/OpenListTeam/OpenList/internal/driver"
	"github.com/OpenListTeam/OpenList/internal/errs"
	"github.com/OpenListTeam/OpenList/internal/model"
	"github.com/OpenListTeam/OpenList/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

type Open123 struct {
	model.Storage
	Addition

	DriveId string

	limitList func(ctx context.Context, data base.Json) (*Files, error)
	limitLink func(ctx context.Context, file model.Obj) (*model.Link, error)
	ref       *Open123
}

func (d *Open123) Config() driver.Config {
	return config
}

func (d *Open123) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Open123) InitReference(storage driver.Driver) error {
	refStorage, ok := storage.(*Open123)
	if ok {
		d.ref = refStorage
		return nil
	}
	return errs.NotSupport
}

func (d *Open123) Drop(ctx context.Context) error {
	d.ref = nil
	return nil
}

// GetRoot implements the driver.GetRooter interface to properly set up the root object
func (d *Open123) GetRoot(ctx context.Context) (model.Obj, error) {
	return &model.Object{
		ID:       d.RootFolderID,
		Path:     "/",
		Name:     "root",
		Size:     0,
		Modified: d.Modified,
		IsFolder: true,
	}, nil
}

func (d *Open123) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if d.limitList == nil {
		return nil, fmt.Errorf("driver not init")
	}
	files, err := d.getFiles(ctx, dir.GetID())
	if err != nil {
		return nil, err
	}

	objs, err := utils.SliceConvert(files, func(src File) (model.Obj, error) {
		obj := fileToObj(src)
		// Set the correct path for the object
		if dir.GetPath() != "" {
			obj.Path = filepath.Join(dir.GetPath(), obj.GetName())
		}
		return obj, nil
	})

	return objs, err
}

func (d *Open123) link(ctx context.Context, file model.Obj) (*model.Link, error) {
	res, err := d.request("/api/v1/file/download_info", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":   d.DriveId,
			"file_id":    file.GetID(),
			"expire_sec": 14400,
		})
	})
	if err != nil {
		return nil, err
	}
	url := utils.Json.Get(res, "url").ToString()
	if url == "" {
		return nil, errors.New("get download url failed: " + string(res))
	}
	exp := time.Minute
	return &model.Link{
		URL:        url,
		Expiration: &exp,
	}, nil
}

func (d *Open123) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if d.limitLink == nil {
		return nil, fmt.Errorf("driver not init")
	}
	return d.limitLink(ctx, file)
}

func (d *Open123) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	nowTime, _ := getNowTime()
	newDir := File{CreatedAt: nowTime, UpdatedAt: nowTime}
	_, err := d.request("/upload/v1/file/mkdir", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"parent_id":       parentDir.GetID(),
			"name":            dirName,
			"type":            "folder",
			"check_name_mode": "refuse",
		}).SetResult(&newDir)
	})
	if err != nil {
		return nil, err
	}
	obj := fileToObj(newDir)

	// Set the correct Path for the returned directory object
	if parentDir.GetPath() != "" {
		obj.Path = filepath.Join(parentDir.GetPath(), dirName)
	} else {
		obj.Path = "/" + dirName
	}

	return obj, nil
}

func (d *Open123) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	var resp MoveOrCopyResp
	_, err := d.request("/api/v1/file/move", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"fileIDs":        srcObj.GetID(),
			"toParentFileID": dstDir.GetID(),
			//"check_name_mode":   "ignore", // optional:ignore,auto_rename,refuse
		}).SetResult(&resp)
	})
	if err != nil {
		return nil, err
	}

	if srcObj, ok := srcObj.(*model.ObjThumb); ok {
		srcObj.ID = resp.FileID
		srcObj.Modified = time.Now()
		srcObj.Path = filepath.Join(dstDir.GetPath(), srcObj.GetName())

		// Check for duplicate files in the destination directory
		if err := d.removeDuplicateFiles(ctx, dstDir.GetPath(), srcObj.GetName(), srcObj.GetID()); err != nil {
			// Only log a warning instead of returning an error since the move operation has already completed successfully
			log.Warnf("Failed to remove duplicate files after move: %v", err)
		}
		return srcObj, nil
	}
	return nil, nil
}

func (d *Open123) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}

	_, err := d.request("/api/v1/file/name", http.MethodPut, func(req *resty.Request) {
		req.SetBody(base.Json{
			"fileId":   srcObj.GetID(),
			"fileName": newName,
		}).SetResult(&apiResp)
	})
	if err != nil {
		return nil, err
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("rename failed: code=%d, message=%s", apiResp.Code, apiResp.Message)
	}

	parentPath := filepath.Dir(srcObj.GetPath())
	if err := d.removeDuplicateFiles(ctx, parentPath, newName, srcObj.GetID()); err != nil {
		log.Warnf("remove duplicate after rename failed: %v", err)
	}

	srcObj.SetName(newName)
	if parentPath != "" && parentPath != "." {
		srcObj.SetPath(filepath.Join(parentPath, newName))
	} else {
		srcObj.SetPath("/" + newName)
	}

	return srcObj, nil
}

func (d *Open123) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// No API was provided.
}

func (d *Open123) Remove(ctx context.Context, obj model.Obj) error {
	uri := "/api/v1/file/trash"

	_, err := d.request(uri, http.MethodPost, func(req *resty.Request) {
		req.SetHeader("Platform", "open_platform")
		req.SetBody(base.Json{
			"fileIDs": []interface{}{obj.GetID()},
		})
	})

	return err
}

func (d *Open123) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	obj, err := d.upload(ctx, dstDir, stream, up)

	// Set the correct Path for the returned file object
	if obj != nil && obj.GetPath() == "" {
		if dstDir.GetPath() != "" {
			if objWithPath, ok := obj.(model.SetPath); ok {
				objWithPath.SetPath(filepath.Join(dstDir.GetPath(), obj.GetName()))
			}
		}
	}

	return obj, err
}

func (d *Open123) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
}

// var _ driver.Driver = (*Open123)(nil)
var _ driver.MkdirResult = (*Open123)(nil)
var _ driver.MoveResult = (*Open123)(nil)
var _ driver.RenameResult = (*Open123)(nil)
var _ driver.PutResult = (*Open123)(nil)
var _ driver.GetRooter = (*Open123)(nil)
