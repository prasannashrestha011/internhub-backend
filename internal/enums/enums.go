package enums

type OrganizationVerificationStatus string

const (
	OrganizationVerificationPending  OrganizationVerificationStatus = "pending"
	OrganizationVerificationApproved OrganizationVerificationStatus = "approved"
	OrganizationVerificationRejected OrganizationVerificationStatus = "rejected"

	//for reviewed verifications, which are either approved or rejected
	OrganizationVerificationReviewed OrganizationVerificationStatus = "reviewed"
)

type OrganizationVerificationMethod string

const (
	VerificationMethodDocument OrganizationVerificationMethod = "document"
	VerificationMethodManual   OrganizationVerificationMethod = "manual"
)
