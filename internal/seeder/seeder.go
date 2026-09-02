package seeder

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DefaultPassword = "Password123!"
	AdminEmail      = "seed.admin@jobportal.test"
	StudentEmail    = "seed.student@jobportal.test"
	StudentTwoEmail = "seed.student2@jobportal.test"
	EmployerEmail   = "seed.employer@jobportal.test"
)

// Summary describes the records ensured by Run. Counts are the size of the
// development dataset, whether each record was inserted or updated.
type Summary struct {
	Users                     int
	StudentProfiles           int
	RecruiterProfiles         int
	OrganizationVerifications int
	Internships               int
	Applications              int
}

type seedUser struct {
	key      string
	email    string
	fullName string
	role     models.Role
}

// Run inserts or refreshes the development dataset in one transaction. Stable
// UUIDs and conflict-aware writes make repeated runs safe.
func Run(db *gorm.DB, password string) (Summary, error) {
	var summary Summary
	if db == nil {
		return summary, errors.New("database is required")
	}
	if password == "" {
		password = DefaultPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return summary, fmt.Errorf("hash seed password: %w", err)
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		users, err := seedUsers(tx, string(passwordHash))
		if err != nil {
			return err
		}
		summary.Users = len(users)

		students, err := seedStudentProfiles(tx, users)
		if err != nil {
			return err
		}
		summary.StudentProfiles = len(students)

		recruiters, err := seedRecruiterProfiles(tx, users)
		if err != nil {
			return err
		}
		summary.RecruiterProfiles = len(recruiters)

		if err := seedStudentDetails(tx, students); err != nil {
			return err
		}

		verificationCount, err := seedVerifications(tx, users, recruiters)
		if err != nil {
			return err
		}
		summary.OrganizationVerifications = verificationCount

		internships, err := seedInternships(tx, users)
		if err != nil {
			return err
		}
		summary.Internships = len(internships)

		applicationCount, err := seedApplications(tx, students, internships)
		if err != nil {
			return err
		}
		summary.Applications = applicationCount
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("seed development data: %w", err)
	}
	return summary, nil
}

func seedUsers(tx *gorm.DB, passwordHash string) (map[string]models.User, error) {
	seeds := []seedUser{
		{key: "admin", email: AdminEmail, fullName: "Portal Administrator", role: models.RoleAdmin},
		{key: "student", email: StudentEmail, fullName: "Aarav Sharma", role: models.RoleStudent},
		{key: "student2", email: StudentTwoEmail, fullName: "Maya Gurung", role: models.RoleStudent},
		{key: "employer", email: EmployerEmail, fullName: "Nisha Adhikari", role: models.RoleEmployer},
		{key: "employer_pending", email: "seed.employer.pending@jobportal.test", fullName: "Suman Karki", role: models.RoleEmployer},
		{key: "employer_rejected", email: "seed.employer.rejected@jobportal.test", fullName: "Ritika Thapa", role: models.RoleEmployer},
	}

	users := make(map[string]models.User, len(seeds))
	for _, seed := range seeds {
		user := models.User{
			ID:           seedID("user", seed.key),
			Email:        seed.email,
			PasswordHash: passwordHash,
			FullName:     seed.fullName,
			Role:         seed.role,
		}
		var existing models.User
		lookup := tx.Unscoped().Where("email = ?", seed.email).Limit(1).Find(&existing)
		if lookup.Error != nil {
			return nil, fmt.Errorf("find user %s: %w", seed.email, lookup.Error)
		}
		if lookup.RowsAffected > 0 {
			user.ID = existing.ID
			user.CreatedAt = existing.CreatedAt
			if err := tx.Unscoped().Save(&user).Error; err != nil {
				return nil, fmt.Errorf("update user %s: %w", seed.email, err)
			}
		} else {
			if err := tx.Create(&user).Error; err != nil {
				return nil, fmt.Errorf("create user %s: %w", seed.email, err)
			}
		}
		if err := tx.Where("email = ?", seed.email).First(&user).Error; err != nil {
			return nil, fmt.Errorf("upsert user %s: %w", seed.email, err)
		}
		users[seed.key] = user
	}
	return users, nil
}

