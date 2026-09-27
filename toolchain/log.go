package toolchain

// emit forwards a progress line to an optional logging function. The pipeline
// stages accept a Logf callback so callers decide where progress goes; tests
// usually leave it nil.
func emit(logf func(format string, args ...any), format string, args ...any) {
	if logf != nil {
		logf(format, args...)
	}
}
