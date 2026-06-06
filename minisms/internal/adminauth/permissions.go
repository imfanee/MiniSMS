// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package adminauth

import "strings"

// Permission keys stored in admin_users.permissions (JSON array of strings).
const (
	PermDashboardStats      = "dashboard_stats"
	PermCarriersView        = "carriers_view"
	PermCarriersEdit        = "carriers_edit"
	PermCarriersPayment     = "carriers_payment"
	PermRateGroupsView      = "rate_groups_view"
	PermRateGroupsEdit      = "rate_groups_edit"
	PermRateGroupsManage    = "rate_groups_manage"
	PermRoutingGroupsView   = "routing_groups_view"
	PermRoutingGroupsEdit   = "routing_groups_edit"
	PermRoutingGroupsManage = "routing_groups_manage"
	PermClientsView         = "clients_view"
	PermClientsEdit         = "clients_edit"
	PermClientsPayment      = "clients_payment"
	PermCurrenciesView      = "currencies_view"
	PermCurrenciesEdit      = "currencies_edit"
	PermSenderIDsView       = "sender_ids_view"
	PermSenderIDsEdit       = "sender_ids_edit"
	PermSMSLogsView         = "sms_logs_view"
	PermSimulate            = "simulate"
)

// PermissionDef describes one assignable permission for admin user forms.
type PermissionDef struct {
	Key   string
	Label string
	Group string
}

// AllAssignablePermissions is the full list shown when editing a non-super admin.
var AllAssignablePermissions = []PermissionDef{
	{PermDashboardStats, "Dashboard stats & reports", "Dashboard"},
	{PermCarriersView, "Carriers — view", "Carriers"},
	{PermCarriersEdit, "Carriers — add / edit", "Carriers"},
	{PermCarriersPayment, "Carriers — add payment", "Carriers"},
	{PermRateGroupsView, "Rate groups — view", "Rate Groups"},
	{PermRateGroupsEdit, "Rate groups — add / edit", "Rate Groups"},
	{PermRateGroupsManage, "Rate groups — manage entries", "Rate Groups"},
	{PermRoutingGroupsView, "Routing groups — view", "Routing Groups"},
	{PermRoutingGroupsEdit, "Routing groups — add / edit", "Routing Groups"},
	{PermRoutingGroupsManage, "Routing groups — manage routes", "Routing Groups"},
	{PermClientsView, "Clients — view", "Clients"},
	{PermClientsEdit, "Clients — add / edit", "Clients"},
	{PermClientsPayment, "Clients — add payment / credit", "Clients"},
	{PermCurrenciesView, "Currencies — view", "Currencies"},
	{PermCurrenciesEdit, "Currencies — add / edit", "Currencies"},
	{PermSenderIDsView, "Sender IDs — view", "Sender IDs"},
	{PermSenderIDsEdit, "Sender IDs — add / edit", "Sender IDs"},
	{PermSMSLogsView, "SMS logs — view", "SMS Logs"},
	{PermSimulate, "Diagnoses — simulate & send", "Diagnoses"},
}

// HasPermission returns true if super admin or the key is in perms.
func HasPermission(isSuper bool, perms []string, key string) bool {
	if isSuper {
		return true
	}
	for _, p := range perms {
		if p == key {
			return true
		}
	}
	return false
}

// PermMap builds a map for templates and quick lookups.
func PermMap(isSuper bool, perms []string) map[string]bool {
	m := make(map[string]bool)
	for _, d := range AllAssignablePermissions {
		m[d.Key] = HasPermission(isSuper, perms, d.Key)
	}
	return m
}

// ParsePermissionList deduplicates permission keys from form input.
func ParsePermissionList(keys []string) []string {
	allowed := make(map[string]struct{})
	for _, d := range AllAssignablePermissions {
		allowed[d.Key] = struct{}{}
	}
	var out []string
	seen := make(map[string]struct{})
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := allowed[k]; !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}
