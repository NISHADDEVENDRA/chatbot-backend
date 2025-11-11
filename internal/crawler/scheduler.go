package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-co-op/gocron"
)

// Scheduler manages scheduled crawling jobs
type Scheduler struct {
	scheduler *gocron.Scheduler
	cancel    context.CancelFunc
	ctx       context.Context
}

// NewScheduler creates a new crawler scheduler
func NewScheduler() *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := gocron.NewScheduler(time.UTC)
	s.TagsUnique()

	return &Scheduler{
		scheduler: s,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.scheduler.StartAsync()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.scheduler.Stop()
	if s.cancel != nil {
		s.cancel()
	}
}

// ScheduleJob schedules a crawl job to run at specified intervals
func (s *Scheduler) ScheduleJob(
	tag string,
	cronExpr string,
	job func() error,
) error {
	_, err := s.scheduler.Cron(cronExpr).Tag(tag).Do(job)
	return err
}

// ScheduleInterval schedules a job to run at regular intervals
func (s *Scheduler) ScheduleInterval(
	tag string,
	duration time.Duration,
	job func() error,
) error {
	_, err := s.scheduler.Every(duration).Tag(tag).Do(job)
	return err
}

// RemoveJob removes a scheduled job by tag
func (s *Scheduler) RemoveJob(tag string) error {
	return s.scheduler.RemoveByTag(tag)
}

// GetJobs returns all scheduled jobs
func (s *Scheduler) GetJobs() []*gocron.Job {
	return s.scheduler.Jobs()
}

// Example: Schedule hourly crawl
func ExampleScheduleHourlyCrawl(callback func() error) (*Scheduler, error) {
	scheduler := NewScheduler()

	err := scheduler.ScheduleInterval("hourly-crawl", 1*time.Hour, func() error {
		fmt.Println("Running hourly crawl job...")
		return callback()
	})

	if err != nil {
		return nil, err
	}

	scheduler.Start()
	return scheduler, nil
}
