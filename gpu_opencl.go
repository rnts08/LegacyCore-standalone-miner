//go:build ignore

package main

// OpenCL stub for future GPU mining support.
// Yespower is memory-hard (~8 MB scratch per hash), so GPU requires
// large shared scratch and batch kernel submission.
//
// Build with: CGO_ENABLED=1 go build -tags opencl ...
