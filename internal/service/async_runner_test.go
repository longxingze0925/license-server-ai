package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"license-server/internal/adapter"
	"license-server/internal/model"
)

type retryCreateAdapter struct {
	attempts int
}

func (a *retryCreateAdapter) Provider() model.ProviderKind { return model.ProviderGemini }

func (a *retryCreateAdapter) Create(context.Context, *model.ProviderCredential, []byte, []byte) (*adapter.CreateResult, error) {
	a.attempts++
	if a.attempts == 1 {
		return nil, &url.Error{Op: "Post", URL: "https://example.test", Err: errors.New("i/o timeout")}
	}
	return &adapter.CreateResult{UpstreamTaskID: "task-1"}, nil
}

func (a *retryCreateAdapter) Poll(context.Context, *model.ProviderCredential, []byte, string) (*adapter.PollResult, error) {
	return nil, nil
}

func (a *retryCreateAdapter) BuildDownloadRequest(context.Context, *model.ProviderCredential, []byte, adapter.MediaDescriptor) (*http.Request, error) {
	return nil, nil
}

func TestAsyncRunnerTaskTimeoutAccessors(t *testing.T) {
	runner := &AsyncRunnerService{}

	runner.SetTaskTimeout(45 * time.Minute)

	if got := runner.TaskTimeout(); got != 45*time.Minute {
		t.Fatalf("task timeout = %s, want 45m", got)
	}
}

func TestCreateWithNetworkRetryRetriesTransientNetworkError(t *testing.T) {
	runner := &AsyncRunnerService{}
	a := &retryCreateAdapter{}

	res, err := runner.createWithNetworkRetry(context.Background(), a, &model.ProviderCredential{}, []byte("key"), []byte(`{}`))
	if err != nil {
		t.Fatalf("createWithNetworkRetry failed: %v", err)
	}
	if res == nil || res.UpstreamTaskID != "task-1" {
		t.Fatalf("result = %#v", res)
	}
	if a.attempts != 2 {
		t.Fatalf("attempts = %d, want 2", a.attempts)
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

func TestResolveNextAsyncPollDelayUsesFastPollWhenProgressChanges(t *testing.T) {
	got := resolveNextAsyncPollDelay(0.2, 0.3)
	if got != asyncProgressPollDelay {
		t.Fatalf("poll delay = %s, want %s", got, asyncProgressPollDelay)
	}
}

func TestResolveNextAsyncPollDelayCapsIdlePollAtFiveSeconds(t *testing.T) {
	got := resolveNextAsyncPollDelay(0.3, 0.3)
	if got != asyncIdlePollDelay {
		t.Fatalf("poll delay = %s, want %s", got, asyncIdlePollDelay)
	}
	if got > 5*time.Second {
		t.Fatalf("poll delay should not exceed 5s, got %s", got)
	}
}
