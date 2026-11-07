package cli

import "devbox/internal/app"

// Pane navigation only reads this invocation's snapshot. Explicit refresh and
// saved operations replace it so changing focus never resolves every config.
func (f *frontend) sessionReport() (app.InventoryReport, error) {
	if f.sessionSnapshot == nil || f.sessionSnapshotDirty {
		return f.refreshSessions()
	}
	return *f.sessionSnapshot, f.sessionSnapshotErr
}

func (f *frontend) invalidateSessions() {
	f.sessionSnapshotDirty = true
}

func (f *frontend) refreshSessions() (app.InventoryReport, error) {
	var report app.InventoryReport
	err := f.loadEngine()
	if err == nil {
		report, err = f.e.StatusAll(f.m.Context, "")
	}
	// Failed refreshes keep the last inventory visible, with the failure reported
	// separately. A failed initial load is also retained until explicit refresh.
	if report.Sessions != nil || f.sessionSnapshot == nil {
		f.sessionSnapshot = &report
	}
	f.sessionSnapshotErr = err
	f.sessionSnapshotDirty = false
	if report.Sessions != nil {
		f.m.RefreshNavigation(*f.sessionCollection(report, f.sortBy))
	}
	return *f.sessionSnapshot, err
}
