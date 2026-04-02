package worker

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
)

type worker struct {
	id         int
	queue      <-chan Job
	validation app.ValidationService
}

func newWorker(id int, queue <-chan Job, validation app.ValidationService) *worker {
	return &worker{id: id, queue: queue, validation: validation}
}

func (w *worker) run(ctx context.Context) {
	for job := range w.queue {
		w.safeProcess(ctx, job)
	}
}

func (w *worker) safeProcess(ctx context.Context, job Job) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("worker %d panic: %v\n%s\n", w.id, r, debug.Stack())
			safeSendResult(job.ResultChannel(), Result{
				ValidationResult: domain.ValidationResult{
					Valid: false,
					Error: "internal_error",
				},
			})
		}
	}()

	jobCtx := ctx
	if jCtx := job.Context(); jCtx != nil {
		jobCtx = jCtx
	}
	job.Execute(w, jobCtx)
}

func safeSendResult(ch chan<- Result, res Result) {
	if ch == nil {
		return
	}
	select {
	case ch <- res:
	default:
	}
}
