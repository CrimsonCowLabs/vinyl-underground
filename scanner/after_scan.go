package scanner

import "sync"

var (
	afterScanMu sync.Mutex
	afterScan   []func()
)

// AfterScan registers fn to be called after every scan this process runs through the scanner
// (the watcher's, the schedule's, the UI's and the Ripper's), once the scan lock is released.
// fn must not block: it runs on the scanning goroutine. The drop reconciler uses it to look for
// Files dropped into the Inbox (#35).
func AfterScan(fn func()) {
	afterScanMu.Lock()
	defer afterScanMu.Unlock()
	afterScan = append(afterScan, fn)
}

func notifyAfterScan() {
	afterScanMu.Lock()
	fns := append([]func(){}, afterScan...)
	afterScanMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}
