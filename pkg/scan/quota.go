package scan

// ErrPlanLimitExceeded is persisted on scan rows when completion is rejected because
// the success-only plan quota ledger is already at limit (IMM-6b G3).
const ErrPlanLimitExceeded = "PLAN_LIMIT_EXCEEDED"