func seedStudentProfiles(tx *gorm.DB, users map[string]models.User) (map[string]models.StudentProfile, error) {
	profiles := []struct {
		key     string
		profile models.StudentProfile
	}{
		{key: "student", profile: models.StudentProfile{
			ID: seedID("student-profile", "aarav"), UserID: users["student"].ID,
			FullName: "Aarav Sharma", Phone: "+977-9800000001", Location: "Kathmandu, Nepal",
			Bio:         "Computer science student focused on dependable web products and accessible user experiences.",
			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
			CurrentSemester: "7th Semester", GraduationYear: time.Now().Year() + 1,
			PreferredJobCategories: "Software Engineering, Frontend Development, Backend Development",
			PreferredLocations:     "Kathmandu, Lalitpur, Remote", PreferredWorkMode: "hybrid",
			Availability: "Immediately", ExpectedSalary: "NPR 20,000/month",
			LinkedinURL: "https://www.linkedin.com/in/aarav-sharma-seed", GithubURL: "https://github.com/aarav-sharma-seed",
			PortfolioURL: "https://aarav-sharma.example", IsSearchable: true, ProfileCompletionPercent: 100,
		}},
		{key: "student2", profile: models.StudentProfile{
			ID: seedID("student-profile", "maya"), UserID: users["student2"].ID,
			FullName: "Maya Gurung", Phone: "+977-9800000002", Location: "Pokhara, Nepal",
			Bio:         "Information technology student interested in product design, data, and thoughtful digital services.",
			CollegeName: "Prithvi Narayan Campus", Degree: "BIT", FacultyOrMajor: "Information Technology",
			CurrentSemester: "6th Semester", GraduationYear: time.Now().Year() + 1,
			PreferredJobCategories: "Product Design, Data Analytics, Quality Assurance",
			PreferredLocations:     "Pokhara, Kathmandu, Remote", PreferredWorkMode: "remote",
			Availability: "Part-time", ExpectedSalary: "NPR 18,000/month",
			LinkedinURL: "https://www.linkedin.com/in/maya-gurung-seed", GithubURL: "https://github.com/maya-gurung-seed",
			PortfolioURL: "https://maya-gurung.example", IsSearchable: true, ProfileCompletionPercent: 100,
		}},
	}

	result := make(map[string]models.StudentProfile, len(profiles))
	for _, item := range profiles {
		profile := item.profile
		if err := saveStudentProfile(tx, &profile); err != nil {
			return nil, fmt.Errorf("upsert student profile %s: %w", item.key, err)
		}
		if err := tx.Where("user_id = ?", profile.UserID).First(&profile).Error; err != nil {
			return nil, fmt.Errorf("load student profile %s: %w", item.key, err)
		}
		result[item.key] = profile
	}
	return result, nil
}

func seedRecruiterProfiles(tx *gorm.DB, users map[string]models.User) (map[string]models.RecruiterProfile, error) {
	profiles := []struct {
		key     string
		profile models.RecruiterProfile
	}{
		{key: "employer", profile: models.RecruiterProfile{
			ID: seedID("recruiter-profile", "techkarkhana"), UserID: users["employer"].ID,
			OrganizationName: "TechKarkhana Nepal", Designation: "Talent Acquisition Lead",
			OrganizationWebsite: "https://techkarkhana.example", OrganizationAddress: "Kupondole, Lalitpur",
			Industry: "Software and Technology", OrganizationSize: "51-200",
			OrganizationAbout:  "A Nepal-based product engineering company building practical software for growing businesses.",
			VerificationStatus: string(enums.OrganizationVerificationApproved),
		}},
		{key: "employer_pending", profile: models.RecruiterProfile{
			ID: seedID("recruiter-profile", "himalayan-analytics"), UserID: users["employer_pending"].ID,
			OrganizationName: "Himalayan Analytics", Designation: "People Operations Manager",
			OrganizationWebsite: "https://himalayan-analytics.example", OrganizationAddress: "Naxal, Kathmandu",
			Industry: "Data and Analytics", OrganizationSize: "11-50",
			OrganizationAbout:  "An analytics studio helping local organizations make responsible, data-informed decisions.",
			VerificationStatus: string(enums.OrganizationVerificationPending),
		}},
		{key: "employer_rejected", profile: models.RecruiterProfile{
			ID: seedID("recruiter-profile", "brightpath"), UserID: users["employer_rejected"].ID,
			OrganizationName: "BrightPath Media", Designation: "Hiring Coordinator",
			OrganizationWebsite: "https://brightpath-media.example", OrganizationAddress: "Baneshwor, Kathmandu",
			Industry: "Marketing and Advertising", OrganizationSize: "1-10",
			OrganizationAbout:  "A small digital media agency serving education and hospitality clients.",
			VerificationStatus: string(enums.OrganizationVerificationRejected),
		}},
	}

	result := make(map[string]models.RecruiterProfile, len(profiles))
	for _, item := range profiles {
		profile := item.profile
		if err := saveRecruiterProfile(tx, &profile); err != nil {
			return nil, fmt.Errorf("upsert recruiter profile %s: %w", item.key, err)
		}
		if err := tx.Where("user_id = ?", profile.UserID).First(&profile).Error; err != nil {
			return nil, fmt.Errorf("load recruiter profile %s: %w", item.key, err)
		}
		result[item.key] = profile
	}
	return result, nil
}

