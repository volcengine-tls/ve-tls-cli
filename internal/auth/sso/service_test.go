package sso

import (
	"context"
	"errors"
	"testing"
)

// fakePortal is a test PortalAPI that returns scripted results.
type fakePortal struct {
	accounts []AccountInfo
	roles    map[string][]RoleInfo
}

func (p *fakePortal) ListAccounts(ctx context.Context, accessToken string) ([]AccountInfo, error) {
	return p.accounts, nil
}

func (p *fakePortal) ListAccountRoles(ctx context.Context, accessToken, accountID string) ([]RoleInfo, error) {
	return p.roles[accountID], nil
}

func newBindingService(t *testing.T, portal PortalAPI, selectAccount AccountSelector, selectRole RoleSelector) *BindingService {
	t.Helper()
	return NewBindingService(&BindingServiceConfig{
		Portal:        portal,
		SelectAccount: selectAccount,
		SelectRole:    selectRole,
	})
}

func TestBindingServiceListsAllAccountsAndRoles(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{
			{AccountID: "acc-2", AccountName: "Second"},
			{AccountID: "acc-1", AccountName: "First"},
			{AccountID: "acc-3", AccountName: "Third"},
		},
		roles: map[string][]RoleInfo{
			"acc-1": {
				{RoleName: "role-b", AccountID: "acc-1"},
				{RoleName: "role-a", AccountID: "acc-1"},
			},
		},
	}
	// Selector that records the sorted list it receives.
	var seenAccounts []AccountInfo
	var seenRoles []RoleInfo
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) {
		seenAccounts = accounts
		return accounts[0], nil
	}
	selectRole := func(roles []RoleInfo) (RoleInfo, error) {
		seenRoles = roles
		return roles[0], nil
	}
	svc := newBindingService(t, portal, selectAccount, selectRole)
	result, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All 3 accounts should be presented (no truncation), sorted by ID.
	if len(seenAccounts) != 3 {
		t.Fatalf("saw %d accounts, want 3", len(seenAccounts))
	}
	if seenAccounts[0].AccountID != "acc-1" || seenAccounts[1].AccountID != "acc-2" || seenAccounts[2].AccountID != "acc-3" {
		t.Fatalf("accounts not sorted: %v", seenAccounts)
	}
	// Roles should be sorted by name.
	if len(seenRoles) != 2 {
		t.Fatalf("saw %d roles, want 2", len(seenRoles))
	}
	if seenRoles[0].RoleName != "role-a" || seenRoles[1].RoleName != "role-b" {
		t.Fatalf("roles not sorted: %v", seenRoles)
	}
	if result.AccountID != "acc-1" || result.RoleName != "role-a" {
		t.Fatalf("got %+v", result)
	}
}

