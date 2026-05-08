//go:build integration

package gitserver_test

// TestBootstrap_RealGitea is consolidated into TestE2E_RealGitea
// (e2e_realgitea_test.go) as a subtest. Gitea's package-global state
// (setting.*, route registry) doesn't survive two NewServer cycles
// in one process, so all real-Gitea integration tests in this package
// share one Server via t.Run subtests.
