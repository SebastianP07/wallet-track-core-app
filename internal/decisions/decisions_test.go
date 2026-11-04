// Package decisions_test tracks business decisions D1-D11 (PLAN.md §1) until
// the phase that implements each one replaces its entry with real domain tests.
package decisions_test

import "testing"

func TestBusinessDecisions(t *testing.T) {
	pending := []struct {
		name  string
		phase int
		rule  string
	}{
		{"D1_por_pagar_is_computed_by_the_backend", 6, "Por pagar = sum of pending fixed + occasional items, from own fixtures"},
		{"D1_por_llegar_is_computed_by_the_backend", 7, "Por llegar = sum of pending income items, from own fixtures"},
		{"D2_monthly_balance_ignores_settled_status", 11, "Balance = Llega - Se va, regardless of paid/received status"},
		{"D3_card_charges_are_informational", 8, "card charges are excluded from Por pagar, Se va and Balance"},
		{"D4_one_income_item_per_owing_platform_member", 9, "members still covered by their last payment get no item"},
		{"D5_tags_never_affect_totals", 6, "at most one optional tag per item; lists filter by tag"},
		{"D6_paying_an_item_never_changes_a_balance", 4, "Total actual = sum of current non-savings balances"},
		{"D7_closing_a_month_marks_unsettled_items_overdue", 5, "pending items become overdue; none are carried over or deleted"},
		{"D7_overdue_items_stay_out_of_current_totals", 10, "marking one done keeps it in its original month"},
		{"D8_member_owes_again_when_coverage_ends", 9, "a semiannual member paid in month M owes again in M+6"},
		{"D9_savings_accounts_are_excluded_from_total_actual", 4, "Ahorro = sum of savings balances"},
		{"D10_platform_shares_round_up_to_the_whole_peso", 9, "ceil(64,700 / 3) = 21,567"},
		{"D11_due_day_clamps_to_month_end", 5, "day 31 becomes the last day of a shorter month"},
		{"D11_urgency_colors_by_days_left", 6, "boundaries at 11 / 10 / 4 / 3 / 0 / -1 days left, America/Bogota"},
	}

	for _, decision := range pending {
		t.Run(decision.name, func(t *testing.T) {
			t.Skipf("pending: Phase %d: %s", decision.phase, decision.rule)
		})
	}
}
