//go:build race

package chatstore_test

// readBudget is wider under the race detector, which slows the read by about 5x.
const readBudget = 250