func seedStudentDetails(tx *gorm.DB, students map[string]models.StudentProfile) error {
	now := time.Now().UTC()
	projectStart := now.AddDate(0, -5, 0)
	projectEnd := now.AddDate(0, -2, 0)
	issued := now.AddDate(0, -8, 0)

	records := []interface{}{
		&models.StudentEducation{ID: seedID("education", "aarav"), ProfileID: students["student"].ID, Institute: "Amrit Science Campus", Degree: "BSc CSIT", Field: "Computer Science", StartYear: now.Year() - 3, EndYear: now.Year() + 1, Description: "Coursework in software engineering, databases, networking, and distributed systems."},
		&models.StudentEducation{ID: seedID("education", "maya"), ProfileID: students["student2"].ID, Institute: "Prithvi Narayan Campus", Degree: "BIT", Field: "Information Technology", StartYear: now.Year() - 3, EndYear: now.Year() + 1, Description: "Coursework in information systems, human-computer interaction, and data analysis."},
		&models.StudentSkill{ID: seedID("skill", "aarav-go"), ProfileID: students["student"].ID, Name: "Go", Level: "intermediate"},
		&models.StudentSkill{ID: seedID("skill", "aarav-react"), ProfileID: students["student"].ID, Name: "React", Level: "advanced"},
		&models.StudentSkill{ID: seedID("skill", "aarav-postgresql"), ProfileID: students["student"].ID, Name: "PostgreSQL", Level: "intermediate"},
		&models.StudentSkill{ID: seedID("skill", "maya-figma"), ProfileID: students["student2"].ID, Name: "Figma", Level: "advanced"},
		&models.StudentSkill{ID: seedID("skill", "maya-sql"), ProfileID: students["student2"].ID, Name: "SQL", Level: "intermediate"},
		&models.StudentSkill{ID: seedID("skill", "maya-python"), ProfileID: students["student2"].ID, Name: "Python", Level: "intermediate"},
		&models.StudentProject{ID: seedID("project", "aarav-campus-events"), ProfileID: students["student"].ID, Title: "Campus Events Platform", Description: "A responsive event discovery and registration platform for student clubs.", Link: "https://github.com/aarav-sharma-seed/campus-events", StartDate: &projectStart, EndDate: &projectEnd},
		&models.StudentProject{ID: seedID("project", "maya-transit"), ProfileID: students["student2"].ID, Title: "Pokhara Transit Study", Description: "A dashboard and UX study exploring common public transit pain points.", Link: "https://maya-gurung.example/transit-study", StartDate: &projectStart, EndDate: &projectEnd},
		&models.StudentCertification{ID: seedID("certification", "aarav-web"), ProfileID: students["student"].ID, Name: "Web Development Fundamentals", Authority: "freeCodeCamp", License: "SEED-AARAV-WEB", URL: "https://freecodecamp.org", IssuedDate: &issued},
		&models.StudentCertification{ID: seedID("certification", "maya-data"), ProfileID: students["student2"].ID, Name: "Data Analytics Foundations", Authority: "Coursera", License: "SEED-MAYA-DATA", URL: "https://coursera.org", IssuedDate: &issued},
	}
	for _, record := range records {
		if err := upsert(tx, record, "id"); err != nil {
			return fmt.Errorf("upsert student detail: %w", err)
		}
	}
	return nil
}

