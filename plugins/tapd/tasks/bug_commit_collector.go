package tasks

import (
	"encoding/json"
	"fmt"
	"github.com/merico-dev/lake/plugins/core"
	"github.com/merico-dev/lake/plugins/helper"
	"github.com/merico-dev/lake/plugins/tapd/models"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

const RAW_BUG_COMMIT_TABLE = "tapd_api_bug_commits"

var _ core.SubTaskEntryPoint = CollectBugCommits

type SimpleBug struct {
	Id uint64
}

func CollectBugCommits(taskCtx core.SubTaskContext) error {
	data := taskCtx.GetData().(*TapdTaskData)
	db := taskCtx.GetDb()
	logger := taskCtx.GetLogger()
	logger.Info("collect issueCommits")

	since := data.Since
	incremental := false
	if since == nil {
		// user didn't specify a time range to sync, try load from database
		var latestUpdated models.TapdBugCommit
		err := db.Where("connection_id = ?", data.Connection.ID).Order("created DESC").Limit(1).Find(&latestUpdated).Error
		if err != nil {
			return fmt.Errorf("failed to get latest tapd changelog record: %w", err)
		}
		if latestUpdated.ID > 0 {
			since = (*time.Time)(latestUpdated.Created)
			incremental = true
		}
	}

	tx := db.Model(&models.TapdBug{})
	if since != nil {
		tx = tx.Where("modified > ? and connection_id = ? and workspace_id = ?", since, data.Options.ConnectionId, data.Options.WorkspaceID)
	}
	cursor, err := tx.Rows()
	if err != nil {
		return err
	}
	iterator, err := helper.NewCursorIterator(db, cursor, reflect.TypeOf(SimpleBug{}))
	if err != nil {
		return err
	}
	collector, err := helper.NewApiCollector(helper.ApiCollectorArgs{
		RawDataSubTaskArgs: helper.RawDataSubTaskArgs{
			Ctx: taskCtx,
			Params: TapdApiParams{
				ConnectionId: data.Connection.ID,
				//CompanyId: data.Options.CompanyId,
				WorkspaceID: data.Options.WorkspaceID,
			},
			Table: RAW_BUG_COMMIT_TABLE,
		},
		Incremental: incremental,
		ApiClient:   data.ApiClient,
		//PageSize:    100,
		Input:       iterator,
		UrlTemplate: "code_commit_infos",
		Query: func(reqData *helper.RequestData) (url.Values, error) {
			input := reqData.Input.(*SimpleBug)
			query := url.Values{}
			query.Set("workspace_id", fmt.Sprintf("%v", data.Options.WorkspaceID))
			query.Set("type", "bug")
			query.Set("object_id", fmt.Sprintf("%v", input.Id))
			query.Set("order", "created asc")
			return query, nil
		},
		ResponseParser: func(res *http.Response) ([]json.RawMessage, error) {
			var data struct {
				Stories []json.RawMessage `json:"data"`
			}
			err := helper.UnmarshalResponse(res, &data)
			return data.Stories, err
		},
	})
	if err != nil {
		logger.Error("collect issueCommit error:", err)
		return err
	}
	return collector.Execute()
}

var CollectBugCommitMeta = core.SubTaskMeta{
	Name:        "collectBugCommits",
	EntryPoint:  CollectBugCommits,
	Required:    true,
	Description: "collect Tapd issueCommits",
}