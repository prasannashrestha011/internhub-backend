package enums

type InternshipStatus string

const (
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
