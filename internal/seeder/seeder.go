package seeder

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const (
	DefaultPassword = "Password123!"
	StudentEmail    = "seed.student@jobportal.test"
	StudentTwoEmail = "seed.student2@jobportal.test"
	EmployerEmail   = "seed.employer@jobportal.test"
)

type Summary struct{ Users, StudentProfiles, RecruiterProfiles, Internships, Applications int }

func Run(db *gorm.DB, password string) (Summary, error) {
	var summary Summary
	if db == nil {
		return summary, errors.New("database is required")
	}
	if password == "" {
		password = DefaultPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return summary, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		users := map[string]models.User{
			"student":           {Email: StudentEmail, FullName: "Aarav Sharma", Role: models.RoleStudent},
			"student2":          {Email: StudentTwoEmail, FullName: "Maya Gurung", Role: models.RoleStudent},
			"employer":          {Email: EmployerEmail, FullName: "Nisha Adhikari", Role: models.RoleEmployer},
			"employer_pending":  {Email: "seed.employer.pending@jobportal.test", FullName: "Suman Karki", Role: models.RoleEmployer},
			"employer_rejected": {Email: "seed.employer.rejected@jobportal.test", FullName: "Ritika Thapa", Role: models.RoleEmployer},
		}
		for key, user := range users {
			now := time.Now().UTC()
			user.EmailVerifiedAt = &now
			user.ID, user.PasswordHash = seedID("user", key), string(hash)
			var existing models.User
			err := tx.Unscoped().Where("email = ?", user.Email).First(&existing).Error
			if err == nil {
				user.ID, user.CreatedAt = existing.ID, existing.CreatedAt
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Unscoped().Save(&user).Error; err != nil {
				return err
			}
			users[key] = user
		}
		summary.Users = len(users)
		students := map[string]models.StudentProfile{}
		for _, item := range []struct{ key, name, college string }{{"student", "aarav", "Amrit Science Campus"}, {"student2", "maya", "Prithvi Narayan Campus"}} {
			profile := models.StudentProfile{ID: seedID("student-profile", item.name), UserID: users[item.key].ID, FullName: users[item.key].FullName, Phone: "+977-9800000001", Location: "Kathmandu, Nepal", CollegeName: item.college, Degree: "BCA", CurrentSemester: "4th Semester", GraduationYear: time.Now().Year() + 2}
			if err := saveStudentProfile(tx, &profile); err != nil {
				return err
			}
			students[item.key] = profile
		}
		summary.StudentProfiles = len(students)
		for _, item := range []struct{ key, name, company string }{
			{"employer", "techkarkhana", "TechKarkhana Nepal"},
			{"employer_pending", "himalayan-analytics", "Himalayan Analytics"},
			{"employer_rejected", "brightpath", "BrightPath Media"},
		} {
			profile := models.RecruiterProfile{ID: seedID("recruiter-profile", item.name), UserID: users[item.key].ID, OrganizationName: item.company, OrganizationAddress: "Kathmandu, Nepal"}
			if err := saveRecruiterProfile(tx, &profile); err != nil {
				return err
			}
			summary.RecruiterProfiles++
		}
		internships, err := seedInternships(tx, users)
		if err != nil {
			return err
		}
		summary.Internships = len(internships)
		for _, item := range []struct {
			key, student, internship string
			status                   models.InternshipApplicationStatus
		}{
			{"aarav-frontend", "student", "frontend", models.ApplicationStatusSubmitted},
			{"aarav-backend", "student", "backend", models.ApplicationStatusSubmitted},
			{"aarav-data", "student", "data-closed", models.ApplicationStatusRejected},
			{"maya-frontend", "student2", "frontend", models.ApplicationStatusSubmitted},
			{"maya-backend", "student2", "backend", models.ApplicationStatusAccepted},
			{"maya-design", "student2", "product-design", models.ApplicationStatusWithdrawn},
		} {
			app := models.InternshipApplication{ID: seedID("application", item.key), StudentID: students[item.student].ID, InternshipID: internships[item.internship].ID, Status: item.status, AppliedAt: time.Now().AddDate(0, 0, -2)}
			if err := saveApplication(tx, &app); err != nil {
				return err
			}
			summary.Applications++
		}
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("seed development data: %w", err)
	}
	return summary, nil
}

func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]models.Internship, error) {
	now := time.Now().UTC()
	future := func(days int) *time.Time { value := now.AddDate(0, 0, days); return &value }
	past := func(days int) *time.Time { value := now.AddDate(0, 0, -days); return &value }
	employerID := users["employer"].ID

	items := []struct {
		key        string
		internship models.Internship
	}{
		{key: "frontend", internship: models.Internship{ID: seedID("internship", "frontend"), IssuedBy: employerID, Title: "Frontend Engineering Intern", Description: "Build accessible, responsive product interfaces with React and TypeScript alongside an experienced engineering team.", Location: "Lalitpur, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "JavaScript, React, HTML, CSS", PreferredSkills: "TypeScript, Next.js, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 18000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(30), ApplicationDeadline: future(21), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Implement product features, review UI quality, and collaborate with design and backend teams.", Benefits: "Mentorship, lunch allowance, flexible hybrid schedule", Status: enums.InternshipStatusPublished}},
		{key: "backend", internship: models.Internship{ID: seedID("internship", "backend"), IssuedBy: employerID, Title: "Backend Go Intern", Description: "Help design and implement secure APIs and background services for a growing business software platform.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 4, DurationUnit: "months", WorkingHours: "9:30 AM - 5:30 PM", RequiredSkills: "Go, REST APIs, SQL", PreferredSkills: "PostgreSQL, Docker, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 22000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(35), ApplicationDeadline: future(28), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Develop APIs, write tests, investigate defects, and contribute to technical documentation.", Benefits: "Engineering mentorship, device allowance, potential full-time offer", Status: enums.InternshipStatusPublished}},
		{key: "product-design", internship: models.Internship{ID: seedID("internship", "product-design"), IssuedBy: employerID, Title: "Product Design Intern", Description: "Research user needs and turn findings into clear flows, prototypes, and polished interface specifications.", Location: "Remote", WorkMode: "remote", InternshipType: "paid", Duration: 12, DurationUnit: "weeks", WorkingHours: "Flexible, 25 hours per week", RequiredSkills: "Figma, Wireframing, User Research", PreferredSkills: "Design systems, Prototyping, Accessibility", RequiredEducation: "Open to students from any relevant program", EligiblePrograms: "BIT, BCA, BIM, Design", EligibleSemester: "4th semester and above", StipendAmount: 16000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(25), ApplicationDeadline: future(18), ApplicationEmail: "design@techkarkhana.example", Responsibilities: "Create prototypes, support interviews, document design decisions, and review implementation.", Benefits: "Design mentorship, flexible hours, remote work", Status: enums.InternshipStatusPublished}},
		{key: "data-closed", internship: models.Internship{ID: seedID("internship", "data-closed"), IssuedBy: employerID, Title: "Data Analytics Intern", Description: "Prepare operational datasets and build concise dashboards that help teams understand product performance.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "SQL, Spreadsheets, Data Visualization", PreferredSkills: "Python, Power BI", StipendAmount: 15000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: past(20), ApplicationDeadline: past(30), Responsibilities: "Clean data, validate reports, and present weekly insights.", Benefits: "Mentorship and completion certificate", Status: enums.InternshipStatusClosed}},
		{key: "qa-private", internship: models.Internship{ID: seedID("internship", "qa-private"), IssuedBy: users["employer_pending"].ID, Title: "QA Automation Intern", Description: "Draft internship being prepared before publication to students.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", RequiredSkills: "Testing fundamentals, JavaScript", PreferredSkills: "Playwright, Cypress", StipendAmount: 17000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(40), ApplicationDeadline: future(32), Responsibilities: "Create test cases and automate high-value product workflows.", Benefits: "QA mentorship", Status: enums.InternshipStatusPrivate}},
		{key: "marketing-private", internship: models.Internship{ID: seedID("internship", "marketing-private"), IssuedBy: users["employer_rejected"].ID, Title: "Digital Marketing Intern", Description: "Private internship saved as a draft for the employer to finish later.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 8, DurationUnit: "weeks", RequiredSkills: "Content writing, Social media", PreferredSkills: "SEO, Analytics", StipendAmount: 12000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(30), ApplicationDeadline: future(20), Responsibilities: "Support campaign planning, publishing, and reporting.", Benefits: "Flexible schedule", Status: enums.InternshipStatusPrivate}},
	}

	result := make(map[string]models.Internship, len(items))
	for _, item := range items {
		internship := item.internship
		if err := upsert(tx, &internship, "id"); err != nil {
			return nil, fmt.Errorf("upsert internship %s: %w", item.key, err)
		}
		result[item.key] = internship
	}

	return result, nil
}
func upsert(tx *gorm.DB, value interface{}, conflictColumn string) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: conflictColumn}},
		UpdateAll: true,
	}).Create(value).Error
}

func saveStudentProfile(tx *gorm.DB, profile *models.StudentProfile) error {
	var existing models.StudentProfile
	lookup := tx.Unscoped().Where("user_id = ?", profile.UserID).Limit(1).Find(&existing)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected == 0 {
		return tx.Create(profile).Error
	}
	profile.ID = existing.ID
	profile.CreatedAt = existing.CreatedAt
	return tx.Unscoped().Save(profile).Error
}

func saveRecruiterProfile(tx *gorm.DB, profile *models.RecruiterProfile) error {
	var existing models.RecruiterProfile
	lookup := tx.Where("user_id = ?", profile.UserID).Limit(1).Find(&existing)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected == 0 {
		return tx.Create(profile).Error
	}
	profile.ID = existing.ID
	profile.CreatedAt = existing.CreatedAt
	return tx.Save(profile).Error
}

func saveApplication(tx *gorm.DB, application *models.InternshipApplication) error {
	var existing models.InternshipApplication
	lookup := tx.Where(
		"id = ? OR (student_id = ? AND internship_id = ?)",
		application.ID,
		application.StudentID,
		application.InternshipID,
	).Limit(1).Find(&existing)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected == 0 {
		return tx.Create(application).Error
	}
	application.ID = existing.ID
	application.CreatedAt = existing.CreatedAt
	return tx.Save(application).Error
}

func seedID(kind, name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("student-job-portal/seed/"+kind+"/"+name))
}
