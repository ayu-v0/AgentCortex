package conversation

import (
	"log"
	"time"
)

type CleanupSchedule struct {
	Enabled       bool
	RetentionDays int
	TimeOfDay     string
	Timezone      string
}

type CleanupScheduler struct {
	service *Service
	cfg     CleanupSchedule
	loc     *time.Location
	stop    chan struct{}
	done    chan struct{}
	logger  func(string, ...any)
}

func NewCleanupScheduler(service *Service, cfg CleanupSchedule) (*CleanupScheduler, error) {
	if service == nil {
		return nil, ErrNilStore
	}
	if cfg.RetentionDays <= 0 {
		return nil, ErrInvalidConfig
	}
	if stringsTrim(cfg.TimeOfDay) == "" {
		return nil, ErrInvalidSchedule
	}
	loc, err := time.LoadLocation(stringsTrim(cfg.Timezone))
	if err != nil {
		return nil, ErrInvalidSchedule
	}
	if _, _, err := parseTimeOfDay(cfg.TimeOfDay); err != nil {
		return nil, err
	}
	return &CleanupScheduler{
		service: service,
		cfg:     cfg,
		loc:     loc,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		logger:  log.Printf,
	}, nil
}

func (s *CleanupScheduler) Start() {
	go s.loop()
}

func (s *CleanupScheduler) Stop() {
	close(s.stop)
	<-s.done
}

func (s *CleanupScheduler) SetLoggerForTest(logger func(string, ...any)) {
	if logger == nil {
		s.logger = log.Printf
		return
	}
	s.logger = logger
}

func (s *CleanupScheduler) loop() {
	defer close(s.done)
	for {
		next, err := NextCleanupTime(time.Now(), s.cfg.TimeOfDay, s.loc)
		if err != nil {
			s.logger("conversation cleanup schedule error: %v", err)
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-timer.C:
			s.runCleanup()
		case <-s.stop:
			if !timer.Stop() {
				<-timer.C
			}
			return
		}
	}
}

func (s *CleanupScheduler) runCleanup() {
	cutoff := time.Now().UTC().AddDate(0, 0, -s.cfg.RetentionDays)
	deleted, err := s.service.CleanupExpired(cutoff)
	if err != nil {
		s.logger("conversation cleanup failed: %v", err)
		return
	}
	s.logger("conversation cleanup completed deleted=%d", deleted)
}

func NextCleanupTime(now time.Time, timeOfDay string, loc *time.Location) (time.Time, error) {
	hour, minute, err := parseTimeOfDay(timeOfDay)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		return time.Time{}, ErrInvalidSchedule
	}
	localNow := now.In(loc)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func parseTimeOfDay(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", stringsTrim(value))
	if err != nil {
		return 0, 0, ErrInvalidSchedule
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func stringsTrim(value string) string {
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\t' || value[0] == '\n' || value[0] == '\r') {
		value = value[1:]
	}
	for len(value) > 0 {
		last := value[len(value)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		value = value[:len(value)-1]
	}
	return value
}
