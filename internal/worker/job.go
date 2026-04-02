package worker

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

// Result wraps validation output with internal execution error.
type Result struct {
	domain.ValidationResult
	Err error
}

// Job is the worker queue contract.
type Job interface {
	Execute(w *worker, ctx context.Context)
	Context() context.Context
	ResultChannel() chan<- Result
}

type ValidateJob struct {
	APIKey     string
	LicenseKey string
	Product    string
	Ctx        context.Context
	ResultCh   chan<- Result
}

func (j *ValidateJob) Execute(w *worker, ctx context.Context) {
	res, err := w.validation.Validate(ctx, j.APIKey, j.LicenseKey, j.Product)
	out := Result{Err: err}
	if res != nil {
		out.ValidationResult = *res
	}
	safeSendResult(j.ResultCh, out)
}

func (j *ValidateJob) Context() context.Context { return j.Ctx }

func (j *ValidateJob) ResultChannel() chan<- Result { return j.ResultCh }
