package db

import "testing"

func TestTablesHaveRowLevelSecurityEnabled(t *testing.T) {
	database := openTestDatabase(t)

	tables := []string{
		"users",
		"licenses",
		"sessions",
		"password_reset_tokens",
		"waitlist_signups",
		"stripe_purchases",
		"stripe_subscriptions",
		"stripe_webhook_events",
		"credit_wallets",
		"credit_reservations",
		"credit_ledger",
		"credit_purchases",
		"smart_library_folders",
		"smart_library_assets",
		"smart_library_batches",
		"smart_library_cost_events",
		"media_search_devices",
		"media_search_assets",
		"media_search_chunks",
		"media_search_segments",
		"trusted_devices",
		"trusted_device_request_nonces",
		"agent_definitions",
		"agent_members",
		"agent_triggers",
		"agent_jobs",
		"agent_job_events",
		"agent_approvals",
		"agent_conversations",
		"agent_conversation_events",
		"agent_attachments",
		"agent_artifacts",
		"spaces",
		"space_members",
		"space_invitations",
		"space_messages",
		"space_nodes",
		"space_agents",
		"space_workflows",
		"space_runs",
		"space_events",
		"space_inbox_items",
		"realtime_tickets",
		"space_resolve_tickets",
	}

	for _, table := range tables {
		var rowSecurityEnabled bool
		var rowSecurityForced bool
		err := database.Conn.QueryRow(
			`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`,
			table,
		).Scan(&rowSecurityEnabled, &rowSecurityForced)
		if err != nil {
			t.Fatalf("failed to inspect RLS settings for %s: %v", table, err)
		}
		if !rowSecurityEnabled {
			t.Fatalf("%s has row-level security disabled", table)
		}
		if !rowSecurityForced {
			t.Fatalf("%s does not force row-level security for table owners", table)
		}

		var policyCount int
		err = database.Conn.QueryRow(
			`SELECT COUNT(*) FROM pg_policies WHERE schemaname = 'public' AND tablename = $1`,
			table,
		).Scan(&policyCount)
		if err != nil {
			t.Fatalf("failed to inspect RLS policies for %s: %v", table, err)
		}
		if policyCount == 0 {
			t.Fatalf("%s has RLS enabled without policies", table)
		}
	}
}
