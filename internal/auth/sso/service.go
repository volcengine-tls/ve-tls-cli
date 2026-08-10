package sso

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

// PortalAPI is the narrow subset of the CloudIdentity Portal client used by the
// binding service. It is an injectable seam so tests never touch the network.
type PortalAPI interface {
	ListAccounts(ctx context.Context, accessToken string) ([]AccountInfo, error)
	ListAccountRoles(ctx context.Context, accessToken, accountID string) ([]RoleInfo, error)
}

// AccountSelector selects one account from a stable-sorted list. It is an
// injectable seam; the default CLI adapter uses numbered selection.
type AccountSelector func(accounts []AccountInfo) (AccountInfo, error)

// RoleSelector selects one role from a stable-sorted list. It is an injectable
// seam; the default CLI adapter uses numbered selection.
type RoleSelector func(roles []RoleInfo) (RoleInfo, error)

// BindingServiceConfig holds the injectable dependencies for BindingService.
type BindingServiceConfig struct {
	Portal        PortalAPI
	SelectAccount AccountSelector
	SelectRole    RoleSelector
}

// BindingService lists available accounts and roles and resolves the binding
// for an SSO session. It supports explicit account/role values or injected
// selection when omitted. The SSO region is never consulted or modified; the
// service returns binding metadata only.
type BindingService struct {
	portal        PortalAPI
	selectAccount AccountSelector
	selectRole    RoleSelector
}

// NewBindingService constructs a BindingService from the given config.
func NewBindingService(cfg *BindingServiceConfig) *BindingService {
	if cfg == nil {
		cfg = &BindingServiceConfig{}
	}
	return &BindingService{
		portal:        cfg.Portal,
		selectAccount: cfg.SelectAccount,
		selectRole:    cfg.SelectRole,
	}
}

// BindingResult is the resolved account/role binding for an SSO session.
type BindingResult struct {
	AccountID   string
	AccountName string
	RoleName    string
}

// ResolveBinding lists all accounts and roles accessible to accessToken and
// returns the selected binding. If explicitAccountID or explicitRoleName is
// non-empty, the corresponding value is used directly (after verifying it is
// available); otherwise the injected selector is used.
//
// Accounts and roles are stable-sorted before selection. Accounts with an empty
// AccountID and roles with an empty RoleName are rejected. When a role's
// AccountID is populated, it must match the selected account; a role from
// another account is not available. Selectors returning incomplete or
// cross-account values fail. Errors are fixed and safe: they do not echo
// user-supplied or server-returned values.
func (s *BindingService) ResolveBinding(ctx context.Context, accessToken, explicitAccountID, explicitRoleName string) (*BindingResult, error) {
	if s == nil {
		return nil, errors.New("nil *BindingService")
	}
	if isNilInterface(ctx) {
		return nil, errors.New("nil context")
	}
	if isNilInterface(s.portal) {
		return nil, errors.New("nil portal client")
	}
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return nil, errors.New("access token is required")
	}

	// 1. List all accounts (the client paginates internally).
	accounts, err := s.portal.ListAccounts(ctx, token)
	if err != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "list accounts failed", Cause: err}
	}
	// Reject accounts with empty AccountID.
	validAccounts := make([]AccountInfo, 0, len(accounts))
	for _, a := range accounts {
		if strings.TrimSpace(a.AccountID) == "" {
			continue
		}
		validAccounts = append(validAccounts, a)
	}
	if len(validAccounts) == 0 {
		return nil, errors.New("no accounts available")
	}
	sortAccounts(validAccounts)

	// 2. Resolve the account.
	var account AccountInfo
	if explicit := strings.TrimSpace(explicitAccountID); explicit != "" {
		found := false
		for _, a := range validAccounts {
			if a.AccountID == explicit {
				account = a
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("account is not available")
		}
	} else {
		if s.selectAccount == nil {
			return nil, errors.New("account selector is required when no explicit account is provided")
		}
		// Pass a copy so a malicious selector cannot mutate the authoritative
		// list; the returned identity is validated against validAccounts below.
		selectorInput := make([]AccountInfo, len(validAccounts))
		copy(selectorInput, validAccounts)
		selected, err := s.selectAccount(selectorInput)
		if err != nil {
			return nil, &auth.Error{Kind: auth.ProtocolError, Description: "account selection failed", Cause: err}
		}
		// Reject incomplete selections (empty AccountID) and verify the
		// selected account is actually in the list.
		if strings.TrimSpace(selected.AccountID) == "" {
			return nil, errors.New("selected account is incomplete")
		}
		found := false
		for _, a := range validAccounts {
			if a.AccountID == selected.AccountID {
				account = a
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("selected account is not available")
		}
	}

	// 3. List all roles for the selected account (the client paginates
	// internally).
	roles, err := s.portal.ListAccountRoles(ctx, token, account.AccountID)
	if err != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "list account roles failed", Cause: err}
	}
	// Reject roles with empty RoleName or empty AccountID. A role's AccountID
	// must equal the selected account; empty AccountID is incomplete, not a
	// wildcard.
	validRoles := make([]RoleInfo, 0, len(roles))
	for _, r := range roles {
		if strings.TrimSpace(r.RoleName) == "" {
			continue
		}
		if strings.TrimSpace(r.AccountID) == "" {
			continue
		}
		if r.AccountID != account.AccountID {
			continue
		}
		validRoles = append(validRoles, r)
	}
	if len(validRoles) == 0 {
		return nil, errors.New("no roles available for account")
	}
	sortRoles(validRoles)

	// 4. Resolve the role.
	var role RoleInfo
	if explicit := strings.TrimSpace(explicitRoleName); explicit != "" {
		found := false
		for _, r := range validRoles {
			if r.RoleName == explicit {
				role = r
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("role is not available")
		}
	} else {
		if s.selectRole == nil {
			return nil, errors.New("role selector is required when no explicit role is provided")
		}
		// Pass a copy so a malicious selector cannot mutate the authoritative
		// list; the returned identity is validated against validRoles below.
		selectorInput := make([]RoleInfo, len(validRoles))
		copy(selectorInput, validRoles)
		selected, err := s.selectRole(selectorInput)
		if err != nil {
			return nil, &auth.Error{Kind: auth.ProtocolError, Description: "role selection failed", Cause: err}
		}
		// Reject incomplete selections (empty RoleName or AccountID) and
		// verify the selected role is actually in the list by both AccountID
		// and RoleName, not name only.
		if strings.TrimSpace(selected.RoleName) == "" {
			return nil, errors.New("selected role is incomplete")
		}
		if strings.TrimSpace(selected.AccountID) == "" {
			return nil, errors.New("selected role is incomplete")
		}
		if selected.AccountID != account.AccountID {
			return nil, errors.New("selected role is not available")
		}
		found := false
		for _, r := range validRoles {
			if r.RoleName == selected.RoleName && r.AccountID == selected.AccountID {
				role = r
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("selected role is not available")
		}
	}

	return &BindingResult{
		AccountID:   account.AccountID,
		AccountName: account.AccountName,
		RoleName:    role.RoleName,
	}, nil
}

// sortAccounts stable-sorts accounts by AccountID for deterministic selection.
func sortAccounts(accounts []AccountInfo) {
	sort.SliceStable(accounts, func(i, j int) bool {
		return accounts[i].AccountID < accounts[j].AccountID
	})
}

// sortRoles stable-sorts roles by RoleName for deterministic selection.
func sortRoles(roles []RoleInfo) {
	sort.SliceStable(roles, func(i, j int) bool {
		return roles[i].RoleName < roles[j].RoleName
	})
}
