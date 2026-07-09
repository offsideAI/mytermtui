//go:build darwin && cgo

package icloud

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#import <Foundation/Foundation.h>

static char *mt_errdup(NSError *err) {
	if (err == nil) return NULL;
	const char *s = [[err localizedDescription] UTF8String];
	return strdup(s ? s : "unknown error");
}

static NSURL *mt_url(const char *cpath) {
	NSString *p = [NSString stringWithUTF8String:cpath];
	if (p == nil) {
		p = [[NSFileManager defaultManager] stringWithFileSystemRepresentation:cpath length:strlen(cpath)];
	}
	return [NSURL fileURLWithPath:p];
}

static char *mt_startDownload(const char *cpath) {
	@autoreleasepool {
		NSError *err = nil;
		[[NSFileManager defaultManager] startDownloadingUbiquitousItemAtURL:mt_url(cpath) error:&err];
		return mt_errdup(err);
	}
}

static char *mt_evict(const char *cpath) {
	@autoreleasepool {
		NSError *err = nil;
		[[NSFileManager defaultManager] evictUbiquitousItemAtURL:mt_url(cpath) error:&err];
		return mt_errdup(err);
	}
}

static char *mt_trash(const char *cpath, char **outPutback) {
	@autoreleasepool {
		NSError *err = nil;
		NSURL *res = nil;
		[[NSFileManager defaultManager] trashItemAtURL:mt_url(cpath) resultingItemURL:&res error:&err];
		if (err == nil && res != nil && outPutback != NULL) {
			const char *rp = [[res path] UTF8String];
			if (rp) *outPutback = strdup(rp);
		}
		return mt_errdup(err);
	}
}

// Thread iopolicy: with materialization OFF, reads on dataless files fail
// with EDEADLK instead of silently triggering a (potentially huge) download.
static int mt_noMaterialize(void) {
	return setiopolicy_np(IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES,
	                      IOPOL_SCOPE_THREAD, IOPOL_MATERIALIZE_DATALESS_FILES_OFF);
}
static int mt_materializeDefault(void) {
	return setiopolicy_np(IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES,
	                      IOPOL_SCOPE_THREAD, IOPOL_MATERIALIZE_DATALESS_FILES_DEFAULT);
}

*/
import "C"

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

type darwinBridge struct{}

// NewBridge returns the native Foundation-backed bridge.
func NewBridge() Bridge { return darwinBridge{} }

func cerr(e *C.char) error {
	if e == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(e))
	return errors.New(C.GoString(e))
}

func (darwinBridge) StartDownload(path string) error {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return cerr(C.mt_startDownload(cs))
}

func (darwinBridge) Evict(path string) error {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return cerr(C.mt_evict(cs))
}

func (darwinBridge) Trash(path string) (string, error) {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	var out *C.char
	err := cerr(C.mt_trash(cs, &out))
	putback := ""
	if out != nil {
		putback = C.GoString(out)
		C.free(unsafe.Pointer(out))
	}
	return putback, err
}

// DownloadProgress reports the percentage brctl currently shows for a
// materializing file. Refreshes are throttled and asynchronous so the
// UI tick never blocks on the subprocess.
func (darwinBridge) DownloadProgress(path string, size int64) (float64, bool) {
	return sharedPoller.get(path, size)
}

// progressPoller shells out to `brctl status com.apple.CloudDocs` — the
// only progress source visible to non-entitled processes — at most once
// per second, in the background.
type progressPoller struct {
	mu      sync.Mutex
	entries []brctlEntry
	fetched time.Time
	running bool
}

var sharedPoller progressPoller

func (p *progressPoller) get(path string, size int64) (float64, bool) {
	p.mu.Lock()
	if time.Since(p.fetched) > time.Second && !p.running {
		p.running = true
		go p.refresh()
	}
	entries := p.entries
	p.mu.Unlock()
	return progressFor(entries, path, size)
}

func (p *progressPoller) refresh() {
	// brctl can answer in <100ms idle but routinely takes ~20s while
	// fileproviderd is busy transferring. The timeout is only a guard
	// against a truly hung process — the refresh is async, so a slow
	// answer just means the percent updates lag by that much.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "brctl", "status", "com.apple.CloudDocs").Output()
	var entries []brctlEntry
	if err == nil {
		entries = parseBrctlStatus(string(out))
	}
	p.mu.Lock()
	if err == nil {
		p.entries = entries
	}
	p.fetched = time.Now()
	p.running = false
	p.mu.Unlock()
}

// WithNoMaterialize runs fn on an OS thread whose VFS iopolicy forbids
// materializing dataless files: any read of an evicted file inside fn
// fails with EDEADLK rather than downloading it. Use around previews and
// any other content reads that must never trigger an iCloud download.
func WithNoMaterialize(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	C.mt_noMaterialize()
	defer C.mt_materializeDefault()
	fn()
}
