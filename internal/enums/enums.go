package enums

type OrganizationVerificationStatus string
type InternshipStatus string

const (
	OrganizationVerificationPending  OrganizationVerificationStatus = "pending"
	OrganizationVerificationApproved OrganizationVerificationStatus = "approved"
	OrganizationVerificationRejected OrganizationVerificationStatus = "rejected"

	//for reviewed verifications, which are either approved or rejected
	OrganizationVerificationReviewed OrganizationVerificationStatus = "reviewed"

	// Internships
	InternshipStatusPrivate   InternshipStatus = "private"
	InternshipStatusPublished InternshipStatus = "published"
	InternshipStatusClosed    InternshipStatus = "closed"
	InternshipStatusExpired   InternshipStatus = "expired"
)

func (s InternshipStatus) IsValid() bool {
	switch s {
	case InternshipStatusPrivate,
		InternshipStatusPublished,
		InternshipStatusClosed,
		InternshipStatusExpired:
		return true
	default:
		return false
	}
}

func (s InternshipStatus) IsActive() bool {
	return s == InternshipStatusPublished
}

type OrganizationVerificationMethod string

const (
	VerificationMethodDocument OrganizationVerificationMethod = "document"
	VerificationMethodManual   OrganizationVerificationMethod = "manual"
)