func seedVerifications(tx *gorm.DB, users map[string]models.User, recruiters map[string]models.RecruiterProfile) (int, error) {
	now := time.Now().UTC()
	submitted := now.AddDate(0, 0, -10)
	reviewed := now.AddDate(0, 0, -8)
	pendingSubmitted := now.AddDate(0, 0, -2)
	rejectedSubmitted := now.AddDate(0, 0, -14)
	rejectedReviewed := now.AddDate(0, 0, -12)
	adminID := users["admin"].ID

	verifications := []models.OrganizationVerification{
		{ID: seedID("verification", "techkarkhana"), RecruiterProfileID: recruiters["employer"].ID, Status: enums.OrganizationVerificationApproved, Method: enums.VerificationMethodManual, OrganizationEmail: "careers@techkarkhana.example", EmailDomain: "techkarkhana.example", ReviewedBy: &adminID, ReviewNotes: "Organization details confirmed for the development seed.", SubmittedAt: &submitted, ReviewedAt: &reviewed, VerifiedAt: &reviewed},
		{ID: seedID("verification", "himalayan-analytics"), RecruiterProfileID: recruiters["employer_pending"].ID, Status: enums.OrganizationVerificationPending, Method: enums.VerificationMethodDocument, OrganizationEmail: "people@himalayan-analytics.example", EmailDomain: "himalayan-analytics.example", DocumentType: "Company registration certificate", SubmittedAt: &pendingSubmitted},
		{ID: seedID("verification", "brightpath"), RecruiterProfileID: recruiters["employer_rejected"].ID, Status: enums.OrganizationVerificationRejected, Method: enums.VerificationMethodManual, OrganizationEmail: "hiring@brightpath-media.example", EmailDomain: "brightpath-media.example", ReviewedBy: &adminID, RejectionReason: "Organization registration details could not be confirmed.", ReviewNotes: "Employer may resubmit with a valid registration document.", SubmittedAt: &rejectedSubmitted, ReviewedAt: &rejectedReviewed},
	}
	for i := range verifications {
		if err := saveVerification(tx, &verifications[i]); err != nil {
			return 0, fmt.Errorf("upsert organization verification: %w", err)
		}
	}
	return len(verifications), nil
}

