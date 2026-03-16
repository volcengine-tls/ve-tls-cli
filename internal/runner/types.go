package runner

type ProfileInfo struct {
	Name     string `json:"profile"`
	Region   string `json:"region,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

const (
	ErrProfileNotFound   = "profile_not_found"
	ErrProfileAmbiguous  = "profile_ambiguous"
	ErrValidation        = "validation_error"
	ErrTLSCTLError       = "tlsctl_error"
	ErrInternal          = "internal_error"
	ErrConfirmToken      = "confirm_token_invalid"
	ErrConfirmTokenExp   = "confirm_token_expired"
	ErrConfirmTokenMiss  = "confirm_token_missing"
	ErrConfirmTokenNoKey = "confirm_token_secret_missing"
)

type ResolveResult struct {
	Profile    string
	Error      string
	Candidates []ProfileInfo
}

type ConfirmRequest struct {
	Account string
	Region  string
	Profile string
	Action  string
	ArgsSig string
}
