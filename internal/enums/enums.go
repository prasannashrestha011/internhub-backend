package enums

type OrganizationVerificationStatus string

const (
	OrganizationVerificationPending  OrganizationVerificationStatus = "pending"
	OrganizationVerificationApproved OrganizationVerificationStatus = "approved"
	OrganizationVerificationRejected OrganizationVerificationStatus = "rejected"
)

type OrganizationVerificationMethod string

const (
	VerificationMethodDocument OrganizationVerificationMethod = "document"
	VerificationMethodManual   OrganizationVerificationMethod = "manual"
)