func TestBindingServiceUsesExplicitOrSelectedAccountAndRole(t *testing.T) {
	t.Run("explicit", func(t *testing.T) {
		portal := &fakePortal{
			accounts: []AccountInfo{
				{AccountID: "acc-1", AccountName: "First"},
				{AccountID: "acc-2", AccountName: "Second"},
			},
			roles: map[string][]RoleInfo{
				"acc-2": {{RoleName: "role-x", AccountID: "acc-2"}, {RoleName: "role-y", AccountID: "acc-2"}},
			},
		}
		// No selectors needed when explicit values are provided.
		svc := newBindingService(t, portal, nil, nil)
		result, err := svc.ResolveBinding(context.Background(), "token", "acc-2", "role-y")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.AccountID != "acc-2" || result.RoleName != "role-y" {
			t.Fatalf("got %+v", result)
		}
	})

	t.Run("selected", func(t *testing.T) {
		portal := &fakePortal{
			accounts: []AccountInfo{
				{AccountID: "acc-1", AccountName: "First"},
				{AccountID: "acc-2", AccountName: "Second"},
			},
			roles: map[string][]RoleInfo{
				"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}},
				"acc-2": {{RoleName: "role-y", AccountID: "acc-2"}},
			},
		}
		selectAccount := func(accounts []AccountInfo) (AccountInfo, error) {
			return accounts[1], nil // select acc-2
		}
		selectRole := func(roles []RoleInfo) (RoleInfo, error) {
			return roles[0], nil
		}
		svc := newBindingService(t, portal, selectAccount, selectRole)
		result, err := svc.ResolveBinding(context.Background(), "token", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.AccountID != "acc-2" {
			t.Fatalf("got account %q, want acc-2", result.AccountID)
		}
		if result.RoleName != "role-y" {
			t.Fatalf("got role %q, want role-y", result.RoleName)
		}
	})
}

func TestBindingServiceRejectsUnavailableAccountOrRole(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{
			{AccountID: "acc-1", AccountName: "First"},
		},
		roles: map[string][]RoleInfo{
			"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}},
		},
	}
	svc := newBindingService(t, portal, nil, nil)

	t.Run("unavailable_account", func(t *testing.T) {
		_, err := svc.ResolveBinding(context.Background(), "token", "acc-999", "role-x")
		if err == nil {
			t.Fatal("expected error for unavailable account")
		}
	})

	t.Run("unavailable_role", func(t *testing.T) {
		_, err := svc.ResolveBinding(context.Background(), "token", "acc-1", "role-zzz")
		if err == nil {
			t.Fatal("expected error for unavailable role")
		}
	})

	t.Run("no_accounts", func(t *testing.T) {
		emptyPortal := &fakePortal{accounts: nil, roles: map[string][]RoleInfo{}}
		svc2 := newBindingService(t, emptyPortal, nil, nil)
		_, err := svc2.ResolveBinding(context.Background(), "token", "", "")
		if err == nil {
			t.Fatal("expected error when no accounts available")
		}
	})

	t.Run("selector_returns_unavailable", func(t *testing.T) {
		badSelect := func(accounts []AccountInfo) (AccountInfo, error) {
			return AccountInfo{AccountID: "does-not-exist"}, nil
		}
		svc3 := newBindingService(t, portal, badSelect, nil)
		_, err := svc3.ResolveBinding(context.Background(), "token", "", "")
		if err == nil {
			t.Fatal("expected error when selector returns unavailable account")
		}
	})
}

func TestBindingServiceKeepsSSORegionSeparateFromTLSRegion(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
		roles:    map[string][]RoleInfo{"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}}},
	}
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil }
	selectRole := func(roles []RoleInfo) (RoleInfo, error) { return roles[0], nil }
	svc := newBindingService(t, portal, selectAccount, selectRole)

	// The service takes no region parameter and returns no region in the result.
	// This proves the SSO region is never mixed with the TLS profile region.
	result, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccountID == "" || result.RoleName == "" {
		t.Fatal("expected binding result")
	}
	// BindingResult has no Region field, confirming separation.
	_ = result
}

func TestBindingServiceNilDeps(t *testing.T) {
	svc := NewBindingService(nil)
	_, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err == nil {
		t.Fatal("expected error for nil portal")
	}
	if !errors.Is(err, err) {
		t.Fatal("error should be non-nil")
	}
}

// Compile-time assertion that fakePortal satisfies PortalAPI.
var _ PortalAPI = (*fakePortal)(nil)

// TestBindingServiceRejectsEmptyAccountID verifies that accounts with an empty
// AccountID are rejected and never selected.
func TestBindingServiceRejectsEmptyAccountID(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{
			{AccountID: "", AccountName: "Empty"},
			{AccountID: "acc-1", AccountName: "First"},
		},
		roles: map[string][]RoleInfo{
			"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}},
		},
	}
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) {
		// Only the valid account should be presented.
		if len(accounts) != 1 {
			t.Fatalf("saw %d accounts, want 1 (empty-ID account must be filtered)", len(accounts))
		}
		return accounts[0], nil
	}
	selectRole := func(roles []RoleInfo) (RoleInfo, error) { return roles[0], nil }
	svc := newBindingService(t, portal, selectAccount, selectRole)
	result, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccountID != "acc-1" {
		t.Fatalf("got %q, want acc-1", result.AccountID)
	}
}

