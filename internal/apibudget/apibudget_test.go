package apibudget

import "testing"

// Each share is enforced by its own sport's test. This is the other half: that
// raising one to make its test pass cannot quietly spend the allowance.
func TestSharesFitTheAllowance(t *testing.T) {
	if planned := Football + Basketball + Reserve; planned > MonthlyAllowance {
		t.Errorf("football %d + basketball %d + reserve %d = %d, over the %d allowance",
			Football, Basketball, Reserve, planned, MonthlyAllowance)
	}
}
