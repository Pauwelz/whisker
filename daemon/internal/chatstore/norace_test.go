//go:build !race

package chatstore_test

// readBudget is the most milliseconds a read of a page with many mentions may take.
const readBudget = 50
