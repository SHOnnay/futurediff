package durablewrite

import "sync"

// Test-only deterministic fault injectors. They are exported so other
// packages' tests can construct them, but they are never wired to production
// configuration: nothing outside tests creates an Injector.

// FaultMap fails every invocation of the named operations with the mapped
// error until the entries are removed. Concurrent use is safe.
type FaultMap struct {
	mu   sync.Mutex
	fail map[string]error
}

// NewFaultMap returns an injector that fails the given operations every time
// they are attempted.
func NewFaultMap(fail map[string]error) *FaultMap {
	return &FaultMap{fail: fail}
}

// Set fails the operation with err; err == nil clears the fault.
func (f *FaultMap) Set(operation string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = map[string]error{}
	}
	if err == nil {
		delete(f.fail, operation)
		return
	}
	f.fail[operation] = err
}

// Before implements Injector.
func (f *FaultMap) Before(operation string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fail[operation]
}

// OneShot fails each mapped operation exactly once (the first time it is
// attempted), then succeeds on later attempts. Concurrent use is safe.
type OneShot struct {
	mu   sync.Mutex
	fail map[string]error
}

// NewOneShot returns an injector that fails each given operation once.
func NewOneShot(fail map[string]error) *OneShot {
	return &OneShot{fail: fail}
}

// Before implements Injector.
func (o *OneShot) Before(operation string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err, ok := o.fail[operation]; ok {
		delete(o.fail, operation)
		return err
	}
	return nil
}
