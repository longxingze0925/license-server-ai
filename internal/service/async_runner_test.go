package service

import (
	"testing"
	"time"

	"license-server/internal/model"
)

func TestAsyncRunnerTaskTimeoutAccessors(t *testing.T) {
	runner := &AsyncRunnerService{}

	runner.SetTaskTimeout(45 * time.Minute)

	if got := runner.TaskTimeout(); got != 45*time.Minute {
		t.Fatalf("task timeout = %s, want 45m", got)
	}
}

func TestExistingSavedFileIDsSkipsAlreadySavedMedia(t *testing.T) {
	existing := []model.GenerationFile{
		{BaseModel: model.BaseModel{ID: "file-1"}},
		{BaseModel: model.BaseModel{ID: "file-2"}},
	}

	fileIDs, resumeFrom := existingSavedFileIDs(existing, 3)

	if resumeFrom != 2 {
		t.Fatalf("resumeFrom = %d, want 2", resumeFrom)
	}
	if len(fileIDs) != 2 || fileIDs[0] != "file-1" || fileIDs[1] != "file-2" {
		t.Fatalf("fileIDs = %#v", fileIDs)
	}
}

func TestExistingSavedFileIDsClampsExtraRows(t *testing.T) {
	existing := []model.GenerationFile{
		{BaseModel: model.BaseModel{ID: "file-1"}},
		{BaseModel: model.BaseModel{ID: "file-2"}},
		{BaseModel: model.BaseModel{ID: "file-3"}},
	}

	fileIDs, resumeFrom := existingSavedFileIDs(existing, 2)

	if resumeFrom != 2 {
		t.Fatalf("resumeFrom = %d, want 2", resumeFrom)
	}
	if len(fileIDs) != 2 || fileIDs[0] != "file-1" || fileIDs[1] != "file-2" {
		t.Fatalf("fileIDs = %#v", fileIDs)
	}
}
