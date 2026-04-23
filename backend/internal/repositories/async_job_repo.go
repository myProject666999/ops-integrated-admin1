package repositories

import (
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/utils"
	"sort"
	"sync"
	"time"
)

type AsyncJobRepository struct {
	jobs   map[string]*models.AsyncOperateJob
	jobMu  sync.Mutex
}

func NewAsyncJobRepository() *AsyncJobRepository {
	return &AsyncJobRepository{
		jobs: make(map[string]*models.AsyncOperateJob),
	}
}

func (r *AsyncJobRepository) CreateJob(user *models.AuthedUser, projectType, action string) (*models.AsyncOperateJob, error) {
	id, err := utils.RandomToken(18)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	job := &models.AsyncOperateJob{
		ID:          id,
		UserID:      user.ID,
		Username:    user.Username,
		ProjectType: projectType,
		Action:      action,
		Status:      models.AsyncJobStatusRunning,
		OK:          false,
		Done:        false,
		Progress:    1,
		Processed:   0,
		Total:       0,
		LogLines:    []string{"开始执行..."},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	r.jobMu.Lock()
	defer r.jobMu.Unlock()
	r.purgeJobsLocked(now)
	r.jobs[job.ID] = job
	return job, nil
}

func (r *AsyncJobRepository) purgeJobsLocked(now time.Time) {
	if r.jobs == nil {
		return
	}
	const keepDuration = 30 * time.Minute
	for id, job := range r.jobs {
		if job == nil {
			delete(r.jobs, id)
			continue
		}
		if job.Done && now.Sub(job.UpdatedAt) > keepDuration {
			delete(r.jobs, id)
		}
	}

	if len(r.jobs) <= 400 {
		return
	}
	ids := make([]string, 0, len(r.jobs))
	for id := range r.jobs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ai := r.jobs[ids[i]]
		aj := r.jobs[ids[j]]
		if ai == nil || aj == nil {
			return ids[i] < ids[j]
		}
		return ai.UpdatedAt.Before(aj.UpdatedAt)
	})
	for _, id := range ids {
		if len(r.jobs) <= 300 {
			break
		}
		delete(r.jobs, id)
	}
}

func (r *AsyncJobRepository) UpdateJob(jobID string, fn func(*models.AsyncOperateJob)) {
	r.jobMu.Lock()
	defer r.jobMu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok {
		return
	}
	fn(job)
	job.UpdatedAt = time.Now()
}

func (r *AsyncJobRepository) GetJobView(jobID string, userID int64) (*models.AsyncOperateJobView, bool) {
	r.jobMu.Lock()
	defer r.jobMu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.UserID != userID {
		return nil, false
	}
	view := &models.AsyncOperateJobView{
		JobID:       job.ID,
		ProjectType: job.ProjectType,
		Action:      job.Action,
		Status:      job.Status,
		OK:          job.OK,
		Done:        job.Done,
		Message:     job.Message,
		Error:       job.Error,
		Progress:    job.Progress,
		Processed:   job.Processed,
		Total:       job.Total,
		LogLines:    append([]string(nil), job.LogLines...),
		ResultText:  job.ResultText,
		ResultItems: append([]interface{}(nil), job.ResultItems...),
		CreatedAt:   job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   job.UpdatedAt.Format(time.RFC3339),
	}
	return view, true
}

func (r *AsyncJobRepository) CalcJobProgress(processed, total, logCount int, done bool) int {
	if done {
		return 100
	}
	if total > 0 && processed >= 0 {
		pct := int(float64(processed) / float64(total) * 100)
		if pct < 1 {
			pct = 1
		}
		if pct > 99 {
			pct = 99
		}
		return pct
	}
	if logCount <= 0 {
		return 1
	}
	pct := 10 + logCount*5
	if pct > 95 {
		pct = 95
	}
	return pct
}
