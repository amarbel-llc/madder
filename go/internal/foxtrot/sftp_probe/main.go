// Package sftp_probe contains pure-function probes that classify
// SFTP blob bytes against candidate blob_store_configs.Config
// values without any SFTP, UI, or filesystem dependency.
//
// See docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md
// for the design. The package's API surface is documented in
// stage.go, candidate.go, verify.go, and aggregate.go.
package sftp_probe