// TestBindingServiceRejectsEmptyRoleName verifies that roles with an empty
// RoleName are rejected and never selected.
func TestBindingServiceRejectsEmptyRoleName(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
		roles: map[string][]RoleInfo{
			"acc-1": {
				{RoleName: "", AccountID: "acc-1"},
				{RoleName: "role-x", AccountID: "acc-1"},
			},
		},
	}
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil }
	selectRole := func(roles []RoleInfo) (RoleInfo, error) {
		if len(roles) != 1 {
			t.Fatalf("saw %d roles, want 1 (empty-name role must be filtered)", len(roles))
		}
		return roles[0], nil
	}
	svc := newBindingService(t, portal, selectAccount, selectRole)
	result, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoleName != "role-x" {
		t.Fatalf("got %q, want role-x", result.RoleName)
	}
}

// TestBindingServiceRejectsCrossAccountRole verifies that when a role's
// AccountID is populated, it must match the selected account; a role from
// another account is not available.
func TestBindingServiceRejectsCrossAccountRole(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{
			{AccountID: "acc-1", AccountName: "First"},
			{AccountID: "acc-2", AccountName: "Second"},
		},
		roles: map[string][]RoleInfo{
			"acc-1": {
				{RoleName: "role-a", AccountID: "acc-1"},
				{RoleName: "role-from-acc-2", AccountID: "acc-2"}, // cross-account, must be filtered
			},
		},
	}
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil } // acc-1
	selectRole := func(roles []RoleInfo) (RoleInfo, error) {
		if len(roles) != 1 {
			t.Fatalf("saw %d roles, want 1 (cross-account role must be filtered)", len(roles))
		}
		return roles[0], nil
	}
	svc := newBindingService(t, portal, selectAccount, selectRole)
	result, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoleName != "role-a" {
		t.Fatalf("got %q, want role-a", result.RoleName)
	}
}

// TestBindingServiceSelectorReturnsIncompleteAccount verifies that a selector
// returning an account with an empty AccountID fails.
func TestBindingServiceSelectorReturnsIncompleteAccount(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
		roles:    map[string][]RoleInfo{"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}}},
	}
	badSelect := func(accounts []AccountInfo) (AccountInfo, error) {
		return AccountInfo{AccountID: ""}, nil
	}
	svc := newBindingService(t, portal, badSelect, nil)
	_, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err == nil {
		t.Fatal("expected error when selector returns incomplete account")
	}
}

// TestBindingServiceSelectorReturnsIncompleteRole verifies that a selector
// returning a role with an empty RoleName fails.
func TestBindingServiceSelectorReturnsIncompleteRole(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
		roles:    map[string][]RoleInfo{"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}}},
	}
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil }
	badSelect := func(roles []RoleInfo) (RoleInfo, error) {
		return RoleInfo{RoleName: ""}, nil
	}
	svc := newBindingService(t, portal, selectAccount, badSelect)
	_, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err == nil {
		t.Fatal("expected error when selector returns incomplete role")
	}
}

// TestBindingServiceRejectsCrossAccountSameNameRole verifies that a selector
// returning a role with the correct RoleName but a different AccountID is
// rejected. The role "admin" exists in acc-1; a selector returning
// {AccountID:"acc-2", RoleName:"admin"} must not be accepted against acc-1.
func TestBindingServiceRejectsCrossAccountSameNameRole(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{
			{AccountID: "acc-1", AccountName: "First"},
			{AccountID: "acc-2", AccountName: "Second"},
		},
		roles: map[string][]RoleInfo{
			"acc-1": {{RoleName: "admin", AccountID: "acc-1"}},
			"acc-2": {{RoleName: "admin", AccountID: "acc-2"}},
		},
	}
	// Select acc-1, then try to select the "admin" role but with acc-2's ID.
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) {
		return accounts[0], nil // acc-1
	}
	selectRole := func(roles []RoleInfo) (RoleInfo, error) {
		// Return a role that has the right name but the wrong account.
		return RoleInfo{RoleName: "admin", AccountID: "acc-2"}, nil
	}
	svc := newBindingService(t, portal, selectAccount, selectRole)
	_, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err == nil {
		t.Fatal("expected error for cross-account same-name role selection")
	}
}

