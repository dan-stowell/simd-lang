//go:build race

package main

// raceBuild: fc itself is built with the race detector (FC_RACE=1 driver/mkfc.sh): its allocator hooks need the Go allocator.
const raceBuild = true
