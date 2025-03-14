//go:build go1.16
// +build go1.16

// Before Go 1.16 filepath.Match returned early on a failed match,
// and thus did not report any later syntax error in the pattern.
// https://go.dev/doc/go1.16#path/filepath

package main

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	rtest "github.com/restic/restic/internal/test"
)

func TestBackupFailsWhenUsingInvalidPatterns(t *testing.T) {
	env, cleanup := withTestEnvironment(t)
	defer cleanup()

	testRunInit(t, env.gopts)

	var err error

	// Test --exclude
	err = testRunBackupAssumeFailure(t, filepath.Dir(env.testdata), []string{"testdata"}, BackupOptions{Excludes: []string{"*[._]log[.-][0-9]", "!*[._]log[.-][0-9]"}}, env.gopts)

	rtest.Equals(t, `Fatal: --exclude: invalid pattern(s) provided:
*[._]log[.-][0-9]
!*[._]log[.-][0-9]`, err.Error())

	// Test --iexclude
	err = testRunBackupAssumeFailure(t, filepath.Dir(env.testdata), []string{"testdata"}, BackupOptions{InsensitiveExcludes: []string{"*[._]log[.-][0-9]", "!*[._]log[.-][0-9]"}}, env.gopts)

	rtest.Equals(t, `Fatal: --iexclude: invalid pattern(s) provided:
*[._]log[.-][0-9]
!*[._]log[.-][0-9]`, err.Error())
}

func TestBackupFailsWhenUsingInvalidPatternsFromFile(t *testing.T) {
	env, cleanup := withTestEnvironment(t)
	defer cleanup()

	testRunInit(t, env.gopts)

	// Create an exclude file with some invalid patterns
	excludeFile := env.base + "/excludefile"
	fileErr := ioutil.WriteFile(excludeFile, []byte("*.go\n*[._]log[.-][0-9]\n!*[._]log[.-][0-9]"), 0644)
	if fileErr != nil {
		t.Fatalf("Could not write exclude file: %v", fileErr)
	}

	var err error

	// Test --exclude-file:
	err = testRunBackupAssumeFailure(t, filepath.Dir(env.testdata), []string{"testdata"}, BackupOptions{ExcludeFiles: []string{excludeFile}}, env.gopts)

	rtest.Equals(t, `Fatal: --exclude-file: invalid pattern(s) provided:
*[._]log[.-][0-9]
!*[._]log[.-][0-9]`, err.Error())

	// Test --iexclude-file
	err = testRunBackupAssumeFailure(t, filepath.Dir(env.testdata), []string{"testdata"}, BackupOptions{InsensitiveExcludeFiles: []string{excludeFile}}, env.gopts)

	rtest.Equals(t, `Fatal: --iexclude-file: invalid pattern(s) provided:
*[._]log[.-][0-9]
!*[._]log[.-][0-9]`, err.Error())
}