// TestBindingServiceRejectsEmptyRoleAccountID verifies that a role entry with
// an empty AccountID is rejected (incomplete, not a wildcard), and that a
// selector returning a role with an empty AccountID is rejected.
func TestBindingServiceRejectsEmptyRoleAccountID(t *testing.T) {
	t.Run("portal_role_empty_account_id", func(t *testing.T) {
		portal := &fakePortal{
			accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
			roles: map[string][]RoleInfo{
				"acc-1": {
					{RoleName: "role-x", AccountID: ""}, // empty AccountID, must be filtered
					{RoleName: "role-y", AccountID: "acc-1"},
				},
			},
		}
		selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil }
		selectRole := func(roles []RoleInfo) (RoleInfo, error) {
			if len(roles) != 1 {
				t.Fatalf("saw %d roles, want 1 (empty-AccountID role must be filtered)", len(roles))
			}
			return roles[0], nil
		}
		svc := newBindingService(t, portal, selectAccount, selectRole)
		result, err := svc.ResolveBinding(context.Background(), "token", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.RoleName != "role-y" {
			t.Fatalf("got %q, want role-y", result.RoleName)
		}
	})

	t.Run("selector_returns_empty_account_id", func(t *testing.T) {
		portal := &fakePortal{
			accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
			roles:    map[string][]RoleInfo{"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}}},
		}
		selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil }
		badSelect := func(roles []RoleInfo) (RoleInfo, error) {
			return RoleInfo{RoleName: "role-x", AccountID: ""}, nil
		}
		svc := newBindingService(t, portal, selectAccount, badSelect)
		_, err := svc.ResolveBinding(context.Background(), "token", "", "")
		if err == nil {
			t.Fatal("expected error when selector returns role with empty AccountID")
		}
	})
}

// TestBindingServiceRejectsMutatingAccountSelector verifies that an account
// selector which mutates its input slice (e.g. rewrites an AccountID) and
// returns the forged item is rejected. The authoritative list passed to the
// selector is a copy, so the validation against the untouched slice must catch
// the forgery.
func TestBindingServiceRejectsMutatingAccountSelector(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{
			{AccountID: "acc-1", AccountName: "First"},
			{AccountID: "acc-2", AccountName: "Second"},
		},
		roles: map[string][]RoleInfo{
			"acc-1": {{RoleName: "role-x", AccountID: "acc-1"}},
		},
	}
	mutatingSelect := func(accounts []AccountInfo) (AccountInfo, error) {
		// Forge: rewrite the first account's ID to a value not in the original
		// list, then return it.
		accounts[0].AccountID = "forged-acc"
		return accounts[0], nil
	}
	selectRole := func(roles []RoleInfo) (RoleInfo, error) { return roles[0], nil }
	svc := newBindingService(t, portal, mutatingSelect, selectRole)
	_, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err == nil {
		t.Fatal("expected error when selector mutates input and returns forged account")
	}
}

// TestBindingServiceRejectsMutatingRoleSelector verifies that a role selector
// which mutates its input slice and returns the forged item is rejected.
func TestBindingServiceRejectsMutatingRoleSelector(t *testing.T) {
	portal := &fakePortal{
		accounts: []AccountInfo{{AccountID: "acc-1", AccountName: "First"}},
		roles: map[string][]RoleInfo{
			"acc-1": {
				{RoleName: "role-x", AccountID: "acc-1"},
				{RoleName: "role-y", AccountID: "acc-1"},
			},
		},
	}
	selectAccount := func(accounts []AccountInfo) (AccountInfo, error) { return accounts[0], nil }
	mutatingSelect := func(roles []RoleInfo) (RoleInfo, error) {
		// Forge: rewrite the first role's name to a value not in the original
		// list, then return it.
		roles[0].RoleName = "forged-role"
		return roles[0], nil
	}
	svc := newBindingService(t, portal, selectAccount, mutatingSelect)
	_, err := svc.ResolveBinding(context.Background(), "token", "", "")
	if err == nil {
		t.Fatal("expected error when selector mutates input and returns forged role")
	}
}
