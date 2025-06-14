package _123

import (
	"context"
	"fmt"

	"github.com/OpenListTeam/OpenList/internal/conf"
	"github.com/OpenListTeam/OpenList/internal/setting"

	_123 "github.com/OpenListTeam/OpenList/drivers/123_open"
	"github.com/OpenListTeam/OpenList/internal/errs"
	"github.com/OpenListTeam/OpenList/internal/model"
	"github.com/OpenListTeam/OpenList/internal/offline_download/tool"
	"github.com/OpenListTeam/OpenList/internal/op"
)

type Open123 struct {
	refreshTaskCache bool
}

func (p *Open123) Name() string {
	return "123 Pan"
}

func (p *Open123) Items() []model.SettingItem {
	return nil
}

func (p *Open123) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (p *Open123) Init() (string, error) {
	p.refreshTaskCache = false
	return "ok", nil
}

func (p *Open123) IsReady() bool {
	tempDir := setting.GetStr(conf.Open123TempDir)
	if tempDir == "" {
		return false
	}
	storage, _, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return false
	}
	if _, ok := storage.(*_123.Open123); !ok {
		return false
	}
	return true
}

func (p *Open123) AddURL(args *tool.AddUrlArgs) (string, error) {
	// 添加新任务刷新缓存
	p.refreshTaskCache = true
	storage, actualPath, err := op.GetStorageAndActualPath(args.TempDir)
	if err != nil {
		return "", err
	}
	driver123, ok := storage.(*_123.Open123)
	if !ok {
		return "", fmt.Errorf("unsupported storage driver for offline download, only 123 Cloud is supported")
	}

	ctx := context.Background()

	if err := op.MakeDir(ctx, storage, actualPath); err != nil {
		return "", err
	}

	parentDir, err := op.GetUnwrap(ctx, storage, actualPath)
	if err != nil {
		return "", err
	}

	hashs, err := driver123.OfflineDownload(ctx, []string{args.Url}, parentDir)
	if err != nil || len(hashs) < 1 {
		return "", fmt.Errorf("failed to add offline download task: %w", err)
	}

	return hashs[0], nil
}

func (p *Open123) Remove(task *tool.DownloadTask) error {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return err
	}
	driver123, ok := storage.(*_123.Open123)
	if !ok {
		return fmt.Errorf("unsupported storage driver for offline download, only 123 Cloud is supported")
	}

	ctx := context.Background()
	if err := driver123.DeleteOfflineTasks(ctx, []string{task.GID}, false); err != nil {
		return err
	}
	return nil
}

func (p *Open123) Status(task *tool.DownloadTask) (*tool.Status, error) {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return nil, err
	}
	driver123, ok := storage.(*_123.Open123)
	if !ok {
		return nil, fmt.Errorf("unsupported storage driver for offline download, only 123 Cloud is supported")
	}

	tasks, err := driver123.OfflineList(context.Background())
	if err != nil {
		return nil, err
	}

	s := &tool.Status{
		Progress:  0,
		NewGID:    "",
		Completed: false,
		Status:    "the task has been deleted",
		Err:       nil,
	}
	for _, t := range tasks {
		if t.InfoHash == task.GID {
			s.Progress = t.Percent
			s.Status = t.GetStatus()
			s.Completed = t.IsDone()
			s.TotalBytes = t.Size
			if t.IsFailed() {
				s.Err = fmt.Errorf(t.GetStatus())
			}
			return s, nil
		}
	}
	s.Err = fmt.Errorf("the task has been deleted")
	return nil, nil
}

var _ tool.Tool = (*Open123)(nil)

func init() {
	tool.Tools.Add(&Open123{})
}
