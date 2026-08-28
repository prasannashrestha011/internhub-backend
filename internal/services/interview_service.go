package services

import (
	"time"

	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

type InterviewService struct {
	Repo *repositories.InterviewRepository
}

func NewInterviewService(repo *repositories.InterviewRepository) *InterviewService {
	return &InterviewService{Repo: repo}
}

func (s *InterviewService) ScheduleInterview(i *models.Interview) error {
	// basic validations could be added here (e.g., job exists)
	if i.ScheduledAt.IsZero() {
		i.ScheduledAt = time.Now().Add(24 * time.Hour)
	}
	if err := s.Repo.Create(i); err != nil {
		return err
	}
	return nil
}

func (s *InterviewService) GetInterview(id uuid.UUID) (*models.Interview, error) {
	return s.Repo.GetByID(id)
}

func (s *InterviewService) ListByJob(jobID uuid.UUID) ([]models.Interview, error) {
	return s.Repo.ListByJob(jobID)
}

func (s *InterviewService) ListByStudent(studentID uuid.UUID) ([]models.Interview, error) {
	return s.Repo.ListByStudent(studentID)
}

func (s *InterviewService) ListByEmployer(employerID uuid.UUID) ([]models.Interview, error) {
	return s.Repo.ListByEmployer(employerID)
}

func (s *InterviewService) UpdateStatus(id uuid.UUID, status string) (*models.Interview, error) {
	i, err := s.Repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	i.Status = status
	if err := s.Repo.Update(i); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *InterviewService) CancelInterview(id uuid.UUID) error {
	return s.Repo.DeleteByID(id)
}
