// Package apibudget is the plan for spending the monthly request allowance
// CFBD and CBBD meter the background syncs against.
//
// Each sport's cadence test counts its own jobs across real months and fails
// when a month overruns its share here; this package's test fails when the
// shares no longer fit the allowance. The two halves are what make a change
// on either side answerable -- before them, football was capped at 10,000 on
// the strength of a comment saying the rest would fit, and nothing looked at
// basketball at all.
//
// Nothing outside tests reads these. They are constants in a package rather
// than in a test so both sports' tests can hold the same numbers.
package apibudget

const (
	// MonthlyAllowance is the requests a month the provider allows. The
	// README treats it as one meter shared by both keys; if the keys are metered
	// separately this plan is conservative, not wrong.
	MonthlyAllowance = 30_000

	// Football is the share for the football jobs a cadence test counts:
	// lines, games, scoreboard, the two daily context jobs, and restarts.
	// TestFootballCadenceStaysWithinMonthlyCallBudget measures its worst
	// month at ~7,770.
	Football = 10_000

	// Basketball is the share for the basketball games and lines jobs at the
	// shortest interval configuration allows, restarts included.
	// TestBasketballCadenceStaysWithinMonthlyCallBudget measures its worst
	// month at ~9,000.
	Basketball = 9_500

	// Reserve is everything no cadence test counts. The football calendar job
	// is ~800 a month and grows by a season a year, since each run walks every
	// season from 2002; rankings are ~120; and a manual seed is tens of
	// requests for basketball and a few hundred for a football season.
	Reserve = 2_000
)