func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]models.Internship, error) {
	now := time.Now().UTC()
	future := func(days int) *time.Time { value := now.AddDate(0, 0, days); return &value }
	past := func(days int) *time.Time { value := now.AddDate(0, 0, -days); return &value }
	approvedEmployer := users["employer"].ID

	items := []struct {
		key        string
		internship models.Internship
	}{
		{key: "frontend", internship: models.Internship{ID: seedID("internship", "frontend"), IssuedBy: approvedEmployer, Title: "Frontend Engineering Intern", Description: "Build accessible, responsive product interfaces with React and TypeScript alongside an experienced engineering team.", Location: "Lalitpur, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "JavaScript, React, HTML, CSS", PreferredSkills: "TypeScript, Next.js, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 18000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(30), ApplicationDeadline: future(21), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Implement product features, review UI quality, and collaborate with design and backend teams.", Benefits: "Mentorship, lunch allowance, flexible hybrid schedule", Status: enums.InternshipStatusPublished, IsActive: true}},
		{key: "backend", internship: models.Internship{ID: seedID("internship", "backend"), IssuedBy: approvedEmployer, Title: "Backend Go Intern", Description: "Help design and implement secure APIs and background services for a growing business software platform.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 4, DurationUnit: "months", WorkingHours: "9:30 AM - 5:30 PM", RequiredSkills: "Go, REST APIs, SQL", PreferredSkills: "PostgreSQL, Docker, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 22000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(35), ApplicationDeadline: future(28), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Develop APIs, write tests, investigate defects, and contribute to technical documentation.", Benefits: "Engineering mentorship, device allowance, potential full-time offer", Status: enums.InternshipStatusPublished, IsActive: true}},
		{key: "product-design", internship: models.Internship{ID: seedID("internship", "product-design"), IssuedBy: approvedEmployer, Title: "Product Design Intern", Description: "Research user needs and turn findings into clear flows, prototypes, and polished interface specifications.", Location: "Remote", WorkMode: "remote", InternshipType: "paid", Duration: 12, DurationUnit: "weeks", WorkingHours: "Flexible, 25 hours per week", RequiredSkills: "Figma, Wireframing, User Research", PreferredSkills: "Design systems, Prototyping, Accessibility", RequiredEducation: "Open to students from any relevant program", EligiblePrograms: "BIT, BCA, BIM, Design", EligibleSemester: "4th semester and above", StipendAmount: 16000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(25), ApplicationDeadline: future(18), ApplicationEmail: "design@techkarkhana.example", Responsibilities: "Create prototypes, support interviews, document design decisions, and review implementation.", Benefits: "Design mentorship, flexible hours, remote work", Status: enums.InternshipStatusPublished, IsActive: true}},
		{key: "data-closed", internship: models.Internship{ID: seedID("internship", "data-closed"), IssuedBy: approvedEmployer, Title: "Data Analytics Intern", Description: "Prepare operational datasets and build concise dashboards that help teams understand product performance.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "SQL, Spreadsheets, Data Visualization", PreferredSkills: "Python, Power BI", StipendAmount: 15000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: past(20), ApplicationDeadline: past(30), Responsibilities: "Clean data, validate reports, and present weekly insights.", Benefits: "Mentorship and completion certificate", Status: enums.InternshipStatusClosed, IsActive: false}},
		{key: "qa-private", internship: models.Internship{ID: seedID("internship", "qa-private"), IssuedBy: users["employer_pending"].ID, Title: "QA Automation Intern", Description: "Draft internship awaiting organization verification before it can be published to students.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", RequiredSkills: "Testing fundamentals, JavaScript", PreferredSkills: "Playwright, Cypress", StipendAmount: 17000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(40), ApplicationDeadline: future(32), Responsibilities: "Create test cases and automate high-value product workflows.", Benefits: "QA mentorship", Status: enums.InternshipStatusPrivate, IsActive: false}},
		{key: "marketing-private", internship: models.Internship{ID: seedID("internship", "marketing-private"), IssuedBy: users["employer_rejected"].ID, Title: "Digital Marketing Intern", Description: "Private internship retained to demonstrate posting restrictions for an unverified organization.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 8, DurationUnit: "weeks", RequiredSkills: "Content writing, Social media", PreferredSkills: "SEO, Analytics", StipendAmount: 12000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(30), ApplicationDeadline: future(20), Responsibilities: "Support campaign planning, publishing, and reporting.", Benefits: "Flexible schedule", Status: enums.InternshipStatusPrivate, IsActive: false}},
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

func seedApplications(tx *gorm.DB, students map[string]models.StudentProfile, internships map[string]models.Internship) (int, error) {
	now := time.Now().UTC()
	timePtr := func(value time.Time) *time.Time { return &value }
	applications := []models.InternshipApplication{
		{ID: seedID("application", "aarav-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusShortlisted, EmployerNote: "Strong frontend portfolio; invite to the technical conversation.", AppliedAt: now.AddDate(0, 0, -9), ReviewedAt: timePtr(now.AddDate(0, 0, -8)), ShortlistedAt: timePtr(now.AddDate(0, 0, -6))},
		{ID: seedID("application", "aarav-backend"), InternshipID: internships["backend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusSubmitted, AppliedAt: now.AddDate(0, 0, -2)},
		{ID: seedID("application", "aarav-data"), InternshipID: internships["data-closed"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusRejected, EmployerNote: "Role required more advanced analytics experience for this intake.", AppliedAt: now.AddDate(0, -2, 0), ReviewedAt: timePtr(now.AddDate(0, -2, 2)), RejectedAt: timePtr(now.AddDate(0, -2, 3))},
		{ID: seedID("application", "maya-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusReviewing, EmployerNote: "Application is under portfolio review.", AppliedAt: now.AddDate(0, 0, -5), ReviewedAt: timePtr(now.AddDate(0, 0, -4))},
		{ID: seedID("application", "maya-backend"), InternshipID: internships["backend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusAccepted, EmployerNote: "Offer accepted for the upcoming cohort.", AppliedAt: now.AddDate(0, -1, -5), ReviewedAt: timePtr(now.AddDate(0, -1, -3)), ShortlistedAt: timePtr(now.AddDate(0, -1, 1)), AcceptedAt: timePtr(now.AddDate(0, 0, -18))},
		{ID: seedID("application", "maya-design"), InternshipID: internships["product-design"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusWithdrawn, AppliedAt: now.AddDate(0, 0, -12), WithdrawnAt: timePtr(now.AddDate(0, 0, -10))},
	}
	for i := range applications {
		if err := saveApplication(tx, &applications[i]); err != nil {
			return 0, fmt.Errorf("upsert internship application: %w", err)
		}
	}
	return len(applications), nil
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

func saveVerification(tx *gorm.DB, verification *models.OrganizationVerification) error {
	var existing models.OrganizationVerification
	lookup := tx.Where("recruiter_profile_id = ?", verification.RecruiterProfileID).Limit(1).Find(&existing)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected == 0 {
		return tx.Create(verification).Error
	}
	verification.ID = existing.ID
	verification.CreatedAt = existing.CreatedAt
	return tx.Save(verification).Error
}

func saveApplication(tx *gorm.DB, application *models.InternshipApplication) error {
	var existing models.InternshipApplication
	lookup := tx.Where(
		"student_id = ? AND internship_id = ?",
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
